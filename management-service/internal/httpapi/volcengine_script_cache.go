package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

type volcengineCachedAudio struct {
	UntimedTranscript bool                                 `json:"untimed_transcript,omitempty"`
	AssetID           int64                                `json:"asset_id"`
	DurationMS        int64                                `json:"duration_ms"`
	VoiceID           string                               `json:"voice_id"`
	Timeline          []model.LiveAgentPlanTimelineSegment `json:"timeline"`
	Blocks            []scriptFirstCachedBlock             `json:"blocks,omitempty"`
}
type volcengineParagraphCache struct {
	Key   string                        `json:"key"`
	Audio map[int]volcengineCachedAudio `json:"audio"`
}

var volcengineParagraphLocks sync.Map

func lockVolcengineParagraph(ctx context.Context, path string) (func(), error) {
	actual, _ := volcengineParagraphLocks.LoadOrStore(path, make(chan struct{}, 1))
	ch := actual.(chan struct{})
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func writeVolcengineJSON(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".paragraph-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
func readVolcengineJSON(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 8<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}
func volcengineParagraphKey(voice, text string, rate float64) string {
	raw, _ := json.Marshal([]any{"volcengine-paragraph-v1", ttsgateway.VolcengineICL2, voice, rate, text, ttsgateway.VolcengineSynthesisProfile})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func loadVolcengineParagraphCache(path, key string) (volcengineParagraphCache, error) {
	c := volcengineParagraphCache{Key: key, Audio: map[int]volcengineCachedAudio{}}
	err := readVolcengineJSON(path, &c)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil || c.Key != key || c.Audio == nil {
		return c, errors.New("长段缓存索引损坏；已保留付费音频，不重新合成")
	}
	return c, nil
}

type volcengineArchiveInfoKey struct{}

func withVolcengineArchiveInfo(ctx context.Context, percent int) context.Context {
	return context.WithValue(ctx, volcengineArchiveInfoKey{}, percent)
}
func applyVolcengineArchiveInfo(ctx context.Context, payload map[string]any) {
	if pct, ok := ctx.Value(volcengineArchiveInfoKey{}).(int); ok {
		payload["segment_head_ms"], payload["segment_tail_ms"] = 0, 0
		payload["timeline_precision"] = "supplier_timestamps"
		payload["playback_tempo"] = float64(pct) / 100
		if pct != 100 {
			payload["timeline_precision"] = "supplier_timestamps_atempo"
		}
	}
}

// The provider staging WAV is the paid immutable source. Even an OSS/archive
// failure or stopping the live session cannot cause another submit for it.
func (s *Server) ensureVolcengineParagraph(ctx context.Context, tenant, room, session int64, version model.LiveAgentPlanVersion, unit volcengineScriptUnit, percent int, allowSynthesis bool) (volcengineCachedAudio, error) {
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
	if audio, ok := cache.Audio[percent]; ok {
		if _, err = s.recordingStreamAssetURL(ctx, tenant, audio.AssetID); err == nil {
			return audio, nil
		}
		// Keep the index, but rebuild this missing archive from its paid source.
	}
	r := ttsgateway.SynthesizeRequest{Provider: ttsgateway.ProviderVolcengine, Model: ttsgateway.VolcengineICL2, VoiceID: version.VoiceIdentity.VoiceID, Rate: version.VoiceIdentity.Rate, Text: unit.Text,
		LongText: &ttsgateway.LongTextJob{StatePath: filepath.Join(unit.Dir, "job.json"), SourceVoiceID: version.VoiceIdentity.VoiceID}}
	generated, ok := ttsgateway.LoadLongTextResult(r)
	var archivedSource *volcengineCachedAudio
	if !ok {
		if base, exists := cache.Audio[100]; exists {
			url, e := s.recordingStreamAssetURL(ctx, tenant, base.AssetID)
			if e == nil {
				path, e := downloadVolcengineCachedWAV(ctx, url, unit.Dir)
				if e != nil {
					return empty, e
				}
				defer os.Remove(path)
				generated = ttsgateway.SynthesizeResponse{AudioFile: path, VoiceID: base.VoiceID}
				ok = true
				archivedSource = &base
			}
		}
	}
	if !ok {
		if !allowSynthesis || percent != 100 {
			return empty, errors.New("循环段缺少首轮声音，未重新调用TTS")
		}
		if err = waitLiveLeasePermit(ctx); err != nil {
			return empty, err
		}
		if err = ctx.Err(); err != nil {
			return empty, err
		}
		scope := liveCostCallScope{TenantID: tenant, RoomID: room, SessionID: session, PlanID: version.PlanID, Priority: "necessary", Reason: "高仿真话术原文长段首次合成", Key: fmt.Sprintf("volcengine-long:%d:%s", session, unit.Key)}
		generated, err = ttsgateway.NewFromEnv().SynthesizeURL(s.withLiveCostTTS(ctx, scope), r)
		if err != nil {
			return empty, err
		}
	}
	// A cloud task may finish after stop. Its local paid WAV has been retained;
	// never archive/publish late audio into a stopped/replaced session.
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	duration, err := volcengineWAVDuration(generated.AudioFile)
	if err != nil {
		return empty, err
	}
	var timeline []model.LiveAgentPlanTimelineSegment
	if archivedSource != nil {
		if duration != archivedSource.DurationMS {
			return empty, errors.New("缓存音频与字幕时长不一致，未重新合成")
		}
		timeline = append([]model.LiveAgentPlanTimelineSegment(nil), archivedSource.Timeline...)
	} else {
		timeline, err = volcengineParagraphTimeline(unit.Text, generated.Subtitles, duration)
		if err != nil {
			return empty, err
		}
	}
	path := generated.AudioFile
	if percent != 100 {
		path, err = renderVolcengineTempo(ctx, path, percent)
		if err != nil {
			return empty, err
		}
		defer os.Remove(path)
		newDuration, e := volcengineWAVDuration(path)
		if e != nil {
			return empty, e
		}
		timeline, err = stretchVolcengineTimeline(timeline, duration, newDuration)
		if err != nil {
			return empty, err
		}
		duration = newDuration
	}
	f, err := os.Open(path)
	if err != nil {
		return empty, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxVolcengineRenderBytes+1))
	f.Close()
	if err != nil || int64(len(raw)) > maxVolcengineRenderBytes {
		return empty, errors.New("读取长段声音失败；未重新合成")
	}
	archive, err := s.archiveFullShowVoice(withVolcengineArchiveInfo(ctx, percent), tenant, 0, version.PlanID, room, "long-"+unit.Key[:16], generated.VoiceID, ttsgateway.VolcengineICL2, version.VoiceIdentity.Source, raw, duration, timeline, version.VoiceIdentity.Rate)
	if err != nil {
		return empty, err
	}
	audio := volcengineCachedAudio{AssetID: archive.AssetID, DurationMS: duration, VoiceID: generated.VoiceID, Timeline: timeline}
	cache.Audio[percent] = audio
	if err = writeVolcengineJSON(indexPath, cache); err != nil {
		return empty, err
	}
	// 切点表在干净 PCM 上计算并随资产归档。失败只记日志，不阻断播放：
	// 缺表时保留完整音频，不根据猜测的位置硬切。
	if _, cutErr := s.ensureVolcengineCuts(ctx, path, unit.Dir, unit.Key, percent); cutErr != nil {
		log.Printf("cuts scan skipped key=%s percent=%d err=%v", unit.Key[:min(len(unit.Key), 8)], percent, cutErr)
	}
	return audio, nil
}
