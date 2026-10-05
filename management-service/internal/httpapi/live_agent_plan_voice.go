package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	"unicode/utf8"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/voicecatalog"
)

const (
	maxFullShowVoiceSegments = 64
	maxVoiceSegmentRunes     = 80
	minVoiceSegmentRunes     = 18
)

type fullShowVariantVoiceInput struct {
	TenantID  int64   `json:"tenant_id,omitempty"`
	RoomID    int64   `json:"room_id"`
	Text      string  `json:"text"`
	Source    string  `json:"source"`
	VoiceID   string  `json:"voice_id,omitempty"`
	ProfileID int64   `json:"profile_id,omitempty"`
	Rate      float64 `json:"rate,omitempty"`
}

type fullShowVariantSubtitleRebuildInput struct {
	TenantID     int64                                `json:"tenant_id,omitempty"`
	RoomID       int64                                `json:"room_id"`
	AudioAssetID int64                                `json:"audio_asset_id"`
	Timeline     []model.LiveAgentPlanTimelineSegment `json:"timeline"`
}

type generatedVoiceWAVPiece struct {
	text     string
	fmtData  []byte
	pcmData  []byte
	byteRate uint32
}

func (s *Server) liveAgentPlanFullShowVariantVoice(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	variantKey := strings.ToUpper(strings.TrimSpace(r.PathValue("variantKey")))
	if variantKey != "A" && variantKey != "B" && variantKey != "C" && variantKey != "D" && variantKey != "E" {
		writeError(w, http.StatusBadRequest, "稿件编号无效")
		return
	}
	var input fullShowVariantVoiceInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "生成正式声音参数格式错误")
		return
	}
	input.Text = strings.TrimSpace(input.Text)
	input.Source = strings.ToLower(strings.TrimSpace(input.Source))
	input.VoiceID = strings.TrimSpace(input.VoiceID)
	if input.RoomID <= 0 || input.Text == "" {
		writeError(w, http.StatusBadRequest, "直播间和稿件正文不能为空")
		return
	}
	if utf8.RuneCountInString(input.Text) > 12000 {
		writeError(w, http.StatusBadRequest, "单稿正文过长")
		return
	}
	if input.Rate == 0 {
		input.Rate = 1
	}
	if input.Rate < 0.5 || input.Rate > 2 {
		writeError(w, http.StatusBadRequest, "声音语速必须在 0.5 到 2.0 之间")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, input.RoomID) {
		return
	}
	plan, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}
	bound := false
	for _, roomID := range plan.RoomIDs {
		if roomID == input.RoomID {
			bound = true
			break
		}
	}
	if !bound {
		writeError(w, http.StatusBadRequest, "当前直播方案没有绑定到所选直播间")
		return
	}

	if actor.IsInternalStaff() && input.Source == "clone" {
		if !s.requireLiveSupportSelectedVoice(w, r, actor, tenantID, map[string]any{"profile_id": input.ProfileID, "voice_id": input.VoiceID}) {
			return
		}
	}
	voiceID, modelName, err := s.resolveFormalVoice(r.Context(), tenantID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	segments := splitFullShowVoiceText(input.Text)
	if len(segments) == 0 {
		writeError(w, http.StatusBadRequest, "稿件没有可生成的语义段")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 240*time.Second)
	defer cancel()
	gateway := ttsgateway.NewFromEnv()
	pieces := make([]generatedVoiceWAVPiece, 0, len(segments))
	for index, segmentText := range segments {
		generated, synthErr := gateway.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{
			Model: modelName, VoiceID: voiceID, Text: segmentText, Rate: input.Rate,
		})
		if synthErr != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("%s稿第%d段声音生成失败：%v", variantKey, index+1, synthErr))
			return
		}
		raw, downloadErr := downloadGeneratedVoiceWAV(ctx, generated.AudioURL)
		if downloadErr != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("%s稿第%d段声音下载失败：%v", variantKey, index+1, downloadErr))
			return
		}
		fmtData, pcmData, byteRate, parseErr := parseGeneratedVoiceWAV(raw)
		if parseErr != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("%s稿第%d段声音格式无效：%v", variantKey, index+1, parseErr))
			return
		}
		pieces = append(pieces, generatedVoiceWAVPiece{
			text: segmentText, fmtData: fmtData, pcmData: pcmData, byteRate: byteRate,
		})
	}
	combined, timeline, durationMS, err := combineGeneratedVoiceWAV(variantKey, pieces)
	if err != nil {
		writeError(w, http.StatusBadGateway, "合成正式声音时间轴失败："+err.Error())
		return
	}
	archive, err := s.archiveFullShowVoice(
		ctx, tenantID, actor.UserID, planID, input.RoomID, variantKey,
		voiceID, modelName, input.Source, combined, durationMS, timeline,
		input.Rate,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "正式声音归档失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"audio_url":      archive.PreviewURL,
		"audio_asset_id": archive.AssetID,
		"duration_ms":    durationMS,
		"timeline":       timeline,
		"segment_count":  len(timeline),
		"srt":            archive.SRT,
		"safe_points":    archive.SafePoints,
		"asset_manifest": archive.Manifest,
	})
}

