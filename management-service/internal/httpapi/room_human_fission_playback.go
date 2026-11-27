package httpapi

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/humanfission"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/storage"
)

const fissionChoicePrefix = "fission:"
const fissionChoiceGroup = "human-fission-plans"
const humanSourceChoicePrefix = "human-source:"

var fissionChoicePattern = regexp.MustCompile("^fission:([1-9][0-9]*):([0-9a-f]{64}):([0-9a-f]{64})$")
var errFissionChoiceChanged = errors.New("所选真人话术组合已更新，请重新选择")
var errHumanSourceChoiceChanged = errors.New("所选真人原声暂不可用，请重新选择")

func scriptStandaloneChoice(id string) bool {
	return strings.HasPrefix(id, fissionChoicePrefix) || strings.HasPrefix(id, humanSourceChoicePrefix)
}

func humanSourceChoiceName(name string) string {
	name = strings.TrimSpace(name)
	switch ext := filepath.Ext(name); strings.ToLower(ext) {
	case ".wav", ".mp3", ".m4a", ".aac", ".ogg", ".opus", ".flac", ".webm", ".wma", ".mp4", ".aiff", ".aif", ".amr":
		name = strings.TrimSpace(strings.TrimSuffix(name, ext))
	}
	if name == "" {
		return "原始录音"
	}
	return name
}

func fissionChoiceID(job db.HumanFissionJob, recipe humanfission.Recipe) string {
	return fmt.Sprintf("%s%d:%s:%s", fissionChoicePrefix, job.Source, job.State.RecipeVersion(), recipe.ID)
}

func parseFissionChoiceID(id string) (int64, string, string, error) {
	fields := fissionChoicePattern.FindStringSubmatch(id)
	if fields == nil {
		return 0, "", "", errFissionChoiceChanged
	}
	source, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, "", "", errFissionChoiceChanged
	}
	return source, fields[2], fields[3], nil
}

func (s *Server) humanScriptPlaybackGroups(ctx context.Context, tenant, plan int64) ([]scriptManualGroup, scriptManualGroup) {
	legacy := scriptManualGroup{ID: fissionChoiceGroup, Title: "真人话术组合"}
	if s.assetStorage == nil {
		return nil, legacy
	}
	sources, _ := s.store.ListPlanHumanScriptSources(ctx, tenant, plan)
	jobs, _ := s.store.ListHumanFissionChoices(ctx, tenant, plan)
	return humanScriptGroups(sources, jobs)
}

func humanScriptGroups(sources []db.ScriptHumanVoiceSummary, jobs []db.HumanFissionJob) ([]scriptManualGroup, scriptManualGroup) {
	legacy := scriptManualGroup{ID: fissionChoiceGroup, Title: "真人话术组合"}
	bySource := make(map[int64][]scriptManualPiece, len(jobs))
	for _, job := range jobs {
		for i, recipe := range job.State.Recipes {
			piece := scriptManualPiece{ID: fissionChoiceID(job, recipe), Title: job.State.RecipeName(recipe, i+1)}
			bySource[job.Source] = append(bySource[job.Source], piece)
			legacy.Pieces = append(legacy.Pieces, piece)
		}
	}
	groups := make([]scriptManualGroup, 0, len(sources))
	for _, source := range sources {
		id, name := humanSourceChoicePrefix+strconv.FormatInt(source.ID, 10), humanSourceChoiceName(source.Name)
		original := scriptManualPiece{ID: id, Title: name}
		groups = append(groups, scriptManualGroup{ID: id, Title: name, Pieces: append([]scriptManualPiece{original}, bySource[source.ID]...)})
	}
	return groups, legacy
}

