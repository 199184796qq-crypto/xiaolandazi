package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/voicecatalog"
)

const (
	qwenCloneTTSModel      = "qwen-audio-3.0-tts-plus"
	maxCloneAudioBytes     = 20 << 20
	maxGeneratedVoiceBytes = 32 << 20
	voicePreviewText       = "你好，我是你的主播，欢迎为你效劳。"
)

const (
	qwenCloneModelPlus  = "qwen-audio-3.0-tts-plus"
	qwenCloneModelFlash = "qwen-audio-3.1-tts-flash"
)

func normalizeVoiceCloneModel(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case qwenCloneModelPlus, qwenCloneModelFlash:
		return value, nil
	default:
		return "", errors.New("当前复刻模型暂不支持")
	}
}

func voiceProfileConfiguredRate(profile model.VoiceProfile) float64 {
	if profile.Config == nil {
		return 1
	}
	raw, ok := profile.Config["rate"]
	if !ok {
		return 1
	}
	switch value := raw.(type) {
	case float64:
		if value >= 0.5 && value <= 2 {
			return value
		}
	case float32:
		rate := float64(value)
		if rate >= 0.5 && rate <= 2 {
			return rate
		}
	case int:
		rate := float64(value)
		if rate >= 0.5 && rate <= 2 {
			return rate
		}
	case json.Number:
		if rate, err := value.Float64(); err == nil && rate >= 0.5 && rate <= 2 {
			return rate
		}
	case string:
		if rate, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && rate >= 0.5 && rate <= 2 {
			return rate
		}
	}
	return 1
}

type officialVoice = voicecatalog.Voice

var officialVoices = voicecatalog.Official()

func findOfficialVoice(id string) (officialVoice, bool) {
	return voicecatalog.Find(id)
}

func (s *Server) liveOfficialVoices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": officialVoices})
}

type voicePreviewInput struct {
	Text     string `json:"text"`
	TenantID int64  `json:"tenant_id,omitempty"`
}

func (s *Server) liveOfficialVoicePreview(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	var input voicePreviewInput
	_ = json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&input)
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	voiceID := strings.TrimSpace(r.PathValue("voiceID"))
	voice, ok := findOfficialVoice(voiceID)
	if !ok {
		writeError(w, http.StatusNotFound, "官方声音不存在")
		return
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		text = voicePreviewText
	}
	audioURL, err := synthesizeVoicePreview(r.Context(), voice.Model, voice.ID, text)
	if err != nil {
		writeError(w, http.StatusBadGateway, "声音试听生成失败："+err.Error())
		return
	}
	assetID, durationMS, err := s.archiveGeneratedVoice(
		r.Context(), tenantID, actor.UserID, audioURL, voice.ID, voice.Model, "official",
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "声音已生成，但归档到声音资产失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"audio_url":      audioURL,
		"audio_asset_id": assetID,
		"duration_ms":    durationMS,
	})
}

type cloneVoiceInput struct {
	Name          string `json:"name"`
	SampleAssetID int64  `json:"sample_asset_id"`
	Model         string `json:"model,omitempty"`
	AvatarKey     string `json:"avatar_key,omitempty"`
}

func validVoiceAvatarKey(value string) bool {
	switch strings.TrimSpace(value) {
	case "female_young", "female_middle", "female_senior",
		"male_young", "male_middle", "male_senior",
		"cartoon_female", "cartoon_male":
		return true
	default:
		return false
	}
}

