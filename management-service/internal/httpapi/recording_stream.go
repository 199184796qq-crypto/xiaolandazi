package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

const (
	recordingStreamMode = "stream_clone"
	// Keep Core tracks long enough that an interaction can cut and return
	// inside the same track, like the original long-form mainline. The
	// individual 40-80 rune TTS pieces still drive cache and polishing.
	recordingStreamBlockSegments = 3
)

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

// runRecordingStreamProducerLegacy 是改造前的单文本框播放路径，已由
// recording_stream_tabs.go 里的多标签实现取代，保留仅作对照，不要在新代码里调用。
func (s *Server) runRecordingStreamProducerLegacy(ctx context.Context, tenant, room, runtimeID int64, version model.LiveAgentPlanVersion) error {
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
	programID, generation := "", uint64(0)
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
		blockStart := 0
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
			if round == 0 {
				// The first pass is polished too: reading the raw transcript is
				// noticeably less natural than the live delivery we want.
				text = polishRecordingText(ctx, s, polish, text)
			} else {
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
			current = append(current, entry)
			blockTarget := recordingStreamBlockTarget(blockStart)
			if len(current)-blockStart >= blockTarget || index == len(segments)-1 {
				block, err := s.combineRecordingStreamBlock(ctx, tenant, version, current[blockStart:], round, blockStart/recordingStreamBlockSegments)
				if err != nil {
					return err
				}
				if programID == "" {
					snapshot, err := s.startRecordingStreamProgram(ctx, tenant, room, runtimeID, version, block)
					if err != nil {
						return err
					}
					programID, generation = snapshot.ProgramID, snapshot.Generation
					log.Printf("recording stream program started tenant=%d room=%d program=%s", tenant, room, programID)
					s.reportLiveStartup(ctx, tenant, room, runtimeID, "sending_audio")
				} else {
					nextGeneration, err := s.appendRecordingStreamTrack(ctx, tenant, room, version, programID, block)
					if err != nil {
						return err
					}
					generation = nextGeneration
				}
				log.Printf("recording stream block appended tenant=%d room=%d round=%d chunks=%d generation=%d", tenant, room, round, len(current)-blockStart, generation)
				blockStart = len(current)
			}
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
		// System interactions need enough future mainline to find a cut and
		// return. Keep roughly 45s queued; 20s was too tight once TTS latency
		// consumed the buffer and Core answered claim with claimed=false.
		if snapshot.BufferedMainlineMS < 45000 || snapshot.WaitingForContent {
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

func (s *Server) appendRecordingStreamTrack(ctx context.Context, tenant, room int64, version model.LiveAgentPlanVersion, programID string, track coreclient.ProgramRefreshTrack) (uint64, error) {
	query := url.Values{"tenant_id": {strconv.FormatInt(tenant, 10)}}
	body := map[string]any{
		"expected_program_id": programID,
		"expected_version_id": version.ID,
		"tracks":              []coreclient.ProgramRefreshTrack{track},
	}
	resp, err := s.core.DoRoom(ctx, tenant, room, http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/audio/program/append", room), query, body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	var snapshot coreclient.ProgramRefreshSnapshot
	var decodeErr error
	if readErr == nil {
		decodeErr = json.Unmarshal(raw, &snapshot)
	}
	if resp.StatusCode != http.StatusOK {
		body := strings.TrimSpace(string(raw))
		if len([]rune(body)) > 500 {
			body = string([]rune(body)[:500])
		}
		log.Printf("recording stream append rejected tenant=%d room=%d program=%s status=%d body=%s", tenant, room, programID, resp.StatusCode, body)
		return 0, anchorcycle.ErrStale
	}
	// Core returns 200 only after it accepts the ready unit. Some append
	// snapshots omit program/version fields (or contain newer receipt shapes),
	// so a 200 must not be turned into a false "late result" failure.
	if snapshot.ProgramID != "" && snapshot.ProgramID != programID {
		log.Printf("recording stream append stale program tenant=%d room=%d expected=%s got=%s", tenant, room, programID, snapshot.ProgramID)
		return 0, anchorcycle.ErrStale
	}
	if snapshot.VersionID != 0 && snapshot.VersionID != version.ID {
		log.Printf("recording stream append stale version tenant=%d room=%d expected=%d got=%d", tenant, room, version.ID, snapshot.VersionID)
		return 0, anchorcycle.ErrStale
	}
	if decodeErr != nil {
		log.Printf("recording stream append accepted with partial snapshot tenant=%d room=%d decode_error=%v", tenant, room, decodeErr)
	}
	return snapshot.Generation, nil
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
	rate := identity.Rate
	if rate == 0 {
		rate = 1
	}
	generated, err := gateway.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{
		Model: modelName, VoiceID: voiceID, Text: text, Rate: rate,
		Instruction: recordingInstructionForVersion(version, text),
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

func recordingStreamBlockTarget(blockStart int) int {
	switch blockStart {
	case 0:
		return 1
	case 1:
		return 2
	default:
		return recordingStreamBlockSegments
	}
}

func (s *Server) combineRecordingStreamBlock(ctx context.Context, tenant int64, version model.LiveAgentPlanVersion, entries []recordingStreamCacheEntry, round, block int) (coreclient.ProgramRefreshTrack, error) {
	identity := version.VoiceIdentity
	voiceID, modelName, err := s.resolveFormalVoice(ctx, tenant, fullShowVariantVoiceInput{
		Source: identity.Source, VoiceID: identity.VoiceID, ProfileID: identity.ProfileID,
		BindingID: identity.BindingID, Model: identity.Model,
	})
	if err != nil {
		return coreclient.ProgramRefreshTrack{}, err
	}
	pieces := make([]generatedVoiceWAVPiece, 0, len(entries))
	texts := make([]string, 0, len(entries))
	for _, entry := range entries {
		raw, err := downloadGeneratedVoiceWAV(ctx, entry.Track.AudioURL)
		if err != nil {
			return coreclient.ProgramRefreshTrack{}, err
		}
		format, pcm, byteRate, err := parseGeneratedVoiceWAV(raw)
		if err != nil {
			return coreclient.ProgramRefreshTrack{}, err
		}
		pieces = append(pieces, generatedVoiceWAVPiece{text: entry.Text, fmtData: format, pcmData: pcm, byteRate: byteRate})
		texts = append(texts, entry.Text)
	}
	variantKey := fmt.Sprintf("recording-%d-block-%d", round, block)
	combined, timeline, durationMS, err := combineGeneratedVoiceWAV(variantKey, pieces)
	if err != nil {
		return coreclient.ProgramRefreshTrack{}, err
	}
	archive, err := s.archiveFullShowVoice(ctx, tenant, 0, version.PlanID, version.RoomID, variantKey, voiceID, modelName, identity.Source, combined, durationMS, timeline, identity.Rate)
	if err != nil {
		return coreclient.ProgramRefreshTrack{}, err
	}
	audioURL, err := s.recordingStreamAssetURL(ctx, tenant, archive.AssetID)
	if err != nil {
		return coreclient.ProgramRefreshTrack{}, err
	}
	return coreclient.ProgramRefreshTrack{
		ID: fmt.Sprintf("recording-%d-block-%d-%d", round, block, archive.AssetID), Label: "录音AI声音",
		Text: strings.Join(texts, ""), AudioURL: audioURL, DurationMS: int(durationMS),
		Timeline: recordingStreamTimeline(timeline), SafePoints: recordingStreamSafePoints(archive.SafePoints),
	}, nil
}

// recordingStreamInstruction picks a short, deterministic delivery direction
// for each TTS chunk. It never changes the spoken text; it only tells the
// cloned voice how to phrase it (questions lift, prices slow down, urgency is
// tighter, ordinary chat keeps a relaxed pause and drawl).
func recordingInstructionForVersion(version model.LiveAgentPlanVersion, text string) string {
	if version.VoiceIdentity.EmotionEnabled == nil || !*version.VoiceIdentity.EmotionEnabled {
		return ""
	}
	return recordingStreamInstruction(text)
}

func recordingStreamInstruction(text string) string {
	trimmed := strings.TrimSpace(text)
	switch {
	case trimmed == "":
		return "像真人直播自然说话，允许停顿和拖音，不要播音腔。"
	case strings.ContainsAny(trimmed, "？?"):
		return "像真人直播随口一问，句尾自然上扬，问完停半拍。"
	case strings.ContainsAny(trimmed, "！!") || containsAnyText(trimmed, "赶紧", "抓紧", "最后", "抢", "限时"):
		return "像真人直播提醒，重点词稍重，语速略快，句尾收住。"
	case containsAnyText(trimmed, "元", "块", "价", "优惠", "券", "套餐"):
		return "像真人直播报价，数字清楚，前后各停半拍，别像播报。"
	case containsAnyText(trimmed, "欢迎", "来了", "进来", "朋友"):
		return "像真人打招呼，亲切放松，尾音别太用力。"
	default:
		return "像真人直播聊天，逗号处停半拍，允许轻轻拖音，不要播音腔。"
	}
}

func containsAnyText(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
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
	// 不设字数上限：分段已经按句末标点切开，长句本身就是一个完整语义单元，
	// 它的结尾天然是合法断点，没必要为了字数去腰斩。
	return polishRecordingTextLimit(ctx, server, state, text, 0)
}

// polishRecordingTextLimit 与 polishRecordingText 相同，但可指定长度上限。
// 批量润色传入的是 5 句合并的整段，本身上限就该更大；单句才用 110 字。
func polishRecordingTextLimit(ctx context.Context, server *Server, state *anchorcycle.State, text string, maxRunes int) string {
	if state == nil || server == nil || strings.TrimSpace(text) == "" {
		return text
	}
	source := text
	for attempt := 0; attempt < 3; attempt++ {
		rewritten, err := server.rewriteCycleText(ctx, state, text)
		if err != nil || strings.TrimSpace(rewritten) == "" {
			return source
		}
		candidate := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: strings.TrimSpace(rewritten)}
		candidate.OutputText = candidate.Text
		if problem := state.CheckP0Candidate(&candidate); problem == nil {
			// 润色会把句子写长；超过上限就回退原文。
			if maxRunes > 0 && len([]rune(candidate.OutputText)) > maxRunes {
				log.Printf("recording stream polish expanded beyond %d runes; fallback original", maxRunes)
				return source
			}
			return candidate.OutputText
		} else {
			// 踩到违规词：把违规之处告诉模型，让它就这一句重写，
			// 而不是直接放弃、退回原文。
			log.Printf("recording stream polish p0 rejected attempt=%d reason=%v", attempt+1, problem)
			text = "原句：" + source + "\n上一次改写出现了违规表述：" + problem.Error() +
				"\n请重写原句，去掉上述违规表述，其余意思、数字、商品名保持不变，只输出改写后的这一句。"
		}
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
