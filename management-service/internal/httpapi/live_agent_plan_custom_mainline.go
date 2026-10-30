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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechasr"
	assetstorage "livecompanion/management/internal/storage"
	"livecompanion/management/internal/ttsgateway"
)

const (
	maxCustomMainlineBytes      int64 = 200 << 20
	maxCustomMainlineTotalBytes int64 = 500 << 20
	maxCustomMainlineFiles            = 20
)

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

	r.Body = http.MaxBytesReader(w, r.Body, maxCustomMainlineTotalBytes+(8<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "自定义音稿文件过大或上传格式错误")
		return
	}
	defer r.MultipartForm.RemoveAll()
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); err != nil {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	}

	headers := r.MultipartForm.File["file"]
	if len(headers) == 0 {
		writeError(w, http.StatusBadRequest, "请选择自定义主线音频")
		return
	}
	if len(headers) > maxCustomMainlineFiles {
		writeError(w, http.StatusBadRequest, "一次最多上传20段录音")
		return
	}
	var totalSourceBytes int64
	sourceNames := make([]string, 0, len(headers))
	contentTypes := make([]string, 0, len(headers))
	for _, header := range headers {
		if header.Size <= 0 || header.Size > maxCustomMainlineBytes {
			writeError(w, http.StatusBadRequest, "每段自定义主线音频必须小于等于200MB")
			return
		}
		totalSourceBytes += header.Size
		extension := strings.ToLower(filepath.Ext(strings.TrimSpace(header.Filename)))
		contentType, allowed := speechAnalysisUploadContentTypes[extension]
		if !allowed {
			writeError(w, http.StatusBadRequest, "仅支持 WAV、MP3、M4A、AAC、FLAC、OGG、WEBM 音频")
			return
		}
		sourceNames = append(sourceNames, filepath.Base(header.Filename))
		contentTypes = append(contentTypes, contentType)
	}
	if totalSourceBytes > maxCustomMainlineTotalBytes {
		writeError(w, http.StatusBadRequest, "多段录音总大小必须小于等于500MB")
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

	sources := make([]customMainlineSource, 0, len(headers))
	for _, header := range headers {
		sources = append(sources, customMainlineSource{Name: header.Filename, Size: header.Size, Open: func(context.Context) (io.ReadCloser, error) { return header.Open() }})
	}
	var sample []byte
	if file, header, err := r.FormFile("voice_sample"); err == nil {
		defer file.Close()
		if header.Size > 0 && header.Size <= 5<<20 {
			sample, _ = io.ReadAll(io.LimitReader(file, (5<<20)+1))
			if len(sample) > 5<<20 {
				sample = nil
			}
		}
	}
	s.processCustomMainlineSources(w, r, tenantID, planID, actor.UserID, sources, sourceNames, contentTypes, totalSourceBytes, durationHint, sample, ossStore)
}

type customMainlineSource struct {
	Name string
	Size int64
	Open func(context.Context) (io.ReadCloser, error)
}