func (s *Server) loadHumanSourceChoice(ctx context.Context, tenant, plan int64, id string) (model.MediaAsset, error) {
	field, ok := strings.CutPrefix(id, humanSourceChoicePrefix)
	source, err := strconv.ParseInt(field, 10, 64)
	if !ok || err != nil || source <= 0 || strconv.FormatInt(source, 10) != field || s.assetStorage == nil {
		return model.MediaAsset{}, errHumanSourceChoiceChanged
	}
	asset, err := s.store.ScriptHumanVoice(ctx, tenant, plan, db.PlanHumanScriptScope, source)
	if err != nil || asset.DurationMS == nil || *asset.DurationMS == 0 {
		return model.MediaAsset{}, errHumanSourceChoiceChanged
	}
	return asset, nil
}

func humanSourceCachedAudio(asset model.MediaAsset) volcengineCachedAudio {
	audio := scriptHumanCachedAudio(asset, volcengineScriptUnit{})
	transcript, _ := asset.Metadata["script_transcript"].(string)
	if timed, err := fissionCachedAudio(asset.ID, humanfission.Render{SRT: transcript, Duration: audio.DurationMS}); err == nil &&
		len(timed.Timeline) > 0 && timed.Timeline[len(timed.Timeline)-1].EndMS <= audio.DurationMS {
		timed.HumanSource = audio.HumanSource
		return timed
	}
	audio.Text = strings.TrimSpace(transcript)
	return audio
}

func (s *Server) loadFissionChoice(ctx context.Context, tenant, plan int64, id string) (db.HumanFissionJob, int, string, error) {
	source, version, recipe, err := parseFissionChoiceID(id)
	if err != nil {
		return db.HumanFissionJob{}, -1, "", err
	}
	job, err := s.store.HumanFission(ctx, tenant, plan, source)
	if err != nil {
		return job, -1, "", errFissionChoiceChanged
	}
	asset, err := s.store.ScriptHumanVoice(ctx, tenant, plan, db.PlanHumanScriptScope, source)
	if err != nil || job.State.Input != db.HumanFissionInput(asset) || job.State.RecipeVersion() != version {
		return job, -1, "", errFissionChoiceChanged
	}
	index := job.State.RecipeIndex(recipe)
	if index < 0 {
		return job, -1, "", errFissionChoiceChanged
	}
	return job, index, humanSourceChoiceName(asset.OriginalName), nil
}

func (s *Server) renderExactHumanFission(ctx context.Context, job *db.HumanFissionJob, oss *storage.OSS) error {
	target := job.State.Exact
	if target == nil || target.Version != job.State.RecipeVersion() {
		return errFissionChoiceChanged
	}
	index := job.State.RecipeIndex(target.Recipe)
	if index < 0 {
		return errFissionChoiceChanged
	}
	stamp, facts, err := s.humanFissionSnapshot(ctx, *job)
	if err != nil {
		return err
	}
	if stamp != target.Stamp {
		return errors.New("方案事实或音色刚刚更新，请重新选择组合")
	}
	key := humanfission.Digest([]string{target.Recipe, stamp})
	render, err := s.buildHumanFissionRender(ctx, *job, job.State.Recipes[index], oss, facts, stamp, key)
	if err != nil {
		return err
	}
	stampNow, _, err := s.humanFissionSnapshot(ctx, *job)
	if err != nil {
		return err
	}
	if stampNow != stamp {
		return errors.New("方案事实或音色刚刚更新，请重新选择组合")
	}
	if job.State.Renders == nil {
		job.State.Renders = map[string]humanfission.Render{}
	}
	job.State.Renders[key] = render
	return nil
}