func (s *Server) liveAgentPlanFullShowVariantSubtitleRebuild(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	variantKey := strings.ToUpper(strings.TrimSpace(r.PathValue("variantKey")))
	if variantKey != "A" && variantKey != "B" && variantKey != "C" && variantKey != "D" && variantKey != "E" {
		writeError(w, http.StatusBadRequest, "稿件编号无效")
		return
	}
	var input fullShowVariantSubtitleRebuildInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "SRT 校对参数格式错误")
		return
	}
	if input.RoomID <= 0 || input.AudioAssetID <= 0 || len(input.Timeline) == 0 {
		writeError(w, http.StatusBadRequest, "SRT 校对参数不完整")
		return
	}
	if len(input.Timeline) > 1024 {
		writeError(w, http.StatusBadRequest, "SRT 分段过多")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, input.RoomID) {
		return
	}
	plan, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}
	bound := false
	for _, roomID := range plan.RoomIDs {
		if roomID == input.RoomID {
			bound = true
			break
		}
	}
	if !bound {
		writeError(w, http.StatusBadRequest, "当前直播方案没有绑定到所选直播间")
		return
	}
	asset, err := s.store.GetMediaAsset(r.Context(), tenantID, input.AudioAssetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "当前声音资产不存在")
		return
	}
	if strings.TrimSpace(fmt.Sprint(asset.Metadata["purpose"])) != "live_agent_formal_voice_timeline" ||
		mediaMetadataInt64(asset.Metadata["plan_id"]) != planID ||
		mediaMetadataInt64(asset.Metadata["room_id"]) != input.RoomID ||
		!strings.EqualFold(strings.TrimSpace(fmt.Sprint(asset.Metadata["variant_key"])), variantKey) {
		writeError(w, http.StatusConflict, "当前声音资产与这篇稿件不匹配")
		return
	}
	if asset.DurationMS == nil || *asset.DurationMS == 0 {
		writeError(w, http.StatusConflict, "当前声音资产缺少时长")
		return
	}
	assetStore, err := s.assetStorage.For(asset.StorageDriver)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "声音存储暂不可用")
		return
	}
	originalTimeline, err := readFormalVoiceTimelineSidecar(r.Context(), assetStore, asset)
	if err != nil {
		writeError(w, http.StatusConflict, "当前声音时间轴不可读取，请重新生成声音")
		return
	}
	updatedTimeline, err := rebuildFormalVoiceSubtitleTimeline(originalTimeline, input.Timeline)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	reader, err := assetStore.Open(r.Context(), asset.ObjectKey)
	if err != nil {
		writeError(w, http.StatusConflict, "当前声音文件不可读取，请重新生成声音")
		return
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, maxGeneratedVoiceBytes+1))
	_ = reader.Close()
	if readErr != nil || len(raw) == 0 || len(raw) > maxGeneratedVoiceBytes {
		writeError(w, http.StatusConflict, "当前声音文件不可读取，请重新生成声音")
		return
	}
	voiceID := strings.TrimSpace(fmt.Sprint(asset.Metadata["voice_id"]))
	modelName := strings.TrimSpace(fmt.Sprint(asset.Metadata["tts_model"]))
	source := strings.TrimSpace(fmt.Sprint(asset.Metadata["source"]))
	if voiceID == "" || voiceID == "<nil>" || modelName == "" || modelName == "<nil>" {
		writeError(w, http.StatusConflict, "当前声音身份信息不完整，请重新生成声音")
		return
	}
	if source == "" || source == "<nil>" {
		source = "official"
	}
	durationMS := int64(*asset.DurationMS)
	var archivedRates []float64
	if rate, ok := archivedVoiceRate(asset.Metadata); ok {
		archivedRates = []float64{rate}
	}
	archive, err := s.archiveFullShowVoice(
		r.Context(), tenantID, actor.UserID, planID, input.RoomID, variantKey,
		voiceID, modelName, source, raw, durationMS, updatedTimeline,
		archivedRates...,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "保存 SRT 校对结果失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"audio_url":      archive.PreviewURL,
		"audio_asset_id": archive.AssetID,
		"duration_ms":    durationMS,
		"timeline":       updatedTimeline,
		"segment_count":  len(updatedTimeline),
		"srt":            archive.SRT,
		"safe_points":    archive.SafePoints,
		"asset_manifest": archive.Manifest,
	})
}

