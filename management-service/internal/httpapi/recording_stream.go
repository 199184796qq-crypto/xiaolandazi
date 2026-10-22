package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

const recordingStreamMode = "stream_clone"

type recordingStreamSegment struct {
	Text string
}

type recordingStreamCacheEntry struct {
	Text  string
	Track coreclient.ProgramRefreshTrack
}

var recordingStreamCache sync.Map // tenant/plan/room/voice/text -> []recordingStreamCacheEntry

func recordingVoiceMode(version model.LiveAgentPlanVersion) string {
	if model.LiveContentModeFromGenerationContext(version.GenerationContext) != model.LiveContentUserAudio {
		return ""
	}
	mode, _ := version.GenerationContext["recording_voice_mode"].(string)
	if strings.TrimSpace(mode) == recordingStreamMode {
		return recordingStreamMode
	}
	return ""
}

func (s *Server) runRecordingStreamProducer(ctx context.Context, tenant, room, runtimeID int64, version model.LiveAgentPlanVersion) error {
	transcript := ""
	for _, variant := range version.Variants {
		if variant.IsFormal && strings.TrimSpace(variant.Text) != "" {
			transcript = strings.TrimSpace(variant.Text)
			if variant.VariantKey == "CUSTOM" {
				break
			}
		}
	}
	if transcript == "" {
		return errors.New("录音主线没有可播放的原文")
	}
	segments := splitRecordingStreamText(transcript)
	if len(segments) == 0 {
		return errors.New("录音主线分段为空")
	}
	log.Printf("recording stream start tenant=%d room=%d version=%d segments=%d", tenant, room, version.ID, len(segments))
	polish := s.recordingPolishState(ctx, tenant, version.PlanID, room)
	history := make([][]recordingStreamCacheEntry, 0, 4)
	cache := map[string]coreclient.ProgramRefreshTrack{}
	programID, generation, versionNo := "", uint64(0), version.VersionNo
	round := 0
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if round > 0 {
			if err := waitRecordingStreamBuffer(ctx, s, tenant, room, programID, version.ID); err != nil {
				return err
			}
		}
		current := make([]recordingStreamCacheEntry, 0, len(segments))
		for index, segment := range segments {
			if err := ctx.Err(); err != nil {
				return err
			}
			if programID != "" {
				if err := waitRecordingStreamBuffer(ctx, s, tenant, room, programID, version.ID); err != nil {
					return err
				}
			}
			text := segment.Text
			if round > 0 {
				switch {
				case round < 4:
					text = polishRecordingText(ctx, s, polish, text)
				case rng.Intn(100) < 30:
					text = polishRecordingText(ctx, s, polish, text)
				default:
					text = ""
				}
			}
			var entry recordingStreamCacheEntry
			if text == "" {
				entry = chooseRecordingHistory(history, index, rng)
			} else if cached, ok := cache[text]; ok {
				entry = recordingStreamCacheEntry{Text: text, Track: cached}
			}
			if entry.Track.AudioURL == "" {
				if text == "" {
					text = segment.Text
				}
				track, err := s.synthesizeRecordingStreamSegment(ctx, tenant, version, text, round, index)
				if err != nil {
					return err
				}
				entry = recordingStreamCacheEntry{Text: text, Track: track}
				cache[text] = track
				log.Printf("recording stream segment synthesized tenant=%d room=%d round=%d index=%d chars=%d", tenant, room, round, index, len([]rune(text)))
			} else {
				log.Printf("recording stream segment cached tenant=%d room=%d round=%d index=%d chars=%d", tenant, room, round, index, len([]rune(text)))
			}
			if programID == "" {
				snapshot, err := s.startRecordingStreamProgram(ctx, tenant, room, runtimeID, version, entry.Track)
				if err != nil {
					return err
				}
				programID, generation, versionNo = snapshot.ProgramID, snapshot.Generation, snapshot.VersionNo
				log.Printf("recording stream program started tenant=%d room=%d program=%s", tenant, room, programID)
				s.reportLiveStartup(ctx, tenant, room, runtimeID, "sending_audio")
			} else {
				nextGeneration, err := s.appendRecordingStreamTrack(ctx, tenant, room, version, programID, generation, versionNo, entry.Track)
				if err != nil {
					return err
				}
				generation = nextGeneration
				log.Printf("recording stream segment appended tenant=%d room=%d round=%d index=%d generation=%d", tenant, room, round, index, generation)
			}
			current = append(current, entry)
		}
		history = append(history, current)
		if len(history) > 4 {
			history = history[len(history)-4:]
		}
		round++
	}
}

