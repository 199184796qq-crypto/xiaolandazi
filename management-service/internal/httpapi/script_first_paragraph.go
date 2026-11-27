package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

var errScriptFirstSynthesisDisabled = errors.New("循环缺少首轮声音，未重新合成")

type scriptFirstPaidState struct {
	Key       string                      `json:"key"`
	Complete  bool                        `json:"complete"`
	SourceSHA string                      `json:"source_sha256,omitempty"`
	VoiceID   string                      `json:"voice_id,omitempty"`
	Subtitles []ttsgateway.SpeechSubtitle `json:"subtitles,omitempty"`
}

// Availability takes precedence: retain an unusable prior attempt as evidence,
// then allow a fresh synthesis under the caller's paragraph lock.
func prepareScriptFirstPaid(ctx context.Context, unit volcengineScriptUnit, generate func(context.Context, string) (ttsgateway.SynthesizeResponse, error)) (ttsgateway.SynthesizeResponse, error) {
	path := filepath.Join(unit.Dir, "paragraph-job.json")
	audioPath := filepath.Join(unit.Dir, "paragraph.wav")
	state := scriptFirstPaidState{Key: unit.Key}
	err := readVolcengineJSON(path, &state)
	if err == nil {
		raw, e := os.ReadFile(audioPath)
		sum := sha256.Sum256(raw)
		if state.Key == unit.Key && state.Complete && e == nil && len(raw) > 0 && hex.EncodeToString(sum[:]) == state.SourceSHA {
			return ttsgateway.SynthesizeResponse{AudioFile: audioPath, VoiceID: state.VoiceID, Subtitles: state.Subtitles}, nil
		}
	}
	if !errors.Is(err, os.ErrNotExist) {
		if err = retireUnavailableScriptFiles(ctx, unit, "paragraph-job.json", "paragraph.wav"); err != nil {
			return ttsgateway.SynthesizeResponse{}, err
		}
	}
	state = scriptFirstPaidState{Key: unit.Key}
	if err = ctx.Err(); err != nil {
		return ttsgateway.SynthesizeResponse{}, err
	}
	if err = writeVolcengineJSON(path, state); err != nil {
		return ttsgateway.SynthesizeResponse{}, err
	}
	if err = checkScriptGenerationGuard(ctx); err != nil {
		return ttsgateway.SynthesizeResponse{}, err
	}
	out, err := generate(ctx, audioPath)
	if err != nil {
		var admission scriptBudgetAdmissionError
		if ttsgateway.DefinitelyNotSent(err) || errors.As(err, &admission) || errors.Is(err, errScriptFirstSynthesisDisabled) {
			if e := os.Remove(path); e != nil {
				return out, e
			}
		}
		return out, err
	}
	// The provider already persisted its result before returning. Save captions
	// under a non-cancelled local operation before checking the live owner.
	raw, err := os.ReadFile(audioPath)
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(raw)
	state = scriptFirstPaidState{Key: unit.Key, Complete: true, SourceSHA: hex.EncodeToString(sum[:]), VoiceID: out.VoiceID, Subtitles: out.Subtitles}
	if err = writeVolcengineJSON(path, state); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Server) ensureScriptFirstParagraph(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, allowSynthesis bool) (volcengineCachedAudio, error) {
	var bindErr error
	ctx, v, unit, bindErr = s.voiceAssetParagraph(ctx, tenant, v, unit)
	if bindErr != nil {
		return volcengineCachedAudio{}, bindErr
	}
	ctx = withScriptSharedAsset(ctx, v, room)
	var audio volcengineCachedAudio
	if v.VoiceIdentity.Model == ttsgateway.VolcengineICL2 {
		var err error
		audio, err = s.ensureVolcengineParagraph(ctx, tenant, room, session, v, unit, 100, allowSynthesis)
		if err != nil {
			return audio, err
		}
	} else {
		var err error
		audio, err = s.ensureUntimedScriptParagraph(ctx, tenant, room, session, v, unit, allowSynthesis)
		if err != nil {
			return audio, err
		}
	}
	// 播放分块已取消：一段话术合成后就是一条音轨，不再按约 60 秒切成子块逐块归档。
	// 互动切断由已上线 Core 的声学切点负责，不再依赖块首块尾。
	return audio, nil
}

