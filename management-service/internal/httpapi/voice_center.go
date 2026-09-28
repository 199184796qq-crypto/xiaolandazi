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
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/voicecatalog"
)

const (
	qwenCloneTTSModel      = "qwen3-tts-vc-2026-01-22"
	maxCloneAudioBytes     = 20 << 20
	maxGeneratedVoiceBytes = 32 << 20
)

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
		text = "大家好，欢迎来到直播间，很高兴今天和大家见面。"
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
}

func (s *Server) liveCloneVoiceProfile(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := actorTenantID(w, actor)
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

	asset, err := s.store.GetMediaAsset(r.Context(), tenantID, input.SampleAssetID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "声音样本不存在")
		return
	}
	if asset.AssetType != "voice_sample" && asset.AssetType != "audio" {
		writeError(w, http.StatusBadRequest, "请选择声音样本文件")
		return
	}
	if asset.SizeBytes > maxCloneAudioBytes {
		writeError(w, http.StatusBadRequest, "声音样本不能超过 20MB")
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
	raw, err := io.ReadAll(io.LimitReader(reader, maxCloneAudioBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxCloneAudioBytes {
		writeError(w, http.StatusBadRequest, "读取声音样本失败")
		return
	}
	mimeType := strings.TrimSpace(asset.MIMEType)
	if mimeType == "" {
		mimeType = "audio/mpeg"
	}
	dataURI := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(raw)
	preferredName := fmt.Sprintf("v%d%d", actor.UserID%10000, time.Now().Unix()%100000000)

	voiceID, err := cloneVoiceProfile(r.Context(), preferredName, dataURI)
	if err != nil {
		writeError(w, http.StatusBadGateway, "声音复刻失败："+err.Error())
		return
	}
	profile, err := s.store.SaveVoiceProfile(r.Context(), tenantID, actor.UserID, 0, model.VoiceProfileInput{
		Name:          input.Name,
		Provider:      "aliyun_qwen_clone",
		VoiceID:       voiceID,
		SampleAssetID: &input.SampleAssetID,
		CloneStatus:   "ready",
		Config: map[string]any{
			"target_model": qwenCloneTTSModel,
			"source":       "customer_voice_clone",
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存克隆声音失败")
		return
	}
	writeJSON(w, http.StatusCreated, profile)
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
	if profile.CloneStatus != "ready" || strings.TrimSpace(profile.VoiceID) == "" {
		writeError(w, http.StatusConflict, "这个声音还没有准备好")
		return
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		text = "大家好，欢迎来到直播间，这是我的声音试听。"
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
	})
	if err != nil {
		return "", err
	}
	return result.AudioURL, nil
}

func cloneVoiceProfile(ctx context.Context, preferredName, audioData string) (string, error) {
	result, err := ttsgateway.NewFromEnv().CloneVoice(ctx, ttsgateway.CloneRequest{
		PreferredName: preferredName,
		AudioData:     audioData,
		TargetModel:   qwenCloneTTSModel,
	})
	if err != nil {
		return "", err
	}
	return result.VoiceID, nil
}