func waitRecordingStreamBuffer(ctx context.Context, s *Server, tenant, room int64, programID string, versionID int64) error {
	if programID == "" {
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		snapshot, err := s.core.GetRoomProgramSnapshot(ctx, tenant, room)
		if err != nil {
			return err
		}
		if !snapshot.Running || snapshot.ProgramID != programID || snapshot.VersionID != versionID {
			return anchorcycle.ErrStale
		}
		// Keep at least one upcoming segment buffered. This is a small queue,
		// not the old full-script audio: generation continues while playback runs.
		// Keep roughly two short segments queued; generating the entire script
		// up front would spend TTS quota before playback catches up.
		if snapshot.BufferedMainlineMS < 20000 || snapshot.WaitingForContent {
			return nil
		}
		if err := waitLiveProducer(ctx); err != nil {
			return err
		}
	}
}

func (s *Server) startRecordingStreamProgram(ctx context.Context, tenant, room, runtimeID int64, version model.LiveAgentPlanVersion, track coreclient.ProgramRefreshTrack) (coreclient.ProgramRefreshSnapshot, error) {
	query := url.Values{"tenant_id": {strconv.FormatInt(tenant, 10)}}
	body := map[string]any{
		"session_id": fmt.Sprintf("recording-stream-%d", runtimeID),
		"label":      fmt.Sprintf("录音AI声音 V%d", version.VersionNo),
		"version_id": version.ID,
		"version_no": version.VersionNo,
		"sequential": true,
		"tracks":     []coreclient.ProgramRefreshTrack{track},
	}
	resp, err := s.core.DoRoom(ctx, tenant, room, http.MethodPost, fmt.Sprintf("/internal/v1/rooms/%d/audio/program/start", room), query, body)
	if err != nil {
		return coreclient.ProgramRefreshSnapshot{}, err
	}
	defer resp.Body.Close()
	var snapshot coreclient.ProgramRefreshSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return snapshot, err
	}
	if resp.StatusCode != http.StatusOK || !snapshot.Sequential || snapshot.VersionID != version.ID || snapshot.ProgramID == "" {
		return snapshot, errors.New("录音AI声音主线启动未确认")
	}
	return snapshot, nil
}

func (s *Server) appendRecordingStreamTrack(ctx context.Context, tenant, room int64, version model.LiveAgentPlanVersion, programID string, generation uint64, versionNo int64, track coreclient.ProgramRefreshTrack) (uint64, error) {
	job := fmt.Sprintf("recording-%d-%d", version.ID, generation+1)
	input := coreclient.ProgramRefreshInput{
		JobID: job, ExpectedProgramID: programID, ExpectedVersionID: version.ID,
		ExpectedGeneration: generation, Generation: generation + 1,
		NewVersionID: version.ID, NewVersionNo: versionNo,
		ValidUntil: time.Now().Add(30 * time.Second),
		Tracks:     []coreclient.ProgramRefreshTrack{track},
	}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return generation, err
		}
		snapshot, err := s.core.GetRoomProgramSnapshot(ctx, tenant, room)
		if err != nil {
			if err := waitLiveProducer(ctx); err != nil {
				return generation, err
			}
			continue
		}
		if !snapshot.Running || snapshot.ProgramID != programID || snapshot.VersionID != version.ID {
			return generation, anchorcycle.ErrStale
		}
		if snapshot.LastAppliedJobID == job && snapshot.Generation == input.Generation {
			return snapshot.Generation, nil
		}
		if snapshot.PendingJobID == job {
			_, err = s.core.RenewRoomProgramRefresh(ctx, tenant, room, job, programID, time.Now().Add(30*time.Second))
		} else if snapshot.PendingJobID == "" && snapshot.Generation == generation {
			input.NewVersionNo = snapshot.VersionNo
			input.ValidUntil = time.Now().Add(30 * time.Second)
			_, err = s.core.RefreshRoomProgram(ctx, tenant, room, input)
		}
		if err != nil && !errors.Is(err, coreclient.ErrProgramRefreshConflict) {
			return generation, err
		}
		if err := waitLiveProducer(ctx); err != nil {
			return generation, err
		}
	}
	return generation, errors.New("录音AI声音追加超时")
}