func mediaMetadataInt64(value any) int64 {
	raw := strings.TrimSpace(fmt.Sprint(value))
	if raw == "" || raw == "<nil>" {
		return 0
	}
	parsed, _ := strconv.ParseInt(strings.TrimSuffix(raw, ".0"), 10, 64)
	return parsed
}

func readFormalVoiceTimelineSidecar(
	ctx context.Context,
	assetStore interface {
		Open(context.Context, string) (io.ReadCloser, error)
	},
	asset model.MediaAsset,
) ([]model.LiveAgentPlanTimelineSegment, error) {
	objectKey := strings.TrimSpace(fmt.Sprint(asset.Metadata["timeline_object_key"]))
	if objectKey == "" || objectKey == "<nil>" {
		return nil, errors.New("timeline sidecar missing")
	}
	reader, err := assetStore.Open(ctx, objectKey)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	var payload struct {
		Sentences []model.LiveAgentPlanTimelineSegment `json:"sentences"`
	}
	if err := json.NewDecoder(io.LimitReader(reader, 4<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	if len(payload.Sentences) == 0 {
		return nil, errors.New("timeline sidecar empty")
	}
	return payload.Sentences, nil
}

func rebuildFormalVoiceSubtitleTimeline(
	original []model.LiveAgentPlanTimelineSegment,
	edited []model.LiveAgentPlanTimelineSegment,
) ([]model.LiveAgentPlanTimelineSegment, error) {
	if len(original) == 0 || len(original) != len(edited) {
		return nil, errors.New("SRT 分段数量与当前声音不一致")
	}
	result := make([]model.LiveAgentPlanTimelineSegment, 0, len(original))
	for index := range original {
		base := original[index]
		candidate := edited[index]
		if strings.TrimSpace(candidate.SegmentID) != strings.TrimSpace(base.SegmentID) ||
			candidate.Index != base.Index ||
			candidate.StartMS != base.StartMS ||
			candidate.EndMS != base.EndMS {
			return nil, fmt.Errorf("第 %d 段时间不能修改，只允许校对文字", index+1)
		}
		text := strings.TrimSpace(candidate.Text)
		if text == "" {
			return nil, fmt.Errorf("第 %d 段字幕文字不能为空", index+1)
		}
		base.Text = text
		base.SafeCut = strongFullShowVoiceCut(text)
		result = append(result, base)
	}
	return result, nil
}

func (s *Server) resolveFormalVoice(ctx context.Context, tenantID int64, input fullShowVariantVoiceInput) (string, string, error) {
	switch input.Source {
	case "", "official":
		voice, ok := voicecatalog.Find(input.VoiceID)
		if !ok {
			return "", "", errors.New("官方声音不存在")
		}
		return voice.ID, voice.Model, nil
	case "clone":
		if input.ProfileID <= 0 {
			return "", "", errors.New("克隆声音缺少声音档案")
		}
		profile, err := s.store.GetVoiceProfile(ctx, tenantID, input.ProfileID)
		if err != nil {
			return "", "", errors.New("克隆声音档案不存在")
		}
		if !strings.EqualFold(strings.TrimSpace(profile.CloneStatus), "ready") || strings.TrimSpace(profile.VoiceID) == "" {
			return "", "", errors.New("克隆声音还没有准备好")
		}
		modelName := qwenCloneTTSModel
		if value := strings.TrimSpace(fmt.Sprint(profile.Config["target_model"])); value != "" && value != "<nil>" {
			modelName = value
		}
		return strings.TrimSpace(profile.VoiceID), modelName, nil
	default:
		return "", "", errors.New("声音来源无效")
	}
}

func splitFullShowVoiceText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var result []string
	buf := make([]rune, 0, maxVoiceSegmentRunes)
	flush := func() {
		value := strings.TrimSpace(string(buf))
		if value != "" {
			result = append(result, value)
		}
		buf = buf[:0]
	}
	for _, r := range []rune(text) {
		if r == '\r' {
			continue
		}
		buf = append(buf, r)
		length := len(buf)
		strong := strings.ContainsRune("。！？!?；;\n", r)
		soft := strings.ContainsRune("，,、：:", r)
		if (strong && length >= minVoiceSegmentRunes) || (soft && length >= 48) || length >= maxVoiceSegmentRunes {
			flush()
		}
	}
	flush()
	if len(result) <= maxFullShowVoiceSegments {
		return result
	}
	for len(result) > maxFullShowVoiceSegments {
		merged := make([]string, 0, (len(result)+1)/2)
		for index := 0; index < len(result); index += 2 {
			if index+1 < len(result) {
				merged = append(merged, strings.TrimSpace(result[index]+result[index+1]))
			} else {
				merged = append(merged, result[index])
			}
		}
		result = merged
	}
	return result
}

func downloadGeneratedVoiceWAV(ctx context.Context, audioURL string) ([]byte, error) {
	audioURL = strings.TrimSpace(audioURL)
	if audioURL == "" {
		return nil, errors.New("声音地址为空")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, audioURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxGeneratedVoiceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > maxGeneratedVoiceBytes {
		return nil, errors.New("声音为空或超过 32MB")
	}
	return raw, nil
}

func parseGeneratedVoiceWAV(audio []byte) ([]byte, []byte, uint32, error) {
	if len(audio) < 12 || string(audio[:4]) != "RIFF" || string(audio[8:12]) != "WAVE" {
		return nil, nil, 0, errors.New("不是 RIFF/WAVE")
	}
	var fmtData []byte
	var pcmData []byte
	var byteRate uint32
	for offset := 12; offset+8 <= len(audio); {
		chunkID := string(audio[offset : offset+4])
		chunkSize := binary.LittleEndian.Uint32(audio[offset+4 : offset+8])
		start := offset + 8
		end := start + int(chunkSize)
		if end > len(audio) {
			if chunkID == "data" {
				end = len(audio)
			} else {
				return nil, nil, 0, errors.New("WAV 分块长度无效")
			}
		}
		switch chunkID {
		case "fmt ":
			if end-start < 16 {
				return nil, nil, 0, errors.New("WAV fmt 分块无效")
			}
			fmtData = append([]byte(nil), audio[start:end]...)
			byteRate = binary.LittleEndian.Uint32(fmtData[8:12])
		case "data":
			pcmData = append([]byte(nil), audio[start:end]...)
		}
		offset = end
		if chunkSize%2 == 1 {
			offset++
		}
	}
	if len(fmtData) == 0 || len(pcmData) == 0 || byteRate == 0 {
		return nil, nil, 0, errors.New("WAV 缺少 fmt 或 data")
	}
	return fmtData, pcmData, byteRate, nil
}

func combineGeneratedVoiceWAV(
	variantKey string,
	pieces []generatedVoiceWAVPiece,
) ([]byte, []model.LiveAgentPlanTimelineSegment, int64, error) {
	if len(pieces) == 0 {
		return nil, nil, 0, errors.New("没有声音分段")
	}
	fmtData := pieces[0].fmtData
	byteRate := pieces[0].byteRate
	var pcm bytes.Buffer
	timeline := make([]model.LiveAgentPlanTimelineSegment, 0, len(pieces))
	var consumed uint64
	for index, piece := range pieces {
		if piece.byteRate != byteRate || !bytes.Equal(piece.fmtData, fmtData) {
			return nil, nil, 0, errors.New("声音分段 WAV 参数不一致")
		}
		startBytes := consumed
		if _, err := pcm.Write(piece.pcmData); err != nil {
			return nil, nil, 0, err
		}
		consumed += uint64(len(piece.pcmData))
		startMS := int64((startBytes*1000 + uint64(byteRate)/2) / uint64(byteRate))
		endMS := int64((consumed*1000 + uint64(byteRate)/2) / uint64(byteRate))
		if endMS <= startMS {
			endMS = startMS + 1
		}
		timeline = append(timeline, model.LiveAgentPlanTimelineSegment{
			SegmentID: fmt.Sprintf("%s-%03d", variantKey, index+1),
			Index:     index + 1, StartMS: startMS, EndMS: endMS,
			Text: strings.TrimSpace(piece.text), SafeCut: strongFullShowVoiceCut(piece.text),
		})
	}
	data := pcm.Bytes()
	if len(data) == 0 || len(data) > maxGeneratedVoiceBytes {
		return nil, nil, 0, errors.New("拼接后的声音为空或超过 32MB")
	}
	var out bytes.Buffer
	fmtPadding := len(fmtData) % 2
	dataPadding := len(data) % 2
	riffSize := 4 + 8 + len(fmtData) + fmtPadding + 8 + len(data) + dataPadding
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(riffSize))
	out.WriteString("WAVE")
	out.WriteString("fmt ")
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(fmtData)))
	out.Write(fmtData)
	if fmtPadding != 0 {
		out.WriteByte(0)
	}
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(data)))
	out.Write(data)
	if dataPadding != 0 {
		out.WriteByte(0)
	}
	durationMS := timeline[len(timeline)-1].EndMS
	return out.Bytes(), timeline, durationMS, nil
}

