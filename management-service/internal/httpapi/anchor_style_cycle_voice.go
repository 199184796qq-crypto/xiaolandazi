package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

// No caller-supplied text or room target: only an adopted segment in the scoped
// run can synthesize. Complete bundles are cached in Redis for idempotent retry.
func (s *Server) anchorStyleCycleVoice(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenant, plan, room, ok := s.anchorCycleScope(w, r, actor)
	if !ok {
		return
	}
	var input struct {
		Retry bool `json:"retry"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "声音请求格式错误")
		return
	}
	state, err := s.anchorCycles.Get(r.Context(), tenant, plan, room)
	if err != nil {
		cycleError(w, err)
		return
	}
	if state.RunID != r.PathValue("runID") {
		cycleError(w, anchorcycle.ErrStale)
		return
	}
	if !s.requireCycleRunAccess(w, r, actor, state) {
		return
	}
	if state.StreamControlled {
		writeError(w, 409, "本场声音由服务器生成并连续输出")
		return
	}
	w, r = cycleProgressStream(w, r)
	voice, err := s.synthesizeAnchorCycleVoice(r.Context(), state, r.PathValue("candidateID"), input.Retry)
	if err != nil {
		if errors.Is(err, anchorcycle.ErrPreviewBudgetExhausted) {
			cycleVoiceCompleteReply(w, state.RunID, r.PathValue("candidateID"))
			return
		}
		cycleError(w, err)
		return
	}
	cycleVoiceReplyForRequest(w, r, state, r.PathValue("candidateID"), voice)
}

// The same adoption, compliance, OSS and SRT/semantic bundle path serves both
// preview and live production; no direct HTTP-handler calls inside the producer.
func (s *Server) synthesizeAnchorCycleVoice(ctx context.Context, state *anchorcycle.State, candidateID string, retry bool) (*anchorcycle.VoiceAsset, error) {
	voices, ok := s.anchorCycles.(anchorcycle.VoiceRepository)
	if !ok {
		return nil, fmt.Errorf("异步声音队列未配置")
	}
	tenant, plan, room := state.TenantID, state.PlanID, state.RoomID
	if state.Config.TTS == nil {
		return nil, fmt.Errorf("%s", "本场没有启用TTS，请在新场开始前打开开关")
	}
	index := -1
	for i, segment := range state.Accepted {
		if segment.Selected.ID == candidateID {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, fmt.Errorf("%s", "没有这份已采纳话术，未采纳候选不会转声音")
	}
	segment := &state.Accepted[index]
	if err := checkCycleVoiceP0(state, *segment); err != nil {
		return nil, fmt.Errorf("%s", err.Error())
	}
	if segment.Voice != nil && segment.Voice.Status == "ready" {
		reportCycleProgress(ctx, 100)
		return segment.Voice, nil
	}
	voice := state.Config.TTS
	if _, err := s.cycleCacheOSS(); err != nil {
		return nil, fmt.Errorf("%s", err.Error())
	}
	retention, retentionErr := s.anchorVoiceCacheTTL(ctx)
	if retentionErr != nil {
		return nil, fmt.Errorf("%s", "读取声音缓存运维配置失败，未调用TTS")
	}
	voiceID, modelName, err := s.resolveFormalVoice(ctx, tenant, fullShowVariantVoiceInput{Source: voice.Source, VoiceID: voice.VoiceID, ProfileID: voice.ProfileID, Model: voice.Model})
	if err != nil || voiceID != voice.VoiceID || modelName != voice.Model {
		return nil, fmt.Errorf("%s", "本场选定音色已变化或不可用，请重新开始，不会自动更换音色")
	}
	job := anchorcycle.ID()
	ctx, done := s.cycleJobContext(ctx, job, tenant, room, state.RunID, state.Surface != "formal_live" && !state.StreamControlled)
	defer done()
	ctx, cancel := context.WithTimeout(ctx, 240*time.Second)
	defer cancel()
	claimed, owned, err := voices.ClaimVoice(ctx, tenant, plan, room, state.RunID, segment.Selected.ID, job, retry)
	if err != nil {
		return nil, err
	}
	if !owned {
		return claimed, nil
	}
	segment.Voice = claimed
	reportCycleProgress(ctx, 8)
	started := time.Now()
	cached, cacheErr := s.cachedCycleVoice(ctx, state, *segment)
	if cacheErr != nil {
		state.Warn("声音缓存查询失败，继续合成：" + cacheErr.Error())
	}
	if cached != nil {
		cached.LatencyMS = time.Since(started).Milliseconds()
		if err := voices.CompleteVoice(ctx, tenant, plan, room, state.RunID, segment.Selected.ID, job, *cached); err != nil {
			return nil, err
		}
		reportCycleProgress(ctx, 100)
		return cached, nil
	}
	text := cycleVoiceText(segment.Selected)
	gateway := ttsgateway.NewFromEnv()
	qwenConfig := ttsgateway.QwenConfigFromEnv()
	if qwenConfig.APIKey == "" {
		// Reuse the explicitly selected Ali credential from the system; never
		// expose it to the browser or require a second plaintext key.
		profile, e := s.store.ResolveSpeechModel(ctx, state.Config.CredentialID)
		if e != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
			err = fmt.Errorf("系统阿里TTS凭据不可用")
		} else {
			qwenConfig.APIKey = profile.APIKey
			gateway.Register(ttsgateway.NewQwenProvider(qwenConfig), "qwen", "dashscope", "qwen_audio_3_0")
		}
	}
	var raw []byte
	var timeline []model.LiveAgentPlanTimelineSegment
	var duration int64
	if err == nil {
		scoped := withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan, RoomID: room, RunID: state.RunID, CandidateID: segment.Selected.ID})
		raw, timeline, duration, err = synthesizeMainlinePreview(scoped, journaledMainlineSynthesizer{s: s, gateway: gateway, profile: state.Config.CredentialID}, *voice, text, segment.Selected.ID)
	}
	var archive *anchorcycle.VoiceAsset
	if err == nil {
		// Keep the adopted, product-audited text and its complete audio together.
		// The browser projection/content endpoint alone clips the remaining
		// listening budget; rewriting OutputText here invalidates product review.
		archive, err = s.archiveCycleVoiceCache(ctx, state, *segment, raw, timeline, duration, retention)
	}
	if err != nil {
		segment.Voice.Status, segment.Voice.Error = "failed", err.Error()
	} else {
		segment.Voice = archive
		segment.Voice.PreviewReservedMS = claimed.PreviewReservedMS
	}
	segment.Voice.LatencyMS = time.Since(started).Milliseconds()
	persist, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), s.cyclePersistenceTimeout())
	defer cancelPersist()
	if err := voices.CompleteVoice(persist, tenant, plan, room, state.RunID, segment.Selected.ID, job, *segment.Voice); err != nil {
		return nil, err
	}
	if segment.Voice.Status == "ready" {
		reportCycleProgress(ctx, 100)
	}
	return segment.Voice, nil
}
func cycleVoiceReply(w http.ResponseWriter, run, candidate string, voice *anchorcycle.VoiceAsset) {
	writeJSON(w, 200, map[string]any{"run_id": run, "candidate_id": candidate, "voice": voice})
}

func cycleVoiceReplyForRequest(w http.ResponseWriter, r *http.Request, state *anchorcycle.State, candidate string, voice *anchorcycle.VoiceAsset) {
	if voice == nil {
		cycleVoiceReply(w, state.RunID, candidate, nil)
		return
	}
	cycleVoiceReply(w, state.RunID, candidate, cycleVoiceForBrowser(r, state, candidate, voice))
}

func cycleVoiceCompleteReply(w http.ResponseWriter, run, candidate string) {
	writeJSON(w, 200, map[string]any{"run_id": run, "candidate_id": candidate, "voice": nil, "preview_complete": true})
}

func (s *Server) anchorStyleCycleVoiceGet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenant, plan, room, ok := s.anchorCycleScope(w, r, actor)
	if !ok {
		return
	}
	state, err := s.anchorCycles.Get(r.Context(), tenant, plan, room)
	if err != nil {
		cycleError(w, err)
		return
	}
	if state.RunID != r.PathValue("runID") {
		cycleError(w, anchorcycle.ErrStale)
		return
	}
	if !s.requireCycleRunAccess(w, r, actor, state) {
		return
	}
	for _, segment := range state.Accepted {
		if segment.Selected.ID == r.PathValue("candidateID") {
			if err := checkCycleVoiceP0(state, segment); err != nil {
				writeError(w, 409, err.Error())
				return
			}
			cycleVoiceReplyForRequest(w, r, state, segment.Selected.ID, segment.Voice)
			return
		}
	}
	writeError(w, 404, "没有这份已采纳话术")
}

// anchorStyleCycleVoiceContent is the only browser download path for transient
// cycle audio. It deliberately does not redirect to a signed public OSS URL:
// the authenticated server reads OSS over its internal endpoint and streams a
// same-origin response. Direct-to-OSS remains reserved for upload workflows.
func (s *Server) anchorStyleCycleVoiceContent(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenant, plan, room, ok := s.anchorCycleScope(w, r, actor)
	if !ok {
		return
	}
	state, err := s.anchorCycles.Get(r.Context(), tenant, plan, room)
	if err != nil {
		cycleError(w, err)
		return
	}
	if state.RunID != r.PathValue("runID") {
		cycleError(w, anchorcycle.ErrStale)
		return
	}
	if !s.requireCycleRunAccess(w, r, actor, state) {
		return
	}
	if state.StreamControlled {
		writeError(w, 409, "请使用本场连续声音流，不能下载单段文件")
		return
	}
	var voice *anchorcycle.VoiceAsset
	for _, segment := range state.Accepted {
		if segment.Selected.ID != r.PathValue("candidateID") {
			continue
		}
		if err := checkCycleVoiceP0(state, segment); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		voice = segment.Voice
		break
	}
	if voice == nil || voice.Status != "ready" {
		writeError(w, http.StatusNotFound, "试听声音尚未生成")
		return
	}
	objectKey, bucket, err := cycleVoiceCacheObject(voice)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	ossStore, err := s.cycleCacheOSS()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if bucket != ossStore.Bucket() {
		writeError(w, http.StatusConflict, "试听声音不属于当前媒体存储")
		return
	}
	reader, err := ossStore.Open(r.Context(), objectKey)
	if err != nil {
		writeError(w, http.StatusNotFound, "试听声音文件不存在或已过期")
		return
	}
	defer reader.Close()
	var playback io.Reader = reader
	if state.PreviewLimitMS > 0 && voice.PreviewReservedMS > 0 && voice.Bundle.AudioDurationMS > voice.PreviewReservedMS {
		const maxPreviewBytes = 64 << 20
		raw, err := io.ReadAll(io.LimitReader(reader, maxPreviewBytes+1))
		if err != nil || len(raw) > maxPreviewBytes {
			writeError(w, http.StatusBadGateway, "试听声音读取失败")
			return
		}
		raw, err = hardLimitPreviewWAV(raw, voice.PreviewReservedMS)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		playback = bytes.NewReader(raw)
		w.Header().Set("Content-Length", fmt.Sprint(len(raw)))
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Disposition", `inline; filename="realtime-preview.wav"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, playback)
}