// Audio preparation is asynchronous so the current room can keep playing and
// appending its regular script while the selected recipe is reviewed.
func (s *Server) prepareManualFission(ctx context.Context, tenant, plan int64, command scriptManualJump, enqueue bool) (volcengineCachedAudio, bool, error) {
	var empty volcengineCachedAudio
	if command.RequestedAt.IsZero() || time.Since(command.RequestedAt) > 8*time.Minute {
		return empty, false, errors.New("组合准备超时，请重新选择")
	}
	job, index, recordingName, err := s.loadFissionChoice(ctx, tenant, plan, command.PieceID)
	if err != nil {
		return empty, false, err
	}
	if job.Busy() {
		return empty, false, nil
	}
	if !enqueue && len(job.State.Renders) == 0 {
		return empty, false, nil
	}
	recipe := job.State.Recipes[index]
	if job.Action == "render_exact" && job.Status == "failed" && job.State.Exact != nil && job.State.Exact.RequestID == command.RequestID {
		return empty, false, errors.New("所选组合的事实复核或声音准备未完成，请重试")
	}
	stamp, _, err := s.humanFissionSnapshot(ctx, job)
	if err != nil {
		return empty, false, errors.New("暂时无法核对组合的当前事实，请重试")
	}
	key := humanfission.Digest([]string{recipe.ID, stamp})
	if render, ok := job.State.Renders[key]; ok && render.Stamp == stamp && render.RecipeID == recipe.ID {
		asset, err := s.store.ScriptHumanVoiceByObject(ctx, tenant, plan, db.HumanFissionScope(job.Source), render.Object)
		if err == nil && asset.DurationMS != nil && int64(*asset.DurationMS) == render.Duration && s.scriptAudioAvailable(ctx, tenant, asset.ID) {
			audio, err := fissionCachedAudio(asset.ID, render)
			if err != nil {
				return empty, false, err
			}
			audio.HumanSource = &mainlineHumanSource{RecordingName: recordingName, CombinationName: job.State.RecipeName(recipe, index+1)}
			return audio, true, nil
		}
		if job.Action == "render_exact" && job.State.Exact != nil && job.State.Exact.RequestID == command.RequestID {
			return empty, false, errors.New("所选组合声音暂时不可用，请重试")
		}
	}
	if !enqueue {
		return empty, false, nil
	}
	err = s.store.QueueExactHumanFission(ctx, tenant, plan, job.Source, humanfission.ExactRequest{
		Recipe: recipe.ID, Version: job.State.RecipeVersion(), Stamp: stamp, RequestID: command.RequestID,
	})
	if err != nil && !errors.Is(err, db.ErrFissionBusy) {
		return empty, false, errors.New("组合无法进入准备队列，请重新选择")
	}
	if err == nil {
		wakeHumanFission()
	}
	return empty, false, nil
}

func fissionCachedAudio(asset int64, render humanfission.Render) (volcengineCachedAudio, error) {
	cues, err := humanfission.ReadSRT(render.SRT)
	if err != nil {
		return volcengineCachedAudio{}, errors.New("所选组合的文稿时间轴不可用")
	}
	audio := volcengineCachedAudio{AssetID: asset, DurationMS: render.Duration, HumanVoice: true}
	var text strings.Builder
	for i, cue := range cues {
		text.WriteString(cue.Text)
		text.WriteByte('\n')
		index := i + 1
		audio.Timeline = append(audio.Timeline, model.LiveAgentPlanTimelineSegment{
			SegmentID: fmt.Sprintf("fission-%d", index), Index: index, StartMS: cue.Start, EndMS: cue.End, Text: cue.Text,
		})
	}
	audio.Text = strings.TrimSpace(text.String())
	return audio, nil
}

func scriptFissionContinuation(cursor *volcengineScriptCursor, trackID string) int {
	for i := len(cursor.Delivered) - 1; i >= 0; i-- {
		if track := cursor.Delivered[i]; track.TrackID == trackID && track.Unit >= 0 {
			return track.Unit
		}
	}
	return cursor.Previous
}

func resumeScriptAfterFission(cursor *volcengineScriptCursor, unit int) {
	for position, index := range cursor.Order {
		if index == unit {
			cursor.Position, cursor.Previous, cursor.LastQueued = position+1, unit, unit
			break
		}
	}
	cursor.Pending = nil
	cursor.PendingSilence = nil
}
