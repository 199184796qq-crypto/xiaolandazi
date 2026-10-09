package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/anchorvoicecache"
	"livecompanion/management/internal/contentstrategy"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/storage"
)

func (s *Server) SetAnchorVoiceCache(cache anchorvoicecache.Repository) { s.anchorVoiceCache = cache }

// Separate tenant/plan/room and immutable style/fact/voice identities. The same
// angle alone must never reuse another room's or an outdated plan's recording.
func cycleVoiceCacheGroup(state *anchorcycle.State, segment anchorcycle.Segment) string {
	contract := "oss-cache-bundle-v1"
	if state.PreviewLimitMS > 0 {
		contract += ":limited-preview"
	}
	if state.ProductFocus != nil {
		contract += ":product-focus-v2:" + state.ProductScopeSHA(segment.Task)
	}
	if strategy := state.ContentStrategy; strategy != nil && strategy.Enabled {
		contract += ":content-strategy-v1:" + anchorvoicecache.Digest(strategy)
		if mother, err := interactionpolicy.Read("fact-purpose-angle-generation.md"); err == nil {
			contract += ":" + contentstrategy.SHA(string(mother))
		} else {
			contract += ":mother-unavailable"
		}
	}
	var voice any
	if v := state.Config.TTS; v != nil {
		voice = struct {
			Source, VoiceID, Model string
			ProfileID              int64
			Rate                   float64
		}{v.Source, v.VoiceID, v.Model, v.ProfileID, v.Rate}
	}
	return anchorvoicecache.Digest(struct {
		Tenant, Plan, Room, Style, Card, Wheel                        int64
		StyleMD, Facts, Baseline, Agent, Phase, Opening, Point, Angle string
		Voice                                                         any
		Contract                                                      string
	}{state.TenantID, state.PlanID, state.RoomID, state.StyleID, state.ScorecardID, state.WheelID, state.StyleMD, state.Facts, state.BaselineJSON, state.Config.AgentID, segment.Phase, segment.OpeningVersion, segment.Task.SellingPoint, segment.Task.Angle, voice, contract})
}

func (s *Server) cycleCacheOSS() (storage.Store, error) {
	if s.assetStorage == nil {
		return nil, errors.New("OSS声音缓存未配置")
	}
	store, err := s.assetStorage.For("oss")
	if err != nil {
		return nil, errors.New("生成声音须配置OSS，不会降级为本地缓存")
	}
	return store, nil
}

func cacheUsable(e anchorvoicecache.Entry, store storage.Store) bool {
	// Do not enqueue a clip that will expire before it can finish playing.
	return e.Bucket == store.Bucket() && time.Until(e.ExpiresAt) > time.Duration(e.Bundle.AudioDurationMS)*time.Millisecond+time.Minute
}

func (s *Server) cachedCycleCandidate(ctx context.Context, state *anchorcycle.State, b anchorstylescore.Baseline) *anchorvoicecache.Entry {
	if state.PreviewLimitMS > 0 {
		return nil
	}
	// Opening/closing and explicit correction/promise handling must be generated
	// in the current context. Never let cached prose bypass those obligations.
	if s.anchorVoiceCache == nil || state.Config.TTS == nil || state.Phase != "body" || len(state.Attempts) > 0 || state.P0PendingRepair != nil || state.PriceHook != nil || state.PendingFeedback != "" {
		return nil
	}
	oss, err := s.cycleCacheOSS()
	if err != nil {
		return nil
	}
	entries, err := s.anchorVoiceCache.Lookup(ctx, cycleVoiceCacheGroup(state, anchorcycle.Segment{Phase: state.Phase, Task: state.Task()}))
	if err != nil {
		state.Warn("查询声音缓存失败，继续正常生成：" + err.Error())
		return nil
	}
	gap := max(5, state.Config.CombinationGap, state.Config.PointGap)
	for _, e := range entries {
		if !cacheUsable(e, oss) || anchorstylescore.Measure(e.OutputText, b).Chars > state.Config.MaxChars || anchorcycle.InspectPrice(e.Text, state.Task()).Hook != "" {
			continue
		}
		recent := false
		for _, prior := range state.Accepted[max(0, len(state.Accepted)-gap):] {
			if prior.Selected.Text == e.Text || prior.Selected.CacheID == e.ID {
				recent = true
				break
			}
		}
		if recent || anchorstylescore.Score(e.Text, b, state.Task().Key).OverallScore < state.Config.Threshold {
			continue
		}
		return &e
	}
	return nil
}