func cycleVoiceCacheObject(voice *anchorcycle.VoiceAsset) (string, string, error) {
	if voice == nil || voice.Status != "ready" {
		return "", "", errors.New("试听声音尚未生成")
	}
	manifest := voice.Bundle.AssetManifest
	driver, _ := manifest["storage_driver"].(string)
	bucket, _ := manifest["storage_bucket"].(string)
	objectKey, _ := manifest["audio_object_key"].(string)
	cacheID := strings.TrimSpace(voice.CacheID)
	if cacheID == "" {
		cacheID, _ = manifest["cache_id"].(string)
		cacheID = strings.TrimSpace(cacheID)
	}
	if driver != "oss" || bucket == "" || len(cacheID) != 32 ||
		!strings.HasPrefix(objectKey, "anchor-voice-cache/v1/") ||
		!strings.HasSuffix(objectKey, "/"+cacheID+"/audio.wav") ||
		strings.Contains(objectKey, "..") {
		return "", "", errors.New("试听声音资产引用无效")
	}
	for _, char := range cacheID {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return "", "", errors.New("试听声音资产引用无效")
		}
	}
	return objectKey, bucket, nil
}

// This gate runs before returning an existing ready bundle or claiming any TTS
// job. Old runs without a policy cannot bypass it through Redis/OSS cache.
func checkCycleVoiceP0(state *anchorcycle.State, segment anchorcycle.Segment) error {
	if err := state.CheckProductVoice(segment); err != nil {
		return err
	}
	if err := state.CheckP0Voice(cycleVoiceText(segment.Selected)); err != nil {
		return err
	}
	if segment.Voice != nil && segment.Voice.Status == "ready" {
		voice := segment.Voice
		limitedPrefix := state.PreviewLimitMS > 0 && voice.PreviewTruncated && voice.PreviewReservedMS > 0 && voice.Bundle.Text != "" && strings.HasPrefix(cycleVoiceText(segment.Selected), voice.Bundle.Text) && voice.Bundle.AudioDurationMS > 0 && voice.Bundle.AudioDurationMS <= voice.PreviewReservedMS+anchorcycle.PreviewSafeOverrunMS
		if limitedPrefix && voice.Bundle.AudioDurationMS > voice.PreviewReservedMS {
			timeline := voice.Bundle.Timeline
			limitedPrefix = len(timeline) > 0 && timeline[len(timeline)-1].SafeCut && timeline[len(timeline)-1].EndMS == voice.Bundle.AudioDurationMS
		}
		if segment.Voice.Bundle.Text != cycleVoiceText(segment.Selected) && !limitedPrefix {
			return fmt.Errorf("声音正文与已采纳稿不一致，不输出声音")
		}
		return state.CheckP0Voice(segment.Voice.Bundle.Text)
	}
	return nil
}

