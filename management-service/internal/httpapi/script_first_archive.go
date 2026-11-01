package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/storage"
)

// Untimed audio is a playback asset with original text and an actual duration,
// but without SRT/semantic timing inferred from character count.
func (s *Server) archiveUntimedScriptFirstAudio(ctx context.Context, tenant, room int64, v model.LiveAgentPlanVersion, key, text, voice string, raw []byte, duration int64) (int64, error) {
	return archiveUntimedScriptFirstAsset(ctx, s.assetStorage, s.store.CreateMediaAsset, tenant, room, v, key, text, voice, raw, duration)
}

func archiveUntimedScriptFirstAsset(ctx context.Context, registry *storage.Registry, create func(context.Context, model.CreateMediaAssetInput) (model.MediaAsset, error), tenant, room int64, v model.LiveAgentPlanVersion, key, text, voice string, raw []byte, duration int64) (int64, error) {
	if registry == nil {
		return 0, errors.New("媒体存储尚未初始化")
	}
	// Preserve the original paid file. Publish canonical playback headers so
	// downstream decoders do not inherit the supplier's unknown-length marker.
	raw, err := normalizeScriptFirstWAVHeader(raw)
	if err != nil {
		return 0, err
	}
	objectKey, err := newMediaObjectKey(tenant, "generated_voice", "paragraph-"+key[:min(len(key), 20)]+".wav")
	if err != nil {
		return 0, err
	}
	storage := registry.Current()
	if oss, e := registry.For("oss"); e == nil {
		storage = oss
	}
	if err = storage.Put(ctx, objectKey, bytes.NewReader(raw), "audio/wav"); err != nil {
		return 0, err
	}
	sum := sha256.Sum256(raw)
	d := uint64(duration)
	asset, err := create(ctx, model.CreateMediaAssetInput{TenantID: tenant, AssetType: "generated_voice", OriginalName: "script-paragraph.wav", StorageDriver: storage.Driver(), StorageBucket: storage.Bucket(), ObjectKey: objectKey, MIMEType: "audio/wav", SizeBytes: uint64(len(raw)), DurationMS: &d, ChecksumSHA256: hex.EncodeToString(sum[:]), Metadata: map[string]any{
		"purpose": "live_agent_formal_voice_timeline", "plan_id": v.PlanID, "room_id": room, "voice_id": voice, "tts_model": v.VoiceIdentity.Model, "source": v.VoiceIdentity.Source, "voice_rate": v.VoiceIdentity.Rate, "paragraph_key": key, "original_transcript": strings.TrimSpace(text), "untimed_transcript": true, "timeline_precision": "none", "segment_head_ms": 0, "segment_tail_ms": 0,
	}})
	if err != nil {
		_ = storage.Delete(ctx, objectKey)
		return 0, err
	}
	return asset.ID, nil
}