func (s *Server) ensureUntimedScriptParagraph(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, allowSynthesis bool) (volcengineCachedAudio, error) {
	var empty volcengineCachedAudio
	unlock, err := lockVolcengineParagraph(ctx, unit.Dir)
	if err != nil {
		return empty, err
	}
	defer unlock()
	indexPath := filepath.Join(unit.Dir, "cache.json")
	cache, err := loadRecoverableScriptCache(ctx, unit)
	if err != nil {
		return empty, err
	}
	if audio, ok := cache.Audio[100]; ok {
		if _, err = s.recordingStreamAssetURL(ctx, tenant, audio.AssetID); err == nil && s.scriptAudioAvailable(ctx, tenant, audio.AssetID) {
			return audio, nil
		}
	}
	if !scriptPaidSourceReady(unit) {
		if audio, ok := s.recoverScriptParagraphArchive(ctx, tenant, v, unit, &cache); ok {
			return audio, nil
		}
	}
	if !allowSynthesis {
		if _, e := os.Stat(filepath.Join(unit.Dir, "paragraph-job.json")); errors.Is(e, os.ErrNotExist) {
			return empty, errScriptFirstSynthesisDisabled
		}
	}
	synthesize := func(c context.Context, part volcengineScriptUnit, path string) (ttsgateway.SynthesizeResponse, error) {
		if !allowSynthesis {
			return ttsgateway.SynthesizeResponse{}, errScriptFirstSynthesisDisabled
		}
		provider := ttsgateway.ProviderForModel(v.VoiceIdentity.Model)
		request := withMainlineDeliveryInstruction(ttsgateway.SynthesizeRequest{
			Provider: provider, Model: v.VoiceIdentity.Model, VoiceID: v.VoiceIdentity.VoiceID,
			Text: part.Text, Rate: v.VoiceIdentity.Rate, Instruction: recordingInstructionForVersion(v, part.Text),
			ParagraphPath: path, DrainOnCancel: provider == ttsgateway.ProviderTencent,
		})
		release, err := reserveScriptVoiceSynthesis(v.PlanID, part.Key, hanCount(part.Text))
		if err != nil {
			return ttsgateway.SynthesizeResponse{}, scriptBudgetAdmissionError{err}
		}
		scope := liveCostCallScope{TenantID: tenant, RoomID: room, SessionID: session, PlanID: v.PlanID, Priority: "necessary", Reason: "话术原文整段合成或恢复补合成", Key: fmt.Sprintf("script-paragraph:%d:%s:%s", session, part.Key, anchorcycle.ID())}
		if parent := liveCostScope(c); parent.Priority == "optional" {
			scope.Priority = "optional"
			scope.Reason = parent.Reason
		}
		if err := checkScriptGenerationGuard(c); err != nil {
			release(true)
			return ttsgateway.SynthesizeResponse{}, err
		}
		out, err := ttsgateway.NewFromEnv().SynthesizeURL(s.withLiveCostTTS(c, scope), request)
		release(ttsgateway.DefinitelyNotSent(err))
		return out, err
	}
	generated, err := prepareScriptFirstPaid(ctx, unit, func(c context.Context, path string) (ttsgateway.SynthesizeResponse, error) {
		if allowSynthesis {
			reportScriptManualProgress(c, scriptManualSynthesizing)
		}
		if v.VoiceIdentity.Model == "qwen3-tts-flash" && utf8.RuneCountInString(unit.Text) > ttsgateway.QwenFlashMaxInputRunes {
			return synthesizeQwenFlashParagraph(c, unit, path, synthesize)
		}
		return synthesize(c, unit, path)
	})
	if err != nil {
		return empty, err
	}
	if err = checkScriptGenerationGuard(ctx); err != nil {
		return empty, err
	}
	raw, duration, err := readScriptParagraphPlaybackWAV(ctx, v.VoiceIdentity.Model, unit, generated.AudioFile)
	if err != nil {
		return empty, err
	}
	// Keep the untimed transcript separate. No character-based cue timing.
	assetID, err := s.archiveUntimedScriptFirstAudio(ctx, tenant, room, v, unit.Key, unit.Text, generated.VoiceID, raw, duration)
	if err != nil {
		return empty, err
	}
	audio := volcengineCachedAudio{AssetID: assetID, DurationMS: duration, VoiceID: generated.VoiceID, UntimedTranscript: true}
	if err = checkScriptGenerationGuard(ctx); err != nil {
		return empty, err
	}
	cache.Audio[100] = audio
	if err = writeVolcengineJSON(indexPath, cache); err != nil {
		return empty, err
	}
	return audio, nil
}

func readScriptParagraphPlaybackWAV(ctx context.Context, modelName string, unit volcengineScriptUnit, path string) ([]byte, int64, error) {
	tencent := modelName == ttsgateway.TencentPremiumModel
	if tencent {
		// Keep the paid 16 kHz source for retries. Only the playback copy is
		// normalized to the 24 kHz format shared by long-paragraph assets.
		normalized, err := normalizeAudioToCoreWAV(ctx, path)
		if err != nil {
			return nil, 0, err
		}
		path = normalized
		defer os.Remove(normalized)
	}
	duration, err := volcengineWAVDuration(path)
	if err != nil {
		if !tencent {
			if e := retireUnavailableScriptFiles(ctx, unit, "paragraph-job.json", "paragraph.wav"); e != nil {
				return nil, 0, e
			}
		}
		return nil, 0, err
	}
	raw, err := os.ReadFile(path)
	return raw, duration, err
}
