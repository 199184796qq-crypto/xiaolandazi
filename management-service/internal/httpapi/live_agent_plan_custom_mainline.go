package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechasr"
	assetstorage "livecompanion/management/internal/storage"
)

const maxCustomMainlineBytes int64 = 50 << 20

type customMainlineDraftResponse struct {
	AudioAssetID  int64                                `json:"audio_asset_id"`
	AudioURL      string                               `json:"audio_url"`
	DurationMS    int64                                `json:"duration_ms"`
	Transcript    string                               `json:"transcript"`
	Timeline      []model.LiveAgentPlanTimelineSegment `json:"timeline"`
	SRT           string                               `json:"srt"`
	SafePoints    []model.LiveAgentPlanSafePoint       `json:"safe_points"`
	Manifest      map[string]any                       `json:"asset_manifest"`
	VoiceProfile  *model.VoiceProfile                  `json:"voice_profile,omitempty"`
	VoiceIdentity *model.LiveAgentVoiceIdentity        `json:"voice_identity,omitempty"`
	CloneError    string                               `json:"clone_error,omitempty"`
}

type rebuildCustomMainlineInput struct {
	RoomID       int64                                `json:"room_id"`
	AudioAssetID int64                                `json:"audio_asset_id"`
	Timeline     []model.LiveAgentPlanTimelineSegment `json:"timeline"`
}

