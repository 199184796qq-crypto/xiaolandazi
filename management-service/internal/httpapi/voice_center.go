package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

const (
	qwenSystemTTSModel = "qwen3-tts-flash"
	qwenCloneTTSModel  = "qwen3-tts-vc-2026-01-22"
	maxCloneAudioBytes = 20 << 20
)

type officialVoice struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Gender      string   `json:"gender"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Model       string   `json:"model"`
}

var officialVoices = []officialVoice{
	{ID: "Cherry", Name: "芊悦", Gender: "女声", Description: "阳光积极、亲切自然", Tags: []string{"自然", "亲切", "活力"}, Model: qwenSystemTTSModel},
	{ID: "Serena", Name: "苏瑶", Gender: "女声", Description: "温柔、柔和、亲近", Tags: []string{"温柔", "柔和"}, Model: qwenSystemTTSModel},
	{ID: "Ethan", Name: "晨煦", Gender: "男声", Description: "阳光、温暖、有活力", Tags: []string{"温暖", "活力"}, Model: qwenSystemTTSModel},
	{ID: "Chelsie", Name: "千雪", Gender: "女声", Description: "年轻、轻快、二次元风格", Tags: []string{"年轻", "轻快"}, Model: qwenSystemTTSModel},
	{ID: "Momo", Name: "茉兔", Gender: "女声", Description: "活泼、俏皮、有互动感", Tags: []string{"活泼", "俏皮"}, Model: qwenSystemTTSModel},
}

func findOfficialVoice(id string) (officialVoice, bool) {
	id = strings.TrimSpace(id)
	for _, item := range officialVoices {
		if item.ID == id {
			return item, true
		}
	}
	return officialVoice{}, false
}

func (s *Server) liveOfficialVoices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": officialVoices})
}

type voicePreviewInput struct {
	Text string `json:"text"`
}

func (s *Server) liveOfficialVoicePreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	voiceID := strings.TrimSpace(r.PathValue("voiceID"))
	voice, ok := findOfficialVoice(voiceID)
	if !ok {
		writeError(w, http.StatusNotFound, "官方声音不存在")
		return
	}
	var input voicePreviewInput
	_ = json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&input)
	text := strings.TrimSpace(input.Text)
	if text == "" {
		text = "大家好，欢迎来到直播间，很高兴今天和大家见面。"
	}
	audioURL, err := synthesizeVoicePreview(r.Context(), voice.Model, voice.ID, text)
	if err != nil {
		writeError(w, http.StatusBadGateway, "声音试听生成失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audio_url": audioURL})
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
	tenantID, ok := actorTenantID(w, actor)
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
	var input voicePreviewInput
	_ = json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&input)
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
	writeJSON(w, http.StatusOK, map[string]any{"audio_url": audioURL})
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
