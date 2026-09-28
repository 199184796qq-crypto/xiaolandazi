package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func liveAgentPlanVersionPathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("versionID")), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "直播智能体版本 ID 无效")
		return 0, false
	}
	return value, true
}

func liveAgentVoiceIdentityKey(identity model.LiveAgentVoiceIdentity) string {
	raw := strings.Join([]string{
		strings.TrimSpace(identity.Source),
		strings.TrimSpace(identity.Provider),
		strings.TrimSpace(identity.VoiceID),
		strconv.FormatInt(identity.ProfileID, 10),
		strings.TrimSpace(identity.Model),
		strings.TrimSpace(identity.Version),
	}, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func normalizeLiveAgentVoiceIdentity(identity *model.LiveAgentVoiceIdentity) error {
	identity.Name = strings.TrimSpace(identity.Name)
	identity.Version = strings.TrimSpace(identity.Version)
	identity.Source = strings.TrimSpace(identity.Source)
	identity.Provider = strings.TrimSpace(identity.Provider)
	identity.VoiceID = strings.TrimSpace(identity.VoiceID)
	identity.Model = strings.TrimSpace(identity.Model)
	identity.Emotion = strings.TrimSpace(identity.Emotion)
	if identity.Version == "" {
		identity.Version = "V1"
	}
	if identity.Rate == 0 {
		identity.Rate = 1
	}
	if identity.Name == "" || utf8.RuneCountInString(identity.Name) > 128 {
		return errors.New("声音身份名称必须为 1 到 128 字")
	}
	if identity.Source != "official" && identity.Source != "clone" {
		return errors.New("声音身份来源无效")
	}
	if identity.Provider == "" || identity.Model == "" || identity.VoiceID == "" {
		return errors.New("声音身份缺少供应商、模型或音色 ID")
	}
	if identity.Rate < 0.5 || identity.Rate > 2.0 {
		return errors.New("声音语速必须在 0.5 到 2.0 之间")
	}
	if identity.Source == "clone" && identity.ProfileID <= 0 {
		return errors.New("克隆声音身份缺少声音档案")
	}
	return nil
}

func validateLiveAgentPlanVersionInput(input *model.CreateLiveAgentPlanVersionInput) error {
	if input.RoomID <= 0 {
		return errors.New("请选择发布直播间")
	}
	if input.DurationMinutes < 30 || input.DurationMinutes > 120 || input.DurationMinutes%30 != 0 {
		return errors.New("生成内容时长必须为 30、60、90 或 120 分钟")
	}
	if input.RoundMinutes <= 0 || input.RoundMinutes > 30 {
		return errors.New("单轮时长必须在 1 到 30 分钟之间")
	}
	if err := normalizeLiveAgentVoiceIdentity(&input.VoiceIdentity); err != nil {
		return err
	}
	if len(input.Variants) == 0 {
		return errors.New("版本至少需要一套稿件")
	}
	if len(input.Variants) > 10 {
		return errors.New("单个版本最多保存 10 套稿件")
	}
	identityKey := liveAgentVoiceIdentityKey(input.VoiceIdentity)
	seen := make(map[string]struct{}, len(input.Variants))
	formalCount := 0
	for index := range input.Variants {
		variant := &input.Variants[index]
		variant.VariantKey = strings.TrimSpace(variant.VariantKey)
		variant.Title = strings.TrimSpace(variant.Title)
		variant.OpeningAngle = strings.TrimSpace(variant.OpeningAngle)
		variant.Text = strings.TrimSpace(variant.Text)
		variant.AudioURL = strings.TrimSpace(variant.AudioURL)
		if variant.VariantKey == "" || utf8.RuneCountInString(variant.VariantKey) > 32 {
			return errors.New("正式稿编号无效")
		}
		if _, exists := seen[variant.VariantKey]; exists {
			return errors.New("正式稿编号不能重复")
		}
		seen[variant.VariantKey] = struct{}{}
		if variant.Text == "" {
			return errors.New(variant.VariantKey + "稿正文为空")
		}
		if !variant.IsFormal {
			variant.AudioURL = ""
			variant.AudioAssetID = 0
			variant.AudioDurationMS = 0
			variant.Timeline = nil
			variant.SRT = ""
			variant.SafePoints = nil
			variant.AssetManifest = nil
			variant.VoiceIdentityKey = ""
			continue
		}
		formalCount++
		if variant.AudioAssetID <= 0 {
			return errors.New(variant.VariantKey + "稿还没有正式声音资产，请重新生成声音")
		}
		if variant.AudioDurationMS <= 0 {
			return errors.New(variant.VariantKey + "稿缺少真实声音时长，请重新生成声音")
		}
		if err := validateLiveAgentVariantTimeline(*variant); err != nil {
			return err
		}
		if err := validateLiveAgentVariantSchedulingAssets(*variant); err != nil {
			return err
		}
		if variant.VoiceIdentityKey != "" && variant.VoiceIdentityKey != identityKey {
			return errors.New(variant.VariantKey + "稿声音身份与当前版本不一致")
		}
		variant.VoiceIdentityKey = identityKey
	}
	if formalCount == 0 {
		return errors.New("请至少选择一套正式稿并生成声音")
	}
	return nil
}

func validateLiveAgentVariantTimeline(variant model.LiveAgentPlanVersionVariant) error {
	if len(variant.Timeline) == 0 {
		return errors.New(variant.VariantKey + "稿缺少声音文字时间轴，请重新生成声音")
	}
	if len(variant.Timeline) > 1024 {
		return errors.New(variant.VariantKey + "稿声音时间轴分段过多")
	}
	var previousEnd int64
	for index, segment := range variant.Timeline {
		if strings.TrimSpace(segment.SegmentID) == "" || strings.TrimSpace(segment.Text) == "" {
			return errors.New(variant.VariantKey + "稿声音时间轴存在空分段")
		}
		if segment.Index != index+1 {
			return errors.New(variant.VariantKey + "稿声音时间轴顺序无效")
		}
		if segment.StartMS < previousEnd || segment.StartMS < 0 || segment.EndMS <= segment.StartMS {
			return errors.New(variant.VariantKey + "稿声音时间轴存在重叠或无效时间")
		}
		if segment.EndMS > variant.AudioDurationMS {
			return errors.New(variant.VariantKey + "稿声音时间轴超过声音总时长")
		}
		previousEnd = segment.EndMS
	}
	return nil
}

func validateLiveAgentVariantSchedulingAssets(variant model.LiveAgentPlanVersionVariant) error {
	if strings.TrimSpace(variant.SRT) == "" || !strings.Contains(variant.SRT, " --> ") {
		return errors.New(variant.VariantKey + "稿缺少 SRT 字幕时间轴，请重新生成声音")
	}
	if len(variant.SafePoints) == 0 {
		return errors.New(variant.VariantKey + "稿缺少双轨安全切点，请重新生成声音")
	}
	if len(variant.SafePoints) > 1024 {
		return errors.New(variant.VariantKey + "稿双轨安全切点过多")
	}
	if len(variant.AssetManifest) == 0 {
		return errors.New(variant.VariantKey + "稿缺少声音资产清单，请重新生成声音")
	}
	segments := make(map[string]model.LiveAgentPlanTimelineSegment, len(variant.Timeline))
	for _, segment := range variant.Timeline {
		segments[strings.TrimSpace(segment.SegmentID)] = segment
	}
	var previousCut int64
	for _, point := range variant.SafePoints {
		point.ID = strings.TrimSpace(point.ID)
		point.Grade = strings.ToUpper(strings.TrimSpace(point.Grade))
		point.Kind = strings.ToUpper(strings.TrimSpace(point.Kind))
		point.SentenceID = strings.TrimSpace(point.SentenceID)
		if point.ID == "" || point.SentenceID == "" {
			return errors.New(variant.VariantKey + "稿双轨安全切点标识无效")
		}
		if point.CutMS <= previousCut || point.CutMS > variant.AudioDurationMS {
			return errors.New(variant.VariantKey + "稿双轨安全切点时间无效")
		}
		if point.Score < 0 || point.Score > 100 {
			return errors.New(variant.VariantKey + "稿双轨安全切点评分无效")
		}
		if point.Grade != "A" && point.Grade != "B" && point.Grade != "C" {
			return errors.New(variant.VariantKey + "稿双轨安全切点等级无效")
		}
		segment, exists := segments[point.SentenceID]
		if !exists || segment.EndMS != point.CutMS || !segment.SafeCut {
			return errors.New(variant.VariantKey + "稿双轨安全切点没有对齐语义句末")
		}
		previousCut = point.CutMS
	}
	return nil
}

func (s *Server) validateLiveAgentVersionAssets(
	ctx context.Context,
	tenantID int64,
	variants []model.LiveAgentPlanVersionVariant,
) error {
	for _, variant := range variants {
		if !variant.IsFormal {
			continue
		}
		asset, err := s.store.GetMediaAsset(ctx, tenantID, variant.AudioAssetID)
		if err != nil {
			return errors.New(variant.VariantKey + "稿正式声音资产不存在，请重新生成声音")
		}
		if strings.TrimSpace(asset.Status) != "active" || strings.TrimSpace(asset.ObjectKey) == "" || asset.SizeBytes == 0 {
			return errors.New(variant.VariantKey + "稿正式声音资产已失效")
		}
		if asset.AssetType != "generated_voice" && asset.AssetType != "audio" {
			return errors.New(variant.VariantKey + "稿绑定的不是正式声音资产")
		}
		if asset.DurationMS == nil || int64(*asset.DurationMS) != variant.AudioDurationMS {
			return errors.New(variant.VariantKey + "稿声音资产时长与时间轴不一致，请重新生成声音")
		}
		if s.assetStorage != nil {
			assetStore, err := s.assetStorage.For(asset.StorageDriver)
			if err != nil {
				return errors.New(variant.VariantKey + "稿声音存储驱动不可用")
			}
			for _, metadataKey := range []string{
				"subtitle_object_key",
				"timeline_object_key",
				"safe_points_object_key",
				"manifest_object_key",
			} {
				objectKey := strings.TrimSpace(fmt.Sprint(asset.Metadata[metadataKey]))
				if objectKey == "" || objectKey == "<nil>" {
					return errors.New(variant.VariantKey + "稿声音资产包不完整，请重新生成声音")
				}
				reader, openErr := assetStore.Open(ctx, objectKey)
				if openErr != nil {
					return errors.New(variant.VariantKey + "稿声音调度资产不存在，请重新生成声音")
				}
				_ = reader.Close()
			}
		}
	}
	return nil
}

func (s *Server) liveAgentPlanVersionCreate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input model.CreateLiveAgentPlanVersionInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "保存智能体版本格式错误")
		return
	}
	if err := validateLiveAgentPlanVersionInput(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	if err := s.validateLiveAgentVersionAssets(r.Context(), tenantID, input.Variants); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := s.store.CreateLiveAgentPlanVersion(r.Context(), tenantID, planID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	}
	if errors.Is(err, appdb.ErrLiveAgentPlanRoomNotBound) {
		writeError(w, http.StatusConflict, "当前方案还没有绑定到这个直播间")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存直播智能体版本失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) liveAgentPlanVersionList(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	roomID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("room_id")), 10, 64)
	if err != nil || roomID <= 0 {
		writeError(w, http.StatusBadRequest, "请选择直播间")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return
	}
	items, err := s.store.ListLiveAgentPlanVersions(r.Context(), tenantID, planID, roomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体版本失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func selectLiveAgentPlanWorkspaceVersion(items []model.LiveAgentPlanVersion) (*model.LiveAgentPlanVersion, string) {
	var latestDraft *model.LiveAgentPlanVersion
	var latestPublished *model.LiveAgentPlanVersion
	for index := range items {
		status := strings.ToLower(strings.TrimSpace(items[index].LifecycleStatus))
		if status == "draft" && latestDraft == nil {
			item := items[index]
			latestDraft = &item
		}
		if status == "published" && latestPublished == nil {
			item := items[index]
			latestPublished = &item
		}
	}
	if latestDraft != nil && (latestPublished == nil || latestDraft.VersionNo > latestPublished.VersionNo) {
		return latestDraft, "draft"
	}
	if latestPublished != nil {
		return latestPublished, "published"
	}
	return nil, ""
}

func hydrateLiveAgentPlanVersionForWorkspace(item *model.LiveAgentPlanVersion) {
	if item == nil {
		return
	}
	for index := range item.Variants {
		variant := &item.Variants[index]
		if variant.Index <= 0 {
			variant.Index = index + 1
		}
		if variant.AudioAssetID > 0 {
			variant.AudioURL = "/api/v1/live/media-assets/" + strconv.FormatInt(variant.AudioAssetID, 10) + "/content"
		}
		// Older saved versions predate persisted audit detail. Those variants
		// could only have reached a saved formal version after passing the
		// formal-draft gate, so restore them as an approved legacy snapshot.
		if variant.IsFormal &&
			variant.Audit.Issues == nil &&
			variant.Audit.FactCoveragePct == 0 &&
			variant.Audit.LinkCoveragePct == 0 &&
			variant.Audit.SimilarityPct == 0 {
			variant.Audit.Passed = true
			variant.Audit.Issues = []model.LiveAgentFullShowAuditIssue{}
		}
		if variant.CoveredFactKeys == nil {
			variant.CoveredFactKeys = []string{}
		}
		if variant.CoveredLinkKeys == nil {
			variant.CoveredLinkKeys = []string{}
		}
	}
}

func (s *Server) liveAgentPlanWorkspaceGet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	roomID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("room_id")), 10, 64)
	if err != nil || roomID <= 0 {
		writeError(w, http.StatusBadRequest, "请选择直播间")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return
	}
	items, err := s.store.ListLiveAgentPlanVersions(r.Context(), tenantID, planID, roomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体编辑工作区失败")
		return
	}
	version, source := selectLiveAgentPlanWorkspaceVersion(items)
	if version == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"version": nil,
			"source":  "",
		})
		return
	}
	hydrateLiveAgentPlanVersionForWorkspace(version)
	writeJSON(w, http.StatusOK, map[string]any{
		"version": version,
		"source":  source,
	})
}