func (s *Server) cachedCycleVoice(ctx context.Context, state *anchorcycle.State, segment anchorcycle.Segment) (*anchorcycle.VoiceAsset, error) {
	if state.PreviewLimitMS > 0 {
		return nil, nil
	}
	if err := state.CheckApprovedText(segment.Selected, cycleVoiceText(segment.Selected)); err != nil {
		return nil, err
	}
	if s.anchorVoiceCache == nil {
		return nil, nil
	}
	oss, err := s.cycleCacheOSS()
	if err != nil {
		return nil, err
	}
	entries, err := s.anchorVoiceCache.Lookup(ctx, cycleVoiceCacheGroup(state, segment))
	if err != nil {
		return nil, err
	}
	text := cycleVoiceText(segment.Selected)
	for _, e := range entries {
		if e.OutputText != text || e.Bundle.Text != text || !cacheUsable(e, oss) {
			continue
		}
		url, err := oss.SignedURL(ctx, e.ObjectPrefix+"audio.wav", time.Until(e.ExpiresAt))
		if err != nil {
			return nil, err
		}
		e.Bundle.AudioURL = url
		return &anchorcycle.VoiceAsset{Status: "ready", CacheID: e.ID, CacheHit: true, CacheExpiresAt: e.ExpiresAt.Format(time.RFC3339Nano), Bundle: e.Bundle}, nil
	}
	return nil, nil
}

func cycleVoiceText(candidate anchorcycle.Candidate) string {
	text := candidate.OutputText
	if text == "" {
		text = candidate.Text
	}
	text = strings.TrimSpace(text)
	if !strongFullShowVoiceCut(text) {
		text += "。"
	}
	return text
}