func (s *Server) liveAgentPlanCustomMainlineUpload(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCustomMainlineBytes+(4<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "自定义音稿文件过大或上传格式错误")
		return
	}
	roomID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("room_id")), 10, 64)
	if err != nil || roomID <= 0 {
		writeError(w, http.StatusBadRequest, "请选择直播间")
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); err != nil {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择自定义主线音频")
		return
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > maxCustomMainlineBytes {
		writeError(w, http.StatusBadRequest, "自定义主线音频必须小于等于 50MB")
		return
	}
	extension := strings.ToLower(filepath.Ext(strings.TrimSpace(header.Filename)))
	contentType, allowed := speechAnalysisUploadContentTypes[extension]
	if !allowed {
		writeError(w, http.StatusBadRequest, "仅支持 WAV、MP3、M4A、AAC、FLAC、OGG、WEBM 音频")
		return
	}
	durationHint, _ := strconv.ParseInt(strings.TrimSpace(r.FormValue("duration_ms")), 10, 64)
	if durationHint < 0 {
		durationHint = 0
	}

	if s.assetStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "媒体存储尚未初始化")
		return
	}
	ossStore, err := s.assetStorage.For("oss")
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "自定义音稿需要 OSS 存储才能进行语音识别")
		return
	}

	audioObjectKey, err := newMediaObjectKey(tenantID, "audio", "custom-mainline"+extension)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成自定义音稿对象编号失败")
		return
	}
	hasher := sha256.New()
	if err := ossStore.Put(r.Context(), audioObjectKey, io.TeeReader(file, hasher), contentType); err != nil {
		writeError(w, http.StatusBadGateway, "上传自定义音稿失败")
		return
	}
	cleanupAudio := true
	defer func() {
		if cleanupAudio {
			_ = ossStore.Delete(context.Background(), audioObjectKey)
		}
	}()

	signedURL, err := ossStore.SignedURL(r.Context(), audioObjectKey, 6*time.Hour)
	if err != nil || strings.TrimSpace(signedURL) == "" {
		writeError(w, http.StatusBadGateway, "无法为自定义音稿生成语音识别地址")
		return
	}
	transcription, err := speechasr.NewFromEnv().Transcribe(r.Context(), signedURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "自定义音稿识别失败："+err.Error())
		return
	}
	timeline, transcriptText, durationMS, err := customMainlineTimelineFromASR(transcription, durationHint)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	srt := liveAgentTimelineSRT(timeline)
	safePoints := buildFullShowSafePoints(timeline)
	if len(safePoints) == 0 {
		writeError(w, http.StatusConflict, "自定义音稿没有生成可用的语义安全切点")
		return
	}

	bundle, err := writeCustomMainlineSidecars(r.Context(), ossStore, tenantID, planID, roomID, audioObjectKey, durationMS, timeline, srt, safePoints)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	durationValue := uint64(durationMS)
	asset, err := s.store.CreateMediaAsset(r.Context(), model.CreateMediaAssetInput{
		TenantID:        tenantID,
		AssetType:       "audio",
		OriginalName:    filepath.Base(header.Filename),
		StorageDriver:   ossStore.Driver(),
		StorageBucket:   ossStore.Bucket(),
		ObjectKey:       audioObjectKey,
		MIMEType:        contentType,
		SizeBytes:       uint64(header.Size),
		DurationMS:      &durationValue,
		ChecksumSHA256:  hex.EncodeToString(hasher.Sum(nil)),
		CreatedByUserID: actor.UserID,
		Metadata: map[string]any{
			"purpose":                "live_agent_custom_mainline_audio",
			"plan_id":                planID,
			"room_id":                roomID,
			"subtitle_object_key":    bundle["srt_object_key"],
			"timeline_object_key":    bundle["timeline_object_key"],
			"safe_points_object_key": bundle["safe_points_object_key"],
			"manifest_object_key":    bundle["manifest_object_key"],
			"timeline_format":        "json+srt+safe_points_v3",
			"timeline_precision":     "asr_sentence_timestamp",
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存自定义音稿资产失败")
		return
	}
	cleanupAudio = false

	response := customMainlineDraftResponse{
		AudioAssetID: asset.ID,
		AudioURL:     fmt.Sprintf("/api/v1/live/media-assets/%d/content", asset.ID),
		DurationMS:   durationMS,
		Transcript:   transcriptText,
		Timeline:     timeline,
		SRT:          srt,
		SafePoints:   safePoints,
		Manifest:     bundle,
	}

	cloneRaw := []byte(nil)
	cloneContentType := ""
	if sampleFile, sampleHeader, sampleErr := r.FormFile("voice_sample"); sampleErr == nil {
		defer sampleFile.Close()
		if sampleHeader.Size > 0 && sampleHeader.Size <= 5<<20 {
			raw, readErr := io.ReadAll(io.LimitReader(sampleFile, (5<<20)+1))
			if readErr == nil && len(raw) > 0 && len(raw) <= 5<<20 {
				cloneRaw = raw
				cloneContentType = strings.TrimSpace(sampleHeader.Header.Get("Content-Type"))
				if cloneContentType == "" {
					cloneContentType = "audio/wav"
				}
			}
		}
	}
	if len(cloneRaw) == 0 {
		if _, seekErr := file.Seek(0, io.SeekStart); seekErr == nil {
			raw, readErr := io.ReadAll(io.LimitReader(file, maxCustomMainlineBytes+1))
			if readErr == nil && len(raw) > 0 && int64(len(raw)) <= maxCustomMainlineBytes {
				cloneRaw = raw
				cloneContentType = contentType
			}
		}
	}
	if len(cloneRaw) > 0 {
		preferredName := fmt.Sprintf("v%d%d", actor.UserID%10000, time.Now().Unix()%100000000)
		dataURI := "data:" + cloneContentType + ";base64," + base64.StdEncoding.EncodeToString(cloneRaw)
		voiceID, cloneErr := cloneVoiceProfile(r.Context(), preferredName, dataURI)
		if cloneErr != nil {
			response.CloneError = cloneErr.Error()
		} else {
			profileName := strings.TrimSuffix(filepath.Base(header.Filename), extension) + " · 自定义音色"
			profile, saveErr := s.store.SaveVoiceProfile(r.Context(), tenantID, actor.UserID, 0, model.VoiceProfileInput{
				Name:          profileName,
				Provider:      "aliyun_qwen_clone",
				VoiceID:       voiceID,
				SampleAssetID: &asset.ID,
				CloneStatus:   "ready",
				Config: map[string]any{
					"target_model": qwenCloneTTSModel,
					"source":       "custom_mainline_auto_clone",
				},
			})
			if saveErr != nil {
				response.CloneError = saveErr.Error()
			} else {
				response.VoiceProfile = &profile
				identity := model.LiveAgentVoiceIdentity{
					Name:      profile.Name,
					Version:   "V1",
					Source:    "clone",
					Provider:  profile.Provider,
					VoiceID:   profile.VoiceID,
					ProfileID: profile.ID,
					Model:     qwenCloneTTSModel,
					Rate:      1,
				}
				response.VoiceIdentity = &identity
			}
		}
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) liveAgentPlanCustomMainlineRebuild(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input rebuildCustomMainlineInput
	if err := readAgentJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "自定义音稿校对参数格式错误")
		return
	}
	if input.RoomID <= 0 || input.AudioAssetID <= 0 {
		writeError(w, http.StatusBadRequest, "自定义音稿参数不完整")
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, input.RoomID)
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); err != nil {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	}
	asset, err := s.store.GetMediaAsset(r.Context(), tenantID, input.AudioAssetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "自定义音稿音频不存在")
		return
	}
	if strings.TrimSpace(fmt.Sprint(asset.Metadata["purpose"])) != "live_agent_custom_mainline_audio" {
		writeError(w, http.StatusConflict, "这个音频不是自定义主线音稿")
		return
	}
	if asset.DurationMS == nil || *asset.DurationMS == 0 {
		writeError(w, http.StatusConflict, "自定义音稿缺少音频时长")
		return
	}
	timeline, err := normalizeCustomMainlineEditedTimeline(input.Timeline, int64(*asset.DurationMS))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	srt := liveAgentTimelineSRT(timeline)
	safePoints := buildFullShowSafePoints(timeline)
	if len(safePoints) == 0 {
		writeError(w, http.StatusConflict, "校对后的音稿没有可用语义安全切点，请补充句末标点")
		return
	}
	assetStore, err := s.assetStorage.For(asset.StorageDriver)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "自定义音稿存储暂不可用")
		return
	}
	bundle, err := rewriteCustomMainlineSidecars(r.Context(), assetStore, asset, planID, input.RoomID, int64(*asset.DurationMS), timeline, srt, safePoints)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	response := customMainlineDraftResponse{
		AudioAssetID: asset.ID,
		AudioURL:     fmt.Sprintf("/api/v1/live/media-assets/%d/content", asset.ID),
		DurationMS:   int64(*asset.DurationMS),
		Transcript:   customMainlineTranscript(timeline),
		Timeline:     timeline,
		SRT:          srt,
		SafePoints:   safePoints,
		Manifest:     bundle,
	}
	profiles, _ := s.store.ListVoiceProfiles(r.Context(), tenantID)
	for index := range profiles {
		profile := profiles[index]
		if profile.SampleAssetID == nil || *profile.SampleAssetID != asset.ID || !strings.EqualFold(profile.CloneStatus, "ready") {
			continue
		}
		response.VoiceProfile = &profile
		modelName := qwenCloneTTSModel
		if candidate := strings.TrimSpace(fmt.Sprint(profile.Config["target_model"])); candidate != "" && candidate != "<nil>" {
			modelName = candidate
		}
		identity := model.LiveAgentVoiceIdentity{
			Name:      profile.Name,
			Version:   "V1",
			Source:    "clone",
			Provider:  profile.Provider,
			VoiceID:   profile.VoiceID,
			ProfileID: profile.ID,
			Model:     modelName,
			Rate:      1,
		}
		response.VoiceIdentity = &identity
		break
	}
	writeJSON(w, http.StatusOK, response)
}

