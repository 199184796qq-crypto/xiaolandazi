package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/humanfission"
	"livecompanion/management/internal/storage"
	"livecompanion/management/internal/ttsgateway"
)

type fissionEdit struct {
	Part   int    `json:"part"`
	Before string `json:"before"`
	After  string `json:"after"`
}

var errHumanFissionRejected = errors.New("组合暂不符合当前事实或前后语义")

func (s *Server) humanFissionSnapshot(ctx context.Context, job db.HumanFissionJob) (string, json.RawMessage, error) {
	facts, err := s.buildRewriteFactsPack(ctx, job.Tenant, job.Plan)
	if err != nil {
		return "", nil, err
	}
	p0, _, err := loadP0Mother()
	if err != nil {
		return "", nil, err
	}
	voice, _ := s.planSelectedVoice(ctx, job.Tenant, job.Plan, 0)
	state, _, err := s.planRiskAnnotationP0(ctx, job.Tenant, job.Plan, p0)
	if err != nil {
		return "", nil, err
	}
	return humanfission.Digest([]any{facts, p0, state.P0Allowances, voice}), facts, nil
}

func (s *Server) renderHumanFission(ctx context.Context, job *db.HumanFissionJob, oss *storage.OSS, last string) error {
	stamp, facts, err := s.humanFissionSnapshot(ctx, *job)
	if err != nil {
		return err
	}
	if job.State.Renders == nil {
		job.State.Renders = map[string]humanfission.Render{}
	}
	var rejected bool
	for _, recipe := range job.State.Choices(last) {
		key := humanfission.Digest([]string{recipe.ID, stamp})
		render, ok := job.State.Renders[key]
		if ok {
			ok, err = oss.Exists(ctx, render.Object)
			if err != nil {
				return err
			}
		}
		if !ok {
			render, err = s.buildHumanFissionRender(ctx, *job, recipe, oss, facts, stamp, key)
			if errors.Is(err, errHumanFissionRejected) {
				rejected = true
				continue
			}
			if err != nil {
				return err
			}
			job.State.Renders[key] = render
		}
		fresh, _, err := s.humanFissionSnapshot(ctx, *job)
		if err != nil {
			return err
		}
		if fresh != stamp {
			return errors.New("方案事实或音色刚刚更新，请重新准备试听")
		}
		for i := range job.State.Recipes {
			if job.State.Recipes[i].ID == recipe.ID {
				job.State.Recipes[i].Used = true
				break
			}
		}
		job.State.Last = recipe.ID
		job.State.Render = &render
		return nil
	}
	if rejected {
		return errHumanFissionRejected
	}
	return errors.New(humanfission.ExhaustedMessage)
}