type mainlineSynthesizer interface {
	SynthesizeURL(context.Context, ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error)
}

type journaledMainlineSynthesizer struct {
	s       *Server
	gateway mainlineSynthesizer
	profile string
}

func (j journaledMainlineSynthesizer) SynthesizeURL(ctx context.Context, request ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	id, err := j.s.beginModelExchange(ctx, "mainline_tts", j.profile, request)
	if err != nil {
		return ttsgateway.SynthesizeResponse{}, err
	}
	started := time.Now()
	response, err := j.gateway.SynthesizeURL(ctx, request)
	err = j.s.finishModelExchange(ctx, id, response, "", started, 0, 0, err)
	return response, err
}

func synthesizeMainlinePreview(ctx context.Context, gateway mainlineSynthesizer, voice anchorcycle.VoiceConfig, text, key string) ([]byte, []model.LiveAgentPlanTimelineSegment, int64, error) {
	pieces := []generatedVoiceWAVPiece{}
	parts := splitFullShowVoiceText(text)
	for i, part := range parts {
		generated, err := gateway.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{Model: voice.Model, VoiceID: voice.VoiceID, Text: part, Rate: voice.Rate})
		if err != nil {
			return nil, nil, 0, fmt.Errorf("第%d个声音片段生成失败: %w", i+1, err)
		}
		reportCycleProgress(ctx, 10+(60*i+30)/len(parts))
		raw, err := downloadGeneratedVoiceWAV(ctx, generated.AudioURL)
		if err != nil {
			return nil, nil, 0, err
		}
		format, pcm, rate, err := parseGeneratedVoiceWAV(raw)
		if err != nil {
			return nil, nil, 0, err
		}
		pieces = append(pieces, generatedVoiceWAVPiece{text: part, fmtData: format, pcmData: pcm, byteRate: rate})
		reportCycleProgress(ctx, 10+60*(i+1)/len(parts))
	}
	raw, timeline, duration, err := combineGeneratedVoiceWAV("test-"+key, pieces)
	if err == nil {
		reportCycleProgress(ctx, 72)
	}
	return raw, timeline, duration, err
}