func customMainlineTimelineFromASR(result speechasr.Result, durationHint int64) ([]model.LiveAgentPlanTimelineSegment, string, int64, error) {
	if strings.TrimSpace(result.Text) == "" {
		return nil, "", 0, errors.New("自定义音稿识别结果为空")
	}
	durationMS := durationHint
	for _, sentence := range result.Sentences {
		if sentence.EndTime > durationMS {
			durationMS = sentence.EndTime
		}
	}
	if durationMS <= 0 {
		return nil, "", 0, errors.New("无法确定自定义音稿时长")
	}
	segments := make([]model.LiveAgentPlanTimelineSegment, 0, len(result.Sentences))
	previousEnd := int64(0)
	for _, sentence := range result.Sentences {
		text := normalizeCustomMainlineSentence(sentence.Text)
		if text == "" {
			continue
		}
		startMS := sentence.BeginTime
		endMS := sentence.EndTime
		if startMS < previousEnd {
			startMS = previousEnd
		}
		if endMS <= startMS {
			continue
		}
		if endMS > durationMS {
			endMS = durationMS
		}
		segments = append(segments, model.LiveAgentPlanTimelineSegment{
			SegmentID: fmt.Sprintf("CUSTOM-%03d", len(segments)+1),
			Index:     len(segments) + 1,
			StartMS:   startMS,
			EndMS:     endMS,
			Text:      text,
			SafeCut:   true,
		})
		previousEnd = endMS
	}
	if len(segments) == 0 {
		segments = append(segments, model.LiveAgentPlanTimelineSegment{
			SegmentID: "CUSTOM-001",
			Index:     1,
			StartMS:   0,
			EndMS:     durationMS,
			Text:      normalizeCustomMainlineSentence(result.Text),
			SafeCut:   true,
		})
	}
	return segments, customMainlineTranscript(segments), durationMS, nil
}