func (s *Server) buildHumanFissionRender(ctx context.Context, job db.HumanFissionJob, recipe humanfission.Recipe, oss *storage.OSS, facts json.RawMessage, stamp, key string) (humanfission.Render, error) {
	cues := make(map[int][]humanfission.Cue)
	input := fissionReviewInput{Order: recipe.Order, Facts: facts, Parts: make([]fissionReviewPart, 0, len(job.State.Parts))}
	for _, number := range recipe.Order {
		if number < 1 || number > len(job.State.Parts) {
			return humanfission.Render{}, errHumanFissionRejected
		}
		if _, known := cues[number]; !known {
			srt := job.State.Parts[number-1].SRT
			lines, err := humanfission.ReadSRT(srt)
			if err != nil {
				return humanfission.Render{}, err
			}
			cues[number] = lines
			input.Parts = append(input.Parts, fissionReviewPart{Number: number, SRT: srt})
		}
	}
	conflicts, err := fissionNumericConflicts(recipe.Order, cues, facts)
	if err != nil {
		return humanfission.Render{}, err
	}
	input.Conflicts = conflicts
	var review struct {
		Blocked *bool          `json:"blocked"`
		Edits   *[]fissionEdit `json:"edits"`
	}
	if err := s.completeHumanFission(ctx, "review", input, &review); err != nil {
		return humanfission.Render{}, err
	}
	if review.Blocked == nil || review.Edits == nil {
		return humanfission.Render{}, errors.New("模型未完成事实复核")
	}
	if *review.Blocked {
		return humanfission.Render{}, errHumanFissionRejected
	}
	edits := map[int]map[string]string{}
	md, _, err := loadP0Mother()
	if err != nil {
		return humanfission.Render{}, err
	}
	p0, policy, err := s.planRiskAnnotationP0(ctx, job.Tenant, job.Plan, md)
	if err != nil {
		return humanfission.Render{}, err
	}
	for _, edit := range *review.Edits {
		if len(cues[edit.Part]) == 0 || strings.TrimSpace(edit.After) == "" || edit.Before == edit.After || edits[edit.Part][edit.Before] != "" ||
			strings.ContainsAny(edit.After, "\r\n") || len([]rune(edit.After)) > 2000 {
			return humanfission.Render{}, errHumanFissionRejected
		}
		if len(autoScriptP0Hits(p0, policy, edit.After)) > 0 {
			return humanfission.Render{}, errHumanFissionRejected
		}
		if edits[edit.Part] == nil {
			edits[edit.Part] = map[string]string{}
		}
		edits[edit.Part][edit.Before] = edit.After
	}
	for _, conflict := range input.Conflicts {
		if !conflict.resolved(edits[conflict.Part][conflict.Before]) {
			return humanfission.Render{}, errHumanFissionRejected
		}
	}
	var voice anchorcycle.VoiceConfig
	if len(edits) > 0 {
		voice, err = s.planSelectedVoice(ctx, job.Tenant, job.Plan, 0)
		if err != nil {
			return humanfission.Render{}, err
		}
	}
	var pcm []int16
	timeline := []humanfission.Cue{}
	type renderedPart struct {
		pcm  []int16
		cues []humanfission.Cue
	}
	resolved := map[int]renderedPart{}
	for _, number := range recipe.Order {
		item, known := resolved[number]
		if !known {
			part := job.State.Parts[number-1]
			item.pcm, err = humanFissionRead(ctx, oss, part.Object)
			if err != nil {
				return humanfission.Render{}, err
			}
			item.cues = cues[number]
			if fixes := edits[number]; len(fixes) > 0 {
				item.pcm, item.cues, err = s.applyFissionEdits(ctx, job, oss, voice, item.pcm, item.cues, fixes)
				if err != nil {
					return humanfission.Render{}, err
				}
			}
			resolved[number] = item
		}
		offset := humanfission.MS(len(pcm))
		pcm = append(pcm, item.pcm...)
		if humanfission.MS(len(pcm))*10 > job.State.Duration*13 {
			return humanfission.Render{}, errHumanFissionRejected
		}
		for _, cue := range item.cues {
			cue.Start += offset
			cue.End = min(cue.End+offset, humanfission.MS(len(pcm)))
			if cue.End > cue.Start {
				timeline = append(timeline, cue)
			}
		}
	}
	var transcript strings.Builder
	for _, cue := range timeline {
		transcript.WriteString(cue.Text)
		transcript.WriteByte('\n')
	}
	duration := humanfission.MS(len(pcm))
	if !humanfission.DurationOK(duration, job.State.Duration) || len(autoScriptP0Hits(p0, policy, transcript.String())) > 0 {
		return humanfission.Render{}, errHumanFissionRejected
	}
	object, err := s.putHumanFission(ctx, job, oss, "mix-"+key, pcm)
	return humanfission.Render{RecipeID: recipe.ID, Object: object, Duration: duration, SRT: humanfission.SRT(timeline), Stamp: stamp}, err
}

type fissionSpan struct {
	Start, End int
	Cue        humanfission.Cue
}

func fissionNearest(samples []int, ms int64) (int, bool) {
	best, delta := 0, int64(701)
	for _, point := range samples {
		d := humanfission.MS(point) - ms
		if d < 0 {
			d = -d
		}
		if d < delta {
			best, delta = point, d
		}
	}
	return best, delta <= 700
}