func (s *Server) liveCloneVoiceProfile(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	var input cloneVoiceInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "声音克隆参数格式错误")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || input.SampleAssetID <= 0 {
		writeError(w, http.StatusBadRequest, "请填写声音名称并上传声音样本")
		return
	}
	limit, err := s.store.GetEffectiveCustomerRoomLimit(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取自定义声音额度失败")
		return
	}
	used, err := s.store.CountActiveVoiceProfiles(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取自定义声音数量失败")
		return
	}
	if used >= limit {
		writeError(w, http.StatusConflict, fmt.Sprintf("自定义声音已达到直播间权限上限（%d 个），请先删除一个已有声音再生成", limit))
		return
	}
	modelName := strings.TrimSpace(input.Model)
	if modelName == "" {
		modelName = qwenCloneTTSModel
	}
	modelName, err = normalizeVoiceCloneModel(modelName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.AvatarKey = strings.TrimSpace(input.AvatarKey)
	if input.AvatarKey == "" {
		input.AvatarKey = "female_young"
	}
	if !validVoiceAvatarKey(input.AvatarKey) {
		writeError(w, http.StatusBadRequest, "请选择一个声音头像")
		return
	}

	asset, err := s.store.GetMediaAsset(r.Context(), tenantID, input.SampleAssetID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "声音样本不存在")
		return
	}
	if !s.requireLiveSupportMedia(w, r, actor, asset) {
		return
	}
	if asset.AssetType != "voice_sample" && asset.AssetType != "audio" {
		writeError(w, http.StatusBadRequest, "请选择声音样本文件")
		return
	}
	assetStore, err := s.assetStorage.For(asset.StorageDriver)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "声音样本存储暂不可用")
		return
	}
	reader, err := assetStore.Open(r.Context(), asset.ObjectKey)
	if err != nil {
		writeError(w, http.StatusNotFound, "声音样本文件不存在")
		return
	}
	defer reader.Close()

	isCustomMainline := strings.TrimSpace(fmt.Sprint(asset.Metadata["purpose"])) == "live_agent_custom_mainline_audio"
	var raw []byte
	var sampleDurationMS int64
	mimeType := strings.TrimSpace(asset.MIMEType)
	if isCustomMainline {
		tempPath, tempErr := copyAudioReaderToTemp(reader, filepath.Ext(asset.OriginalName))
		if tempErr != nil {
			writeError(w, http.StatusBadRequest, "读取自定义音稿声音样本失败")
			return
		}
		defer os.Remove(tempPath)
		raw, err = extractCloneSampleFromAudioPath(r.Context(), tempPath)
		mimeType = "audio/wav"
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		sampleDurationMS, _ = generatedVoiceWAVDurationMS(raw)
	} else {
		if asset.SizeBytes > maxCloneAudioBytes {
			writeError(w, http.StatusBadRequest, "普通声音样本不能超过 20MB；自定义音稿会自动截取短样本")
			return
		}
		tempPath, tempErr := copyAudioReaderToTemp(reader, filepath.Ext(asset.OriginalName))
		if tempErr != nil {
			writeError(w, http.StatusBadRequest, "读取声音样本失败")
			return
		}
		defer os.Remove(tempPath)
		raw, sampleDurationMS, err = normalizeUploadedVoiceSample(r.Context(), tempPath)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		mimeType = "audio/wav"
	}
	dataURI := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(raw)
	preferredName := fmt.Sprintf("v%d%d", actor.UserID%10000, time.Now().Unix()%100000000)
	tempObjectKey := fmt.Sprintf("tenants/%d/voice-samples/tmp/%d-%d.wav", tenantID, actor.UserID, time.Now().UnixNano())
	if err := assetStore.Put(r.Context(), tempObjectKey, bytes.NewReader(raw), mimeType); err != nil {
		writeError(w, http.StatusBadGateway, "上传声音复刻临时样本失败")
		return
	}
	defer assetStore.Delete(r.Context(), tempObjectKey)
	sampleURL, err := assetStore.SignedURL(r.Context(), tempObjectKey, 15*time.Minute)
	if err != nil || strings.TrimSpace(sampleURL) == "" {
		writeError(w, http.StatusBadGateway, "生成声音复刻公网样本地址失败")
		return
	}

	voiceID, err := cloneVoiceProfile(r.Context(), preferredName, sampleURL, dataURI, modelName)
	if err != nil {
		writeError(w, http.StatusBadGateway, "声音复刻失败："+err.Error())
		return
	}
	supportRoomID, _ := liveSupportScopeRoomID(r)
	profile, err := s.store.SaveVoiceProfile(r.Context(), tenantID, actor.UserID, 0, model.VoiceProfileInput{
		Name:          input.Name,
		Provider:      "aliyun_qwen_clone",
		VoiceID:       voiceID,
		SampleAssetID: &input.SampleAssetID,
		CloneStatus:   "ready",
		Config: map[string]any{
			"support_room_id":          supportRoomID,
			"target_model":             modelName,
			"source":                   "customer_voice_clone",
			"avatar_key":               input.AvatarKey,
			"sample_duration_ms":       sampleDurationMS,
			"normalized_sample_format": "wav_pcm_s16le_mono_16000",
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存克隆声音失败")
		return
	}
	binding, err := s.store.CreateVoiceModelBinding(r.Context(), tenantID, actor.UserID, model.VoiceModelBindingInput{
		ProfileID: profile.ID, SampleAssetID: input.SampleAssetID, Provider: ttsgateway.ProviderQwen,
		Model: modelName, VoiceID: profile.VoiceID, Rate: 1, Status: "ready",
		Config: map[string]any{"source": "customer_voice_clone"},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存声音模型绑定失败")
		return
	}
	if profile.Config == nil {
		profile.Config = map[string]any{}
	}
	profile.Config["binding_id"] = binding.ID
	writeJSON(w, http.StatusCreated, profile)
}

type cloneVoiceModelBindingInput struct {
	Model string `json:"model"`
}

func (s *Server) liveListVoiceModelBindings(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	profileID := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("profile_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			writeError(w, http.StatusBadRequest, "声音档案编号无效")
			return
		}
		profileID = value
	}
	items, err := s.store.ListVoiceModelBindings(r.Context(), tenantID, profileID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取声音模型绑定失败")
		return
	}
	if actor.IsInternalStaff() {
		roomID, _ := liveSupportScopeRoomID(r)
		filtered := make([]model.VoiceModelBinding, 0, len(items))
		for _, item := range items {
			profile, err := s.store.GetVoiceProfile(r.Context(), tenantID, item.ProfileID)
			if err == nil && s.liveSupportVoiceAllowed(r.Context(), tenantID, roomID, profile) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) liveCloneVoiceModelBinding(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	profileID, ok := namedPathID(w, r, "profileID", "声音档案")
	if !ok {
		return
	}
	var input cloneVoiceModelBindingInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "声音模型参数格式错误")
		return
	}
	modelName, err := normalizeVoiceCloneModel(input.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	profile, err := s.store.GetVoiceProfile(r.Context(), tenantID, profileID)
	if err != nil || profile.SampleAssetID == nil || *profile.SampleAssetID <= 0 {
		writeError(w, http.StatusBadRequest, "这个声音档案还没有可用样本")
		return
	}
	if !s.requireLiveSupportVoice(w, r, actor, tenantID, profile, false) {
		return
	}
	binding, err := s.cloneVoiceModelBindingForProfile(r.Context(), tenantID, actor.UserID, profile, modelName, voiceProfileConfiguredRate(profile))
	if err != nil {
		writeError(w, http.StatusBadGateway, "生成模型音色失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, binding)
}

func (s *Server) liveVoiceModelBindingPreview(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	bindingID, ok := namedPathID(w, r, "bindingID", "声音模型绑定")
	if !ok {
		return
	}
	binding, err := s.store.GetVoiceModelBinding(r.Context(), tenantID, bindingID)
	if err != nil || !strings.EqualFold(strings.TrimSpace(binding.Status), "ready") {
		writeError(w, http.StatusNotFound, "声音模型绑定不存在或尚未准备好")
		return
	}
	if actor.IsInternalStaff() {
		profile, err := s.store.GetVoiceProfile(r.Context(), tenantID, binding.ProfileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "声音不存在")
			return
		}
		if !s.requireLiveSupportVoice(w, r, actor, tenantID, profile, false) {
			return
		}
	}
	var input voicePreviewInput
	_ = json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&input)
	text := strings.TrimSpace(input.Text)
	if text == "" {
		text = voicePreviewText
	}
	result, err := ttsgateway.NewFromEnv().SynthesizeURL(r.Context(), ttsgateway.SynthesizeRequest{
		Provider: binding.Provider, Model: binding.Model, VoiceID: binding.VoiceID, Text: text, Rate: binding.Rate,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "声音试听生成失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audio_url": result.AudioURL})
}

func (s *Server) cloneVoiceModelBindingForProfile(
	ctx context.Context, tenantID, userID int64, profile model.VoiceProfile, targetModel string, rate float64,
) (model.VoiceModelBinding, error) {
	if profile.SampleAssetID == nil || *profile.SampleAssetID <= 0 {
		return model.VoiceModelBinding{}, errors.New("声音档案缺少人工样本")
	}
	asset, err := s.store.GetMediaAsset(ctx, tenantID, *profile.SampleAssetID)
	if err != nil {
		return model.VoiceModelBinding{}, err
	}
	assetStore, err := s.assetStorage.For(asset.StorageDriver)
	if err != nil {
		return model.VoiceModelBinding{}, err
	}
	reader, err := assetStore.Open(ctx, asset.ObjectKey)
	if err != nil {
		return model.VoiceModelBinding{}, err
	}
	defer reader.Close()
	isCustomMainline := strings.TrimSpace(fmt.Sprint(asset.Metadata["purpose"])) == "live_agent_custom_mainline_audio"
	var raw []byte
	mimeType := strings.TrimSpace(asset.MIMEType)
	if isCustomMainline {
		tempPath, tempErr := copyAudioReaderToTemp(reader, filepath.Ext(asset.OriginalName))
		if tempErr != nil {
			return model.VoiceModelBinding{}, tempErr
		}
		defer os.Remove(tempPath)
		raw, err = extractCloneSampleFromAudioPath(ctx, tempPath)
		mimeType = "audio/wav"
	} else {
		if asset.SizeBytes > maxCloneAudioBytes {
			return model.VoiceModelBinding{}, errors.New("普通声音样本不能超过 20MB")
		}
		raw, err = io.ReadAll(io.LimitReader(reader, maxCloneAudioBytes+1))
		if mimeType == "" {
			mimeType = "audio/wav"
		}
	}
	if err != nil || len(raw) == 0 || len(raw) > maxCloneAudioBytes {
		return model.VoiceModelBinding{}, errors.New("读取声音样本失败")
	}
	dataURI := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(raw)
	preferredName := fmt.Sprintf("v%d%d", userID%10000, time.Now().Unix()%100000000)
	tempObjectKey := fmt.Sprintf("tenants/%d/voice-samples/tmp/%d-%d.wav", tenantID, userID, time.Now().UnixNano())
	if err := assetStore.Put(ctx, tempObjectKey, bytes.NewReader(raw), mimeType); err != nil {
		return model.VoiceModelBinding{}, err
	}
	defer assetStore.Delete(ctx, tempObjectKey)
	sampleURL, err := assetStore.SignedURL(ctx, tempObjectKey, 15*time.Minute)
	if err != nil || strings.TrimSpace(sampleURL) == "" {
		return model.VoiceModelBinding{}, errors.New("生成声音复刻公网样本地址失败")
	}
	voiceID, err := cloneVoiceProfile(ctx, preferredName, sampleURL, dataURI, targetModel)
	if err != nil {
		return model.VoiceModelBinding{}, err
	}
	binding, err := s.store.CreateVoiceModelBinding(ctx, tenantID, userID, model.VoiceModelBindingInput{
		ProfileID: profile.ID, SampleAssetID: *profile.SampleAssetID, Provider: ttsgateway.ProviderQwen,
		Model: targetModel, VoiceID: voiceID, Rate: rate, Status: "ready", Config: map[string]any{"source": "customer_voice_clone"},
	})
	if err != nil {
		return model.VoiceModelBinding{}, err
	}
	return binding, nil
}

func (s *Server) liveVoiceProfilePreview(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	var input voicePreviewInput
	_ = json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&input)
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	profileID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("profileID")), 10, 64)
	if err != nil || profileID <= 0 {
		writeError(w, http.StatusBadRequest, "声音编号无效")
		return
	}
	profile, err := s.store.GetVoiceProfile(r.Context(), tenantID, profileID)
	if err != nil {
		writeError(w, http.StatusNotFound, "声音不存在")
		return
	}
	if !s.requireLiveSupportVoice(w, r, actor, tenantID, profile, false) {
		return
	}
	if profile.CloneStatus != "ready" || strings.TrimSpace(profile.VoiceID) == "" {
		writeError(w, http.StatusConflict, "这个声音还没有准备好")
		return
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		text = voicePreviewText
	}
	modelName := qwenCloneTTSModel
	if value, exists := profile.Config["target_model"]; exists {
		if candidate := strings.TrimSpace(fmt.Sprint(value)); candidate != "" {
			modelName = candidate
		}
	}
	audioURL, err := synthesizeVoicePreview(r.Context(), modelName, profile.VoiceID, text)
	if err != nil {
		writeError(w, http.StatusBadGateway, "声音试听生成失败："+err.Error())
		return
	}
	assetID, durationMS, err := s.archiveGeneratedVoice(
		r.Context(), tenantID, actor.UserID, audioURL, profile.VoiceID, modelName, "clone",
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "声音已生成，但归档到声音资产失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"audio_url":      audioURL,
		"audio_asset_id": assetID,
		"duration_ms":    durationMS,
	})
}