func strongFullShowVoiceCut(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	runes := []rune(text)
	switch runes[len(runes)-1] {
	case '。', '！', '？', '!', '?', '；', ';':
		return true
	default:
		return false
	}
}

type fullShowVoiceArchiveResult struct {
	AssetID    int64
	PreviewURL string
	SRT        string
	SafePoints []model.LiveAgentPlanSafePoint
	Manifest   map[string]any
}

func buildFullShowSafePoints(timeline []model.LiveAgentPlanTimelineSegment) []model.LiveAgentPlanSafePoint {
	points := make([]model.LiveAgentPlanSafePoint, 0, len(timeline))
	for index, segment := range timeline {
		if !segment.SafeCut || strings.TrimSpace(segment.Text) == "" || segment.EndMS <= segment.StartMS {
			continue
		}
		nextPreview := ""
		if index+1 < len(timeline) {
			nextPreview = strings.TrimSpace(timeline[index+1].Text)
		}
		points = append(points, model.LiveAgentPlanSafePoint{
			ID:          fmt.Sprintf("SP%03d", len(points)+1),
			CutMS:       segment.EndMS,
			Score:       96,
			Grade:       "A",
			Kind:        "SENTENCE",
			SentenceID:  segment.SegmentID,
			LeftPreview: strings.TrimSpace(segment.Text),
			NextPreview: nextPreview,
		})
	}
	return points
}