func (s *Server) archiveCycleVoiceCache(ctx context.Context, state *anchorcycle.State, segment anchorcycle.Segment, raw []byte, timeline []model.LiveAgentPlanTimelineSegment, duration int64, retentions ...time.Duration) (*anchorcycle.VoiceAsset, error) {
	if err := state.CheckApprovedText(segment.Selected, cycleVoiceText(segment.Selected)); err != nil {
		return nil, err
	}
	if s.anchorVoiceCache == nil {
		return nil, errors.New("声音复用缓存未配置")
	}
	oss, err := s.cycleCacheOSS()
	if err != nil {
		return nil, err
	}
	retention := anchorvoicecache.DefaultTTL
	if len(retentions) > 0 {
		retention = retentions[0]
	}
	if retention < time.Hour || retention > 168*time.Hour {
		return nil, errors.New("声音缓存运维配置无效")
	}
	e := s.anchorVoiceCache.NewEntry(state.TenantID, cycleVoiceCacheGroup(state, segment), anchorcycle.ID(), oss.Bucket(), retention)
	e.Text = segment.Selected.Text
	e.OutputText = cycleVoiceText(segment.Selected)
	keys := anchorvoicecache.ObjectKeys(e)
	safe := buildFullShowSafePoints(timeline)
	srt := liveAgentTimelineSRT(timeline)
	manifest := map[string]any{"version": "formal-voice-bundle-v3", "usage": "test_preview", "published": false, "cache_id": e.ID, "cache_expires_at": e.ExpiresAt.Format(time.RFC3339Nano), "storage_driver": "oss", "storage_bucket": e.Bucket, "voice_id": state.Config.TTS.VoiceID, "voice_rate": state.Config.TTS.Rate, "tts_model": state.Config.TTS.Model, "audio_duration_ms": duration, "audio_object_key": keys[0], "srt_object_key": keys[1], "timeline_object_key": keys[2], "safe_points_object_key": keys[3], "manifest_object_key": keys[4], "timeline_precision": "segment_pcm_exact"}
	e.Bundle = model.LiveAgentPlanVersionVariant{VariantKey: "cache-" + e.ID, IsFormal: false, Text: e.OutputText, AudioDurationMS: duration, Timeline: timeline, SRT: srt, SafePoints: safe, AssetManifest: manifest, VoiceIdentityKey: state.Config.TTS.VoiceID}
	if err = validateLiveAgentVariantTimeline(e.Bundle); err != nil {
		return nil, err
	}
	if err = validateLiveAgentVariantSchedulingAssets(e.Bundle); err != nil {
		return nil, err
	}
	if err = s.anchorVoiceCache.Reserve(ctx, e); err != nil {
		return nil, err
	}
	reportCycleProgress(ctx, 75)
	jsonBytes := func(v any) []byte { data, _ := json.Marshal(v); return data }
	payloads := [][]byte{raw, []byte(srt), jsonBytes(map[string]any{"version": "formal-mainline-v3", "audio_duration_ms": duration, "sentences": timeline, "safe_points": safe}), jsonBytes(map[string]any{"version": "safe-points-v3", "safe_points": safe}), jsonBytes(manifest)}
	for i, key := range keys {
		mime := "application/json"
		if i == 0 {
			mime = "audio/wav"
		}
		if i == 1 {
			mime = "application/x-subrip; charset=utf-8"
		}
		if private, ok := oss.(interface {
			PutPrivate(context.Context, string, io.Reader, string) error
		}); ok {
			err = private.PutPrivate(ctx, key, bytes.NewReader(payloads[i]), mime)
		} else {
			err = oss.Put(ctx, key, bytes.NewReader(payloads[i]), mime)
		}
		if err != nil {
			return nil, fmt.Errorf("上传OSS声音缓存: %w", err)
		}
		reportCycleProgress(ctx, 75+20*(i+1)/len(keys))
	}
	e.Bundle.AudioURL, err = oss.SignedURL(ctx, keys[0], time.Until(e.ExpiresAt))
	if err != nil {
		return nil, err
	}
	if err = s.anchorVoiceCache.Publish(ctx, e); err != nil {
		return nil, err
	}
	reportCycleProgress(ctx, 98)
	return &anchorcycle.VoiceAsset{Status: "ready", CacheID: e.ID, CacheExpiresAt: e.ExpiresAt.Format(time.RFC3339Nano), Bundle: e.Bundle}, nil
}

func (s *Server) cleanupAnchorVoiceCache(ctx context.Context) error {
	if s.anchorVoiceCache == nil {
		return nil
	}
	oss, err := s.cycleCacheOSS()
	if err != nil {
		return err
	}
	entries, err := s.anchorVoiceCache.Due(ctx, 20)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Bucket != oss.Bucket() {
			return errors.New("声音缓存桶与当前配置不同，不删除未知桶")
		}
		if time.Now().Before(e.ExpiresAt) {
			continue
		}
		for _, key := range anchorvoicecache.ObjectKeys(e) {
			if err = oss.Delete(ctx, key); err != nil {
				return err
			}
		}
		if err = s.anchorVoiceCache.Forget(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) RunAnchorVoiceCacheCleanup(ctx context.Context) {
	if _, err := s.cycleCacheOSS(); err != nil {
		log.Printf("anchor voice cache disabled: %v", err)
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		job, cancel := context.WithTimeout(ctx, 45*time.Second)
		err := s.cleanupAnchorVoiceCache(job)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Printf("anchor voice cache cleanup failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