func normalizeCustomMainlineEditedTimeline(input []model.LiveAgentPlanTimelineSegment, durationMS int64) ([]model.LiveAgentPlanTimelineSegment, error) {
	if len(input) == 0 || len(input) > 1024 {
		return nil, errors.New("自定义音稿分段数量无效")
	}
	result := make([]model.LiveAgentPlanTimelineSegment, 0, len(input))
	var previousEnd int64
	for index, raw := range input {
		text := normalizeCustomMainlineSentence(raw.Text)
		if text == "" {
			return nil, fmt.Errorf("第 %d 段文字不能为空", index+1)
		}
		if raw.StartMS < previousEnd || raw.StartMS < 0 || raw.EndMS <= raw.StartMS || raw.EndMS > durationMS {
			return nil, fmt.Errorf("第 %d 段时间范围无效", index+1)
		}
		result = append(result, model.LiveAgentPlanTimelineSegment{
			SegmentID: fmt.Sprintf("CUSTOM-%03d", index+1),
			Index:     index + 1,
			StartMS:   raw.StartMS,
			EndMS:     raw.EndMS,
			Text:      text,
			SafeCut:   true,
		})
		previousEnd = raw.EndMS
	}
	return result, nil
}

func normalizeCustomMainlineSentence(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	switch runes[len(runes)-1] {
	case '。', '！', '？', '!', '?', '；', ';':
		return value
	default:
		return value + "。"
	}
}