func (s *Server) applyFissionEdits(ctx context.Context, job db.HumanFissionJob, oss *storage.OSS, voice anchorcycle.VoiceConfig, pcm []int16, cues []humanfission.Cue, edits map[string]string) ([]int16, []humanfission.Cue, error) {
	counts := map[string]int{}
	for _, cue := range cues {
		counts[cue.Text]++
	}
	for before := range edits {
		if counts[before] != 1 {
			return nil, nil, errHumanFissionRejected
		}
	}
	quiet := append([]int{0}, humanfission.QuietCuts(pcm)...)
	quiet = append(quiet, len(pcm))
	spans := []fissionSpan{}
	previous := 0
	local := true
	for i, cue := range cues {
		if replacement, ok := edits[cue.Text]; ok {
			a, okA := fissionNearest(quiet, cue.Start)
			b, okB := fissionNearest(quiet, cue.End)
			leftSafe := i == 0 || humanfission.MS(a) >= cues[i-1].End-100
			rightSafe := i == len(cues)-1 || humanfission.MS(b) <= cues[i+1].Start+100
			if !okA || !okB || !leftSafe || !rightSafe || b <= a || a < previous {
				local = false
				break
			}
			cue.Text = replacement
			spans = append(spans, fissionSpan{Start: a, End: b, Cue: cue})
			previous = b
		}
	}
	if !local {
		// An unsafe sentence boundary uses the containing semantic clip as
		// the replacement unit; the immutable original remains available.
		var text strings.Builder
		for _, cue := range cues {
			if replacement, ok := edits[cue.Text]; ok {
				cue.Text = replacement
			}
			text.WriteString(cue.Text)
		}
		audio, err := s.humanFissionTTS(ctx, job, oss, voice, text.String())
		return audio, []humanfission.Cue{{Start: 0, End: humanfission.MS(len(audio)), Text: text.String()}}, err
	}
	var out []int16
	var result []humanfission.Cue
	audioAt, cueAt := 0, 0
	for _, span := range spans {
		offset := humanfission.MS(len(out) - audioAt)
		if len(out) < audioAt {
			offset = -humanfission.MS(audioAt - len(out))
		}
		out = append(out, pcm[audioAt:span.Start]...)
		for cueAt < len(cues) && edits[cues[cueAt].Text] == "" {
			cue := cues[cueAt]
			cue.Start = max(0, cue.Start+offset)
			cue.End = min(cue.End+offset, humanfission.MS(len(out)))
			if cue.End > cue.Start {
				result = append(result, cue)
			}
			cueAt++
		}
		audio, err := s.humanFissionTTS(ctx, job, oss, voice, span.Cue.Text)
		if err != nil {
			return nil, nil, err
		}
		start := humanfission.MS(len(out))
		out = append(out, audio...)
		result = append(result, humanfission.Cue{Start: start, End: humanfission.MS(len(out)), Text: span.Cue.Text})
		audioAt = span.End
		cueAt++
	}
	delta := int64(len(out)-audioAt) * 1000 / humanfission.Rate
	out = append(out, pcm[audioAt:]...)
	for _, cue := range cues[cueAt:] {
		cue.Start = max(0, cue.Start+delta)
		cue.End = min(cue.End+delta, humanfission.MS(len(out)))
		if cue.End > cue.Start {
			result = append(result, cue)
		}
	}
	return out, result, nil
}

func (s *Server) humanFissionTTS(ctx context.Context, job db.HumanFissionJob, oss *storage.OSS, voice anchorcycle.VoiceConfig, text string) ([]int16, error) {
	hash := humanfission.Digest([]any{voice, text})
	name := "tts-" + hash
	object := job.ObjectKey(name)
	exists, err := oss.Exists(ctx, object)
	if err != nil {
		return nil, err
	}
	if exists {
		return humanFissionRead(ctx, oss, object)
	}
	gateway := ttsgateway.NewFromEnv()
	raw, _, _, err := synthesizeMainlinePreview(ctx, journaledMainlineSynthesizer{s: s, gateway: gateway, profile: voice.Model}, voice, text, hash)
	if err != nil {
		return nil, fmt.Errorf("方案音色合成失败: %w", err)
	}
	pcm, err := humanfission.ReadWAV(raw)
	if err != nil {
		file, copyErr := copyAudioReaderToTemp(bytes.NewReader(raw), ".wav")
		if copyErr != nil {
			return nil, copyErr
		}
		defer os.Remove(file)
		wav, convertErr := normalizeAudioToCoreWAV(ctx, file)
		if convertErr != nil {
			return nil, convertErr
		}
		defer os.Remove(wav)
		normalized, readErr := os.ReadFile(wav)
		if readErr != nil {
			return nil, readErr
		}
		pcm, err = humanfission.ReadWAV(normalized)
		if err != nil {
			return nil, err
		}
	}
	// Six milliseconds at the edges avoid a step when changing voice source.
	for i := 0; i < min(144, len(pcm)/2); i++ {
		pcm[i] = int16(int(pcm[i]) * i / 144)
		j := len(pcm) - 1 - i
		pcm[j] = int16(int(pcm[j]) * i / 144)
	}
	if _, err = s.putHumanFission(ctx, job, oss, name, pcm); err != nil {
		return nil, err
	}
	return pcm, nil
}