func (s *Server) archiveGeneratedVoice(
	ctx context.Context,
	tenantID, actorUserID int64,
	providerURL, voiceID, modelName, source string,
) (int64, int64, error) {
	if s.assetStorage == nil {
		return 0, 0, fmt.Errorf("媒体存储尚未初始化")
	}
	providerURL = strings.TrimSpace(providerURL)
	if providerURL == "" {
		return 0, 0, fmt.Errorf("声音地址为空")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, providerURL, nil)
	if err != nil {
		return 0, 0, err
	}
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("下载生成声音: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, 0, fmt.Errorf("下载生成声音 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxGeneratedVoiceBytes+1))
	if err != nil {
		return 0, 0, fmt.Errorf("读取生成声音: %w", err)
	}
	if len(raw) == 0 || len(raw) > maxGeneratedVoiceBytes {
		return 0, 0, fmt.Errorf("生成声音为空或超过 32MB")
	}
	durationMS, err := generatedVoiceWAVDurationMS(raw)
	if err != nil {
		return 0, 0, fmt.Errorf("读取生成声音时长: %w", err)
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" || contentType == "application/octet-stream" {
		limit := len(raw)
		if limit > 512 {
			limit = 512
		}
		contentType = http.DetectContentType(raw[:limit])
	}
	objectKey, err := newMediaObjectKey(tenantID, "generated_voice", "generated.wav")
	if err != nil {
		return 0, 0, err
	}
	assetStore := s.assetStorage.Current()
	if ossStore, ossErr := s.assetStorage.For("oss"); ossErr == nil {
		assetStore = ossStore
	}
	if err := assetStore.Put(ctx, objectKey, bytes.NewReader(raw), contentType); err != nil {
		return 0, 0, fmt.Errorf("写入声音存储: %w", err)
	}
	digest := sha256.Sum256(raw)
	durationValue := uint64(durationMS)
	item, err := s.store.CreateMediaAsset(ctx, model.CreateMediaAssetInput{
		TenantID:        tenantID,
		AssetType:       "generated_voice",
		OriginalName:    "generated.wav",
		StorageDriver:   assetStore.Driver(),
		StorageBucket:   assetStore.Bucket(),
		ObjectKey:       objectKey,
		MIMEType:        contentType,
		SizeBytes:       uint64(len(raw)),
		DurationMS:      &durationValue,
		ChecksumSHA256:  hex.EncodeToString(digest[:]),
		CreatedByUserID: actorUserID,
		Metadata: map[string]any{
			"source":    strings.TrimSpace(source),
			"voice_id":  strings.TrimSpace(voiceID),
			"tts_model": strings.TrimSpace(modelName),
			"purpose":   "live_agent_formal_voice_candidate",
		},
	})
	if err != nil {
		_ = assetStore.Delete(ctx, objectKey)
		return 0, 0, fmt.Errorf("保存声音资产记录: %w", err)
	}
	return item.ID, durationMS, nil
}