func customMainlineTranscript(timeline []model.LiveAgentPlanTimelineSegment) string {
	parts := make([]string, 0, len(timeline))
	for _, segment := range timeline {
		if text := strings.TrimSpace(segment.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func writeCustomMainlineSidecars(
	ctx context.Context,
	assetStore assetstorage.Store,
	tenantID, planID, roomID int64,
	audioObjectKey string,
	durationMS int64,
	timeline []model.LiveAgentPlanTimelineSegment,
	srt string,
	safePoints []model.LiveAgentPlanSafePoint,
) (map[string]any, error) {
	srtObjectKey, err := newMediaObjectKey(tenantID, "audio", "custom-mainline.srt")
	if err != nil {
		return nil, err
	}
	timelineObjectKey, err := newMediaObjectKey(tenantID, "audio", "custom-mainline-timeline.json")
	if err != nil {
		return nil, err
	}
	safePointsObjectKey, err := newMediaObjectKey(tenantID, "audio", "custom-mainline-safe-points.json")
	if err != nil {
		return nil, err
	}
	manifestObjectKey, err := newMediaObjectKey(tenantID, "audio", "custom-mainline-manifest.json")
	if err != nil {
		return nil, err
	}
	manifest := map[string]any{
		"version":                "custom-mainline-bundle-v1",
		"plan_id":                planID,
		"room_id":                roomID,
		"audio_object_key":       audioObjectKey,
		"srt_object_key":         srtObjectKey,
		"timeline_object_key":    timelineObjectKey,
		"safe_points_object_key": safePointsObjectKey,
		"manifest_object_key":    manifestObjectKey,
		"audio_duration_ms":      durationMS,
		"segment_count":          len(timeline),
		"safe_point_count":       len(safePoints),
		"timeline_precision":     "asr_sentence_timestamp",
	}
	if err := putCustomMainlineSidecars(ctx, assetStore, manifest, timeline, srt, safePoints); err != nil {
		return nil, err
	}
	return manifest, nil
}

func rewriteCustomMainlineSidecars(
	ctx context.Context,
	assetStore assetstorage.Store,
	asset model.MediaAsset,
	planID, roomID, durationMS int64,
	timeline []model.LiveAgentPlanTimelineSegment,
	srt string,
	safePoints []model.LiveAgentPlanSafePoint,
) (map[string]any, error) {
	manifest := map[string]any{
		"version":                "custom-mainline-bundle-v1",
		"plan_id":                planID,
		"room_id":                roomID,
		"audio_object_key":       asset.ObjectKey,
		"srt_object_key":         strings.TrimSpace(fmt.Sprint(asset.Metadata["subtitle_object_key"])),
		"timeline_object_key":    strings.TrimSpace(fmt.Sprint(asset.Metadata["timeline_object_key"])),
		"safe_points_object_key": strings.TrimSpace(fmt.Sprint(asset.Metadata["safe_points_object_key"])),
		"manifest_object_key":    strings.TrimSpace(fmt.Sprint(asset.Metadata["manifest_object_key"])),
		"audio_duration_ms":      durationMS,
		"segment_count":          len(timeline),
		"safe_point_count":       len(safePoints),
		"timeline_precision":     "asr_sentence_timestamp",
	}
	for _, key := range []string{"srt_object_key", "timeline_object_key", "safe_points_object_key", "manifest_object_key"} {
		if value := strings.TrimSpace(fmt.Sprint(manifest[key])); value == "" || value == "<nil>" {
			return nil, errors.New("自定义音稿调度资产不完整，请重新上传")
		}
	}
	if err := putCustomMainlineSidecars(ctx, assetStore, manifest, timeline, srt, safePoints); err != nil {
		return nil, err
	}
	return manifest, nil
}

func putCustomMainlineSidecars(
	ctx context.Context,
	assetStore assetstorage.Store,
	manifest map[string]any,
	timeline []model.LiveAgentPlanTimelineSegment,
	srt string,
	safePoints []model.LiveAgentPlanSafePoint,
) error {
	timelineRaw, _ := json.MarshalIndent(map[string]any{
		"version":     "custom-mainline-timeline-v1",
		"sentences":   timeline,
		"safe_points": safePoints,
	}, "", "  ")
	safePointsRaw, _ := json.MarshalIndent(map[string]any{
		"version":     "safe-points-v3",
		"safe_points": safePoints,
	}, "", "  ")
	manifestRaw, _ := json.MarshalIndent(manifest, "", "  ")
	items := []struct {
		key         string
		raw         []byte
		contentType string
	}{
		{strings.TrimSpace(fmt.Sprint(manifest["srt_object_key"])), []byte(srt), "application/x-subrip; charset=utf-8"},
		{strings.TrimSpace(fmt.Sprint(manifest["timeline_object_key"])), timelineRaw, "application/json; charset=utf-8"},
		{strings.TrimSpace(fmt.Sprint(manifest["safe_points_object_key"])), safePointsRaw, "application/json; charset=utf-8"},
		{strings.TrimSpace(fmt.Sprint(manifest["manifest_object_key"])), manifestRaw, "application/json; charset=utf-8"},
	}
	for _, item := range items {
		if err := assetStore.Put(ctx, item.key, bytes.NewReader(item.raw), item.contentType); err != nil {
			return fmt.Errorf("写入自定义音稿调度资产失败: %w", err)
		}
	}
	return nil
}