func (s *Server) archiveFullShowVoice(
	ctx context.Context,
	tenantID, actorUserID, planID, roomID int64,
	variantKey, voiceID, modelName, source string,
	raw []byte,
	durationMS int64,
	timeline []model.LiveAgentPlanTimelineSegment,
	voiceRates ...float64,
) (fullShowVoiceArchiveResult, error) {
	var result fullShowVoiceArchiveResult
	voiceRate, err := optionalArchivedVoiceRate(voiceRates)
	if err != nil {
		return result, err
	}
	if s.assetStorage == nil {
		return result, errors.New("媒体存储尚未初始化")
	}
	objectKey, err := newMediaObjectKey(tenantID, "generated_voice", strings.ToLower(variantKey)+"-formal.wav")
	if err != nil {
		return result, err
	}
	assetStore := s.assetStorage.Current()
	if ossStore, ossErr := s.assetStorage.For("oss"); ossErr == nil {
		assetStore = ossStore
	}
	if err := assetStore.Put(ctx, objectKey, bytes.NewReader(raw), "audio/wav"); err != nil {
		return result, err
	}
	subtitleObjectKey, err := newMediaObjectKey(tenantID, "generated_voice", strings.ToLower(variantKey)+"-formal.srt")
	if err != nil {
		_ = assetStore.Delete(ctx, objectKey)
		return result, err
	}
	srt := liveAgentTimelineSRT(timeline)
	if strings.TrimSpace(srt) == "" {
		_ = assetStore.Delete(ctx, objectKey)
		return result, errors.New("正式声音字幕时间轴为空")
	}
	if err := assetStore.Put(ctx, subtitleObjectKey, strings.NewReader(srt), "application/x-subrip; charset=utf-8"); err != nil {
		_ = assetStore.Delete(ctx, objectKey)
		return result, fmt.Errorf("写入字幕时间轴: %w", err)
	}
	safePoints := buildFullShowSafePoints(timeline)
	if len(safePoints) == 0 {
		_ = assetStore.Delete(ctx, objectKey)
		_ = assetStore.Delete(ctx, subtitleObjectKey)
		return result, errors.New("正式声音没有可用语义安全切点")
	}
	timelineObjectKey, err := newMediaObjectKey(tenantID, "generated_voice", strings.ToLower(variantKey)+"-formal-timeline.json")
	if err != nil {
		_ = assetStore.Delete(ctx, objectKey)
		_ = assetStore.Delete(ctx, subtitleObjectKey)
		return result, err
	}
	safePointsObjectKey, err := newMediaObjectKey(tenantID, "generated_voice", strings.ToLower(variantKey)+"-formal-safe-points.json")
	if err != nil {
		_ = assetStore.Delete(ctx, objectKey)
		_ = assetStore.Delete(ctx, subtitleObjectKey)
		return result, err
	}
	manifestObjectKey, err := newMediaObjectKey(tenantID, "generated_voice", strings.ToLower(variantKey)+"-formal-manifest.json")
	if err != nil {
		_ = assetStore.Delete(ctx, objectKey)
		_ = assetStore.Delete(ctx, subtitleObjectKey)
		return result, err
	}
	timelinePayload := map[string]any{
		"version":           "formal-mainline-v3",
		"audio_duration_ms": durationMS,
		"sentence_count":    len(timeline),
		"sentences":         timeline,
		"safe_points":       safePoints,
	}
	timelineRaw, _ := json.MarshalIndent(timelinePayload, "", "  ")
	safePointsRaw, _ := json.MarshalIndent(map[string]any{
		"version":     "safe-points-v3",
		"safe_points": safePoints,
	}, "", "  ")
	manifest := map[string]any{
		"version":                "formal-voice-bundle-v3",
		"variant_key":            variantKey,
		"voice_id":               voiceID,
		"tts_model":              modelName,
		"tts_source":             source,
		"audio_duration_ms":      durationMS,
		"segment_count":          len(timeline),
		"safe_point_count":       len(safePoints),
		"audio_object_key":       objectKey,
		"srt_object_key":         subtitleObjectKey,
		"timeline_object_key":    timelineObjectKey,
		"safe_points_object_key": safePointsObjectKey,
		"manifest_object_key":    manifestObjectKey,
		"timeline_precision":     "segment_pcm_exact",
	}
	if voiceRate > 0 {
		manifest["voice_rate"] = voiceRate
	}
	manifestRaw, _ := json.MarshalIndent(manifest, "", "  ")
	putSidecar := func(key string, raw []byte, contentType string) error {
		if err := assetStore.Put(ctx, key, bytes.NewReader(raw), contentType); err != nil {
			return err
		}
		return nil
	}
	for _, sidecar := range []struct {
		key         string
		raw         []byte
		contentType string
	}{
		{timelineObjectKey, timelineRaw, "application/json; charset=utf-8"},
		{safePointsObjectKey, safePointsRaw, "application/json; charset=utf-8"},
		{manifestObjectKey, manifestRaw, "application/json; charset=utf-8"},
	} {
		if err := putSidecar(sidecar.key, sidecar.raw, sidecar.contentType); err != nil {
			_ = assetStore.Delete(ctx, objectKey)
			_ = assetStore.Delete(ctx, subtitleObjectKey)
			_ = assetStore.Delete(ctx, timelineObjectKey)
			_ = assetStore.Delete(ctx, safePointsObjectKey)
			_ = assetStore.Delete(ctx, manifestObjectKey)
			return result, fmt.Errorf("写入正式声音资产包: %w", err)
		}
	}
	digest := sha256.Sum256(raw)
	durationValue := uint64(durationMS)
	metadata := map[string]any{
		"purpose": "live_agent_formal_voice_timeline", "plan_id": planID, "room_id": roomID,
		"variant_key": variantKey, "source": source, "voice_id": voiceID, "tts_model": modelName,
		"subtitle_object_key":    subtitleObjectKey,
		"timeline_object_key":    timelineObjectKey,
		"safe_points_object_key": safePointsObjectKey,
		"manifest_object_key":    manifestObjectKey,
		"timeline_format":        "json+srt+safe_points_v3",
		"timeline_precision":     "segment_pcm_exact",
	}
	if voiceRate > 0 {
		metadata["voice_rate"] = voiceRate
	}
	item, err := s.store.CreateMediaAsset(ctx, model.CreateMediaAssetInput{
		TenantID: tenantID, AssetType: "generated_voice", OriginalName: strings.ToLower(variantKey) + "-formal.wav",
		StorageDriver: assetStore.Driver(), StorageBucket: assetStore.Bucket(), ObjectKey: objectKey,
		MIMEType: "audio/wav", SizeBytes: uint64(len(raw)), DurationMS: &durationValue,
		ChecksumSHA256: hex.EncodeToString(digest[:]), CreatedByUserID: actorUserID,
		Metadata: metadata,
	})
	if err != nil {
		_ = assetStore.Delete(ctx, objectKey)
		_ = assetStore.Delete(ctx, subtitleObjectKey)
		_ = assetStore.Delete(ctx, timelineObjectKey)
		_ = assetStore.Delete(ctx, safePointsObjectKey)
		_ = assetStore.Delete(ctx, manifestObjectKey)
		return result, err
	}
	previewURL, signErr := assetStore.SignedURL(ctx, item.ObjectKey, 12*time.Hour)
	if signErr != nil {
		return result, signErr
	}
	if strings.TrimSpace(previewURL) == "" {
		base := strings.TrimRight(strings.TrimSpace(s.publicWebURL), "/")
		path := "/api/v1/live/media-assets/" + strconv.FormatInt(item.ID, 10) + "/content"
		previewURL = path
		if base != "" {
			previewURL = base + path
		}
	}
	result.AssetID = item.ID
	result.PreviewURL = strings.TrimSpace(previewURL)
	result.SRT = srt
	result.SafePoints = safePoints
	result.Manifest = manifest
	return result, nil
}

func liveAgentTimelineSRT(timeline []model.LiveAgentPlanTimelineSegment) string {
	var builder strings.Builder
	for _, segment := range timeline {
		if strings.TrimSpace(segment.Text) == "" || segment.EndMS <= segment.StartMS {
			continue
		}
		fmt.Fprintf(&builder, "%d\n%s --> %s\n%s\n\n",
			segment.Index,
			formatSRTMillis(segment.StartMS),
			formatSRTMillis(segment.EndMS),
			strings.TrimSpace(segment.Text),
		)
	}
	return builder.String()
}

func formatSRTMillis(value int64) string {
	if value < 0 {
		value = 0
	}
	hours := value / 3600000
	value %= 3600000
	minutes := value / 60000
	value %= 60000
	seconds := value / 1000
	millis := value % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, seconds, millis)
}