func (s *Server) synthesizeRecordingStreamSegment(ctx context.Context, tenant int64, version model.LiveAgentPlanVersion, text string, round, index int) (coreclient.ProgramRefreshTrack, error) {
	identity := version.VoiceIdentity
	voiceID, modelName, err := s.resolveFormalVoice(ctx, tenant, fullShowVariantVoiceInput{
		Source: identity.Source, VoiceID: identity.VoiceID, ProfileID: identity.ProfileID,
		BindingID: identity.BindingID, Model: identity.Model,
	})
	if err != nil {
		return coreclient.ProgramRefreshTrack{}, err
	}
	gateway := ttsgateway.NewFromEnv()
	generated, err := gateway.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{
		Model: modelName, VoiceID: voiceID, Text: text, Rate: identity.Rate,
	})
	if err != nil {
		return coreclient.ProgramRefreshTrack{}, err
	}
	raw, err := downloadGeneratedVoiceWAV(ctx, generated.AudioURL)
	if err != nil {
		return coreclient.ProgramRefreshTrack{}, err
	}
	format, pcm, byteRate, err := parseGeneratedVoiceWAV(raw)
	if err != nil {
		return coreclient.ProgramRefreshTrack{}, err
	}
	combined, timeline, durationMS, err := combineGeneratedVoiceWAV(fmt.Sprintf("recording-%d-%d", round, index), []generatedVoiceWAVPiece{{text: text, fmtData: format, pcmData: pcm, byteRate: byteRate}})
	if err != nil {
		return coreclient.ProgramRefreshTrack{}, err
	}
	archive, err := s.archiveFullShowVoice(ctx, tenant, 0, version.PlanID, version.RoomID,
		fmt.Sprintf("recording-%d-%d", round, index), voiceID, modelName, identity.Source,
		combined, durationMS, timeline, identity.Rate)
	if err != nil {
		return coreclient.ProgramRefreshTrack{}, err
	}
	audioURL, err := s.recordingStreamAssetURL(ctx, tenant, archive.AssetID)
	if err != nil {
		return coreclient.ProgramRefreshTrack{}, err
	}
	return coreclient.ProgramRefreshTrack{
		ID:    fmt.Sprintf("recording-%d-%d-%d", round, index, archive.AssetID),
		Label: "录音AI声音", Text: text, AudioURL: audioURL,
		DurationMS: int(durationMS),
		Timeline:   recordingStreamTimeline(timeline),
		SafePoints: recordingStreamSafePoints(archive.SafePoints),
	}, nil
}

func recordingStreamTimeline(timeline []model.LiveAgentPlanTimelineSegment) []coreclient.ProgramRefreshTimelineSegment {
	out := make([]coreclient.ProgramRefreshTimelineSegment, 0, len(timeline))
	for _, segment := range timeline {
		out = append(out, coreclient.ProgramRefreshTimelineSegment{
			SegmentID: segment.SegmentID, Index: segment.Index, StartMS: int(segment.StartMS),
			EndMS: int(segment.EndMS), Text: segment.Text, SafeCut: segment.SafeCut,
		})
	}
	return out
}