func (s *Server) liveAgentPlanVersionPublish(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	versionID, ok := liveAgentPlanVersionPathID(w, r)
	if !ok {
		return
	}
	var input model.PublishLiveAgentPlanVersionInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "发布智能体版本格式错误")
		return
	}
	if input.RoomID <= 0 {
		writeError(w, http.StatusBadRequest, "请选择发布直播间")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	pending, err := s.store.GetLiveAgentPlanVersion(r.Context(), tenantID, planID, versionID)
	if errors.Is(err, appdb.ErrLiveAgentPlanVersionNotFound) {
		writeError(w, http.StatusNotFound, "待发布智能体版本不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取待发布智能体版本失败")
		return
	}
	if pending.RoomID != input.RoomID {
		writeError(w, http.StatusBadRequest, "待发布版本不属于当前直播间")
		return
	}
	check := model.CreateLiveAgentPlanVersionInput{
		RoomID: pending.RoomID, DurationMinutes: pending.DurationMinutes, RoundMinutes: pending.RoundMinutes,
		VoiceIdentity: pending.VoiceIdentity, Variants: append([]model.LiveAgentPlanVersionVariant(nil), pending.Variants...),
		GenerationContext: pending.GenerationContext,
	}
	if err := validateLiveAgentPlanVersionInput(&check); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.validateLiveAgentVersionAssets(r.Context(), tenantID, pending.Variants); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	item, err := s.store.PublishLiveAgentPlanVersion(
		r.Context(),
		tenantID,
		planID,
		input.RoomID,
		versionID,
		actor.UserID,
	)
	if errors.Is(err, appdb.ErrLiveAgentPlanVersionNotFound) {
		writeError(w, http.StatusNotFound, "待发布智能体版本不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "发布直播智能体版本失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveAgentPlanPublishedVersionForRoom(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	item, err := s.store.GetPublishedLiveAgentPlanVersionForRoom(r.Context(), tenantID, roomID)
	if errors.Is(err, appdb.ErrLiveAgentPlanVersionNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"version": nil})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播间已发布智能体版本失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": item})
}