func generatedVoiceWAVDurationMS(audio []byte) (int64, error) {
	if len(audio) < 12 || string(audio[:4]) != "RIFF" || string(audio[8:12]) != "WAVE" {
		return 0, errors.New("生成声音不是 RIFF/WAVE 文件")
	}
	var byteRate uint32
	var dataSize uint32
	for offset := 12; offset+8 <= len(audio); {
		chunkID := string(audio[offset : offset+4])
		chunkSize := binary.LittleEndian.Uint32(audio[offset+4 : offset+8])
		dataStart := offset + 8
		dataEnd := dataStart + int(chunkSize)
		streamingData := chunkID == "data" && dataEnd > len(audio)
		if dataEnd > len(audio) && !streamingData {
			return 0, errors.New("生成声音 WAV 分块无效")
		}
		switch chunkID {
		case "fmt ":
			if chunkSize < 16 || dataStart+16 > len(audio) {
				return 0, errors.New("生成声音 WAV fmt 分块无效")
			}
			byteRate = binary.LittleEndian.Uint32(audio[dataStart+8 : dataStart+12])
		case "data":
			if streamingData {
				dataSize = uint32(len(audio) - dataStart)
				dataEnd = len(audio)
			} else {
				dataSize = chunkSize
			}
		}
		if byteRate > 0 && dataSize > 0 {
			break
		}
		offset = dataEnd
		if chunkSize%2 == 1 {
			offset++
		}
	}
	if byteRate == 0 || dataSize == 0 {
		return 0, errors.New("生成声音 WAV 缺少 fmt 或 data 分块")
	}
	durationMS := int64((uint64(dataSize)*1000 + uint64(byteRate)/2) / uint64(byteRate))
	if durationMS <= 0 {
		return 0, errors.New("生成声音时长无效")
	}
	return durationMS, nil
}

func synthesizeVoicePreview(ctx context.Context, modelName, voiceID, text string) (string, error) {
	result, err := ttsgateway.NewFromEnv().SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{
		Model:   modelName,
		VoiceID: voiceID,
		Text:    text,
		Rate:    1,
	})
	if err != nil {
		return "", err
	}
	return result.AudioURL, nil
}

func cloneVoiceProfile(ctx context.Context, preferredName, audioURL, audioData, targetModel string) (string, error) {
	result, err := ttsgateway.NewFromEnv().CloneVoice(ctx, ttsgateway.CloneRequest{
		PreferredName: preferredName,
		AudioURL:      audioURL,
		AudioData:     audioData,
		TargetModel:   targetModel,
	})
	if err != nil {
		return "", err
	}
	return result.VoiceID, nil
}