func recordingStreamSafePoints(points []model.LiveAgentPlanSafePoint) []coreclient.ProgramRefreshSafePoint {
	out := make([]coreclient.ProgramRefreshSafePoint, 0, len(points))
	for _, point := range points {
		out = append(out, coreclient.ProgramRefreshSafePoint{
			ID: point.ID, CutMS: int(point.CutMS), Score: point.Score, Grade: point.Grade,
			Kind: point.Kind, SentenceID: point.SentenceID, LeftPreview: point.LeftPreview, NextPreview: point.NextPreview,
		})
	}
	return out
}

func (s *Server) recordingStreamAssetURL(ctx context.Context, tenant, assetID int64) (string, error) {
	if assetID <= 0 || s.assetStorage == nil {
		return "", errors.New("录音AI声音资产不存在")
	}
	asset, err := s.store.GetMediaAsset(ctx, tenant, assetID)
	if err != nil {
		return "", err
	}
	assetStore, err := s.assetStorage.For(asset.StorageDriver)
	if err != nil {
		return "", err
	}
	expiry := 12 * time.Hour
	if s.assetURLTTL > expiry {
		expiry = s.assetURLTTL
	}
	if signer, ok := assetStore.(interface {
		InternalSignedURL(context.Context, string, time.Duration) (string, error)
	}); ok {
		return signer.InternalSignedURL(ctx, asset.ObjectKey, expiry)
	}
	return assetStore.SignedURL(ctx, asset.ObjectKey, expiry)
}

func (s *Server) recordingPolishState(ctx context.Context, tenant, plan, room int64) *anchorcycle.State {
	state := &anchorcycle.State{TenantID: tenant, PlanID: plan, RoomID: room, RunID: anchorcycle.ID(), Surface: "formal_live"}
	if err := installP0Mother(state); err != nil {
		return nil
	}
	if err := s.installCycleTextModel(ctx, state); err != nil || state.TextModel == nil {
		return nil
	}
	return state
}

func polishRecordingText(ctx context.Context, server *Server, state *anchorcycle.State, text string) string {
	if state == nil || server == nil || strings.TrimSpace(text) == "" {
		return text
	}
	for attempt := 0; attempt < 2; attempt++ {
		rewritten, err := server.rewriteCycleText(ctx, state, text)
		if err != nil || strings.TrimSpace(rewritten) == "" {
			return text
		}
		candidate := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: strings.TrimSpace(rewritten)}
		candidate.OutputText = candidate.Text
		if state.CheckP0Candidate(&candidate) == nil {
			if len([]rune(candidate.OutputText)) > 80 {
				log.Printf("recording stream polish expanded beyond 80 runes; fallback original")
				return text
			}
			return candidate.OutputText
		}
		log.Printf("recording stream polish p0 rejected attempt=%d", attempt+1)
	}
	return text
}

func chooseRecordingHistory(history [][]recordingStreamCacheEntry, index int, rng *rand.Rand) recordingStreamCacheEntry {
	if len(history) == 0 {
		return recordingStreamCacheEntry{}
	}
	round := rng.Intn(len(history))
	if index >= len(history[round]) {
		return recordingStreamCacheEntry{}
	}
	return history[round][index]
}

func splitRecordingStreamText(text string) []recordingStreamSegment {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	var result []recordingStreamSegment
	start := 0
	for start < len(runes) {
		end := min(len(runes), start+80)
		if end < len(runes) {
			cut := -1
			for i := end - 1; i >= start+40; i-- {
				if strings.ContainsRune("。！？；!?;\n", runes[i]) {
					cut = i + 1
					break
				}
			}
			if cut > start {
				end = cut
			}
		}
		if end-start < 40 && end < len(runes) {
			end = min(len(runes), start+80)
		}
		part := strings.TrimSpace(string(runes[start:end]))
		if part != "" {
			result = append(result, recordingStreamSegment{Text: part})
		}
		start = end
		for start < len(runes) && strings.ContainsRune(" \t\r\n", runes[start]) {
			start++
		}
	}
	return result
}
