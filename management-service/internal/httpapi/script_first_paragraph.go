package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

type scriptFirstPaidState struct {
	Key       string                      `json:"key"`
	Complete  bool                        `json:"complete"`
	SourceSHA string                      `json:"source_sha256,omitempty"`
	VoiceID   string                      `json:"voice_id,omitempty"`
	Subtitles []ttsgateway.SpeechSubtitle `json:"subtitles,omitempty"`
}

// Only local validation/admission failures can clear the intent marker. An
// ambiguous remote outcome never buys the paragraph again on retry/restart.
func prepareScriptFirstPaid(ctx context.Context, unit volcengineScriptUnit, generate func(context.Context, string) (ttsgateway.SynthesizeResponse, error)) (ttsgateway.SynthesizeResponse, error) {
	path := filepath.Join(unit.Dir, "paragraph-job.json")
	audioPath := filepath.Join(unit.Dir, "paragraph.wav")
	state := scriptFirstPaidState{Key: unit.Key}
	err := readVolcengineJSON(path, &state)
	if err == nil {
		if state.Key != unit.Key || !state.Complete {
			return ttsgateway.SynthesizeResponse{}, errors.New("整段合成结果尚未确认，已保留原任务，未重复购买")
		}
		raw, e := os.ReadFile(audioPath)
		if e != nil {
			return ttsgateway.SynthesizeResponse{}, errors.New("已付费整段声音暂不可用，未重复购买")
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != state.SourceSHA {
			return ttsgateway.SynthesizeResponse{}, errors.New("已付费整段声音校验失败，未重复购买")
		}
		return ttsgateway.SynthesizeResponse{AudioFile: audioPath, VoiceID: state.VoiceID, Subtitles: state.Subtitles}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return ttsgateway.SynthesizeResponse{}, err
	}
	if err = ctx.Err(); err != nil {
		return ttsgateway.SynthesizeResponse{}, err
	}
	if err = writeVolcengineJSON(path, state); err != nil {
		return ttsgateway.SynthesizeResponse{}, err
	}
	out, err := generate(ctx, audioPath)
	if err != nil {
		var admission scriptBudgetAdmissionError
		if ttsgateway.DefinitelyNotSent(err) || errors.As(err, &admission) {
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
		audio, err = s.ensureQwenScriptParagraph(ctx, tenant, room, session, v, unit, allowSynthesis)
		if err != nil {
			return audio, err
		}
	}
	// 播放分块已取消：一段话术合成后就是一条音轨，不再按约 60 秒切成子块逐块归档。
	// 互动切断由已上线 Core 的声学切点负责，不再依赖块首块尾。
	return audio, nil
}

func (s *Server) ensureQwenScriptParagraph(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, allowSynthesis bool) (volcengineCachedAudio, error) {
	var empty volcengineCachedAudio
	unlock, err := lockVolcengineParagraph(ctx, unit.Dir)
	if err != nil {
		return empty, err
	}
	defer unlock()
	indexPath := filepath.Join(unit.Dir, "cache.json")
	cache, err := loadVolcengineParagraphCache(indexPath, unit.Key)
	if err != nil {
		return empty, err
	}
	if audio, ok := cache.Audio[100]; ok {
		if _, err = s.recordingStreamAssetURL(ctx, tenant, audio.AssetID); err == nil {
			return audio, nil
		}
	}
	if !allowSynthesis {
		if _, e := os.Stat(filepath.Join(unit.Dir, "paragraph-job.json")); errors.Is(e, os.ErrNotExist) {
			return empty, errors.New("循环缺少首轮声音，未重新合成")
		}
	}
	generated, err := prepareScriptFirstPaid(ctx, unit, func(c context.Context, path string) (ttsgateway.SynthesizeResponse, error) {
		if !allowSynthesis {
			return ttsgateway.SynthesizeResponse{}, errors.New("循环缺少首轮声音，未重新合成")
		}
		request := withNaturalBreathInstruction(ttsgateway.SynthesizeRequest{Provider: ttsgateway.ProviderQwen, Model: v.VoiceIdentity.Model, VoiceID: v.VoiceIdentity.VoiceID, Text: unit.Text, Rate: v.VoiceIdentity.Rate, Instruction: recordingInstructionForVersion(v, unit.Text), ParagraphPath: path})
		release, err := reserveScriptVoiceSynthesis(v.PlanID, unit.Key, hanCount(unit.Text))
		if err != nil {
			return ttsgateway.SynthesizeResponse{}, scriptBudgetAdmissionError{err}
		}
		scope := liveCostCallScope{TenantID: tenant, RoomID: room, SessionID: session, PlanID: v.PlanID, Priority: "necessary", Reason: "话术原文整段首次合成", Key: fmt.Sprintf("script-paragraph:%d:%s", session, unit.Key)}
		if parent := liveCostScope(c); parent.Priority == "optional" {
			scope.Priority = "optional"
			scope.Reason = parent.Reason
		}
		out, err := ttsgateway.NewFromEnv().SynthesizeURL(s.withLiveCostTTS(c, scope), request)
		release(ttsgateway.DefinitelyNotSent(err))
		return out, err
	})
	if err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	duration, err := volcengineWAVDuration(generated.AudioFile)
	if err != nil {
		return empty, err
	}
	// Keep the untimed transcript separate. No character-based cue timing.
	var timeline []model.LiveAgentPlanTimelineSegment
	raw, err := os.ReadFile(generated.AudioFile)
	if err != nil {
		return empty, err
	}
	assetID, err := s.archiveUntimedScriptFirstAudio(ctx, tenant, room, v, unit.Key, unit.Text, generated.VoiceID, raw, duration)
	if err != nil {
		return empty, err
	}
	audio := volcengineCachedAudio{AssetID: assetID, DurationMS: duration, VoiceID: generated.VoiceID, Timeline: timeline, UntimedTranscript: true}
	cache.Audio[100] = audio
	if err = writeVolcengineJSON(indexPath, cache); err != nil {
		return empty, err
	}
	return audio, nil
}