// Shared by legacy multipart uploads and the durable direct-OSS processing queue.
func (s *Server) processCustomMainlineSources(w http.ResponseWriter, r *http.Request, tenantID, planID, userID int64, sources []customMainlineSource, sourceNames, contentTypes []string, totalSourceBytes, durationHint int64, voiceSample []byte, ossStore assetstorage.Store) {
	roomID := int64(0)
	normalizedPaths := make([]string, 0, len(sources))
	defer func() {
		for _, path := range normalizedPaths {
			_ = os.Remove(path)
		}
	}()
	for _, source := range sources {
		file, err := source.Open(r.Context())
		if err != nil {
			writeError(w, http.StatusBadRequest, "读取自定义音稿失败")
			return
		}
		extension := strings.ToLower(filepath.Ext(strings.TrimSpace(source.Name)))
		sourcePath, copyErr := copyAudioReaderToTemp(io.LimitReader(file, source.Size+1), extension)
		if copyErr == nil {
			info, statErr := os.Stat(sourcePath)
			if statErr != nil || info.Size() != source.Size {
				_ = os.Remove(sourcePath)
				copyErr = errors.New("原始音频大小与上传信息不一致")
			}
		}
		_ = file.Close()
		if copyErr != nil {
			writeError(w, http.StatusBadRequest, "读取自定义音稿失败")
			return
		}
		normalizedPath, normalizeErr := normalizeAudioToCoreWAV(r.Context(), sourcePath)
		_ = os.Remove(sourcePath)
		if normalizeErr != nil {
			for _, path := range normalizedPaths {
				_ = os.Remove(path)
			}
			writeError(w, http.StatusBadRequest, normalizeErr.Error())
			return
		}
		normalizedPaths = append(normalizedPaths, normalizedPath)
	}
	// Existing playback requires a standard RIFF/WAVE asset. Reject overflow
	// before FFmpeg can write a truncated 32-bit data length.
	var combinedPCMBytes int64
	for _, path := range normalizedPaths {
		file, err := os.Open(path)
		if err != nil {
			writeError(w, 500, "读取规范化录音失败")
			return
		}
		_, size, err := customPCMData(file)
		_ = file.Close()
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		combinedPCMBytes += size
	}
	if combinedPCMBytes > (1<<32)-1024 {
		writeError(w, 400, "本批录音转换后超过单条WAV音轨4GB上限，请分批上传处理")
		return
	}
	normalizedPath, err := concatenateCoreWAVs(r.Context(), normalizedPaths)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(normalizedPaths) > 1 {
		defer os.Remove(normalizedPath)
	}
	normalizedInfo, err := os.Stat(normalizedPath)
	if err != nil || normalizedInfo.Size() <= 44 {
		writeError(w, http.StatusInternalServerError, "读取规范化后的自定义音稿失败")
		return
	}

	audioObjectKey, err := newMediaObjectKey(tenantID, "audio", "custom-mainline.wav")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成自定义音稿对象编号失败")
		return
	}
	normalizedFile, err := os.Open(normalizedPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "打开规范化后的自定义音稿失败")
		return
	}
	hasher := sha256.New()
	putErr := ossStore.Put(r.Context(), audioObjectKey, io.TeeReader(normalizedFile, hasher), "audio/wav")
	_ = normalizedFile.Close()
	if putErr != nil {
		writeError(w, http.StatusBadGateway, "上传自定义音稿失败")
		return
	}
	cleanupAudio := true
	defer func() {
		if cleanupAudio {
			_ = ossStore.Delete(context.Background(), audioObjectKey)
		}
	}()

	partPrefix, _ := r.Context().Value(customASRPrefixKey{}).(string)
	transcription, actualDuration, err := transcribeCustomPCM(r.Context(), ossStore, normalizedPath, audioObjectKey, partPrefix, customASRChunkBytes, speechasr.NewFromEnv().Transcribe)
	if err != nil {
		writeError(w, http.StatusBadGateway, "自定义音稿识别失败："+err.Error())
		return
	}
	if actualDuration > 0 {
		durationHint = actualDuration
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
	originalName := strings.TrimSuffix(sourceNames[0], filepath.Ext(sourceNames[0])) + ".wav"
	if len(sourceNames) > 1 {
		originalName = fmt.Sprintf("%s-等%d段合并.wav", strings.TrimSuffix(sourceNames[0], filepath.Ext(sourceNames[0])), len(sourceNames))
	}
	asset, err := s.store.CreateMediaAsset(r.Context(), model.CreateMediaAssetInput{
		TenantID:        tenantID,
		AssetType:       "audio",
		OriginalName:    originalName,
		StorageDriver:   ossStore.Driver(),
		StorageBucket:   ossStore.Bucket(),
		ObjectKey:       audioObjectKey,
		MIMEType:        "audio/wav",
		SizeBytes:       uint64(normalizedInfo.Size()),
		DurationMS:      &durationValue,
		ChecksumSHA256:  hex.EncodeToString(hasher.Sum(nil)),
		CreatedByUserID: userID,
		Metadata: map[string]any{
			"purpose":                "live_agent_custom_mainline_audio",
			"plan_id":                planID,
			"room_id":                roomID,
			"source_original_name":   sourceNames[0],
			"source_original_names":  sourceNames,
			"source_content_types":   contentTypes,
			"source_size_bytes":      totalSourceBytes,
			"source_file_count":      len(sourceNames),
			"playback_format":        "pcm_s16le_24k_mono",
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

	cloneRaw := voiceSample
	cloneContentType := "audio/wav"
	if len(cloneRaw) == 0 {
		if raw, sampleErr := extractCloneSampleFromAudioPath(r.Context(), normalizedPath); sampleErr == nil {
			cloneRaw = raw
			cloneContentType = "audio/wav"
		}
	}
	voiceConfig, configErr := s.store.VoiceModelSettings(r.Context())
	if configErr != nil {
		response.CloneError = "读取声音配置失败，录音主线已保存，请稍后重新生成声音"
	}
	if len(cloneRaw) > 0 && configErr == nil {
		cloneModel := voiceConfig.CustomModel
		preferredName := fmt.Sprintf("v%d%d", userID%10000, time.Now().Unix()%100000000)
		dataURI := "data:" + cloneContentType + ";base64," + base64.StdEncoding.EncodeToString(cloneRaw)
		tempObjectKey := fmt.Sprintf("tenants/%d/voice-samples/tmp/%d-%d.wav", tenantID, userID, time.Now().UnixNano())
		cloneErr := ossStore.Put(r.Context(), tempObjectKey, bytes.NewReader(cloneRaw), cloneContentType)
		var sampleURL string
		if cloneErr == nil {
			defer ossStore.Delete(r.Context(), tempObjectKey)
			sampleURL, cloneErr = ossStore.SignedURL(r.Context(), tempObjectKey, 15*time.Minute)
			if cloneErr == nil && strings.TrimSpace(sampleURL) == "" {
				cloneErr = errors.New("生成声音复刻公网样本地址失败")
			}
		}
		var voiceID string
		if cloneErr == nil {
			voiceID, cloneErr = cloneVoiceProfile(r.Context(), preferredName, sampleURL, dataURI, cloneModel)
		}
		if cloneErr != nil {
			response.CloneError = cloneErr.Error()
		} else {
			profileName := strings.TrimSuffix(sourceNames[0], filepath.Ext(sourceNames[0])) + " · 自定义音色"
			profile, saveErr := s.store.SaveVoiceProfile(r.Context(), tenantID, userID, 0, model.VoiceProfileInput{
				Name:          profileName,
				Provider:      ttsgateway.ProviderForModel(cloneModel),
				VoiceID:       voiceID,
				SampleAssetID: &asset.ID,
				CloneStatus:   "ready",
				Config: map[string]any{
					"target_model": cloneModel,
					"source":       "custom_mainline_auto_clone",
				},
			})
			if saveErr != nil {
				response.CloneError = saveErr.Error()
			} else {
				binding, bindingErr := s.store.CreateVoiceModelBinding(r.Context(), tenantID, userID, model.VoiceModelBindingInput{
					ProfileID: profile.ID, SampleAssetID: asset.ID, Provider: ttsgateway.ProviderForModel(cloneModel),
					Model: cloneModel, VoiceID: profile.VoiceID, Rate: 1, Status: "ready",
					Config: map[string]any{"source": "custom_mainline_auto_clone"},
				})
				if bindingErr != nil {
					response.CloneError = bindingErr.Error()
				} else {
					response.VoiceProfile = &profile
					identity := model.LiveAgentVoiceIdentity{
						Name:      profile.Name,
						Version:   "V1",
						Source:    "clone",
						Provider:  profile.Provider,
						VoiceID:   profile.VoiceID,
						ProfileID: profile.ID,
						BindingID: binding.ID,
						Model:     cloneModel,
						Rate:      1,
					}
					response.VoiceIdentity = &identity
				}
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
	if input.AudioAssetID <= 0 {
		writeError(w, http.StatusBadRequest, "自定义音稿参数不完整")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return
	}
	input.RoomID = 0
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); err != nil {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	}
	asset, err := s.store.GetMediaAsset(r.Context(), tenantID, input.AudioAssetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "自定义音稿音频不存在")
		return
	}
	if strings.TrimSpace(fmt.Sprint(asset.Metadata["purpose"])) != "live_agent_custom_mainline_audio" || mediaMetadataInt64(asset.Metadata["plan_id"]) != planID {
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
