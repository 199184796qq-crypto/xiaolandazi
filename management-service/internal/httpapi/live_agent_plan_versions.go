package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/contentguard"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/ttsscheduler"
	"livecompanion/management/internal/voicecatalog"
)

func liveAgentPlanVersionPathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("versionID")), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "直播智能体版本 ID 无效")
		return 0, false
	}
	return value, true
}

// derivedSafetyRef 构造派生安全资产的版本快照引用。removed_risks 携带本次保存
// 因无法安全改写而被整条剔除的风险事实明细，供前端定位到对应事实。
func derivedSafetyRef(safety model.LiveDerivedSafetyAsset) map[string]any {
	ref := map[string]any{"asset_id": safety.ID, "source_sha256": safety.SourceSHA256, "rewritten_count": safety.RewrittenCount, "notice": safety.Notice}
	if safety.RemovedRiskCount > 0 && strings.TrimSpace(safety.RemovedRisksJSON) != "" {
		var risks []map[string]any
		if json.Unmarshal([]byte(safety.RemovedRisksJSON), &risks) == nil {
			ref["removed_risks"] = risks
			ref["removed_risk_count"] = safety.RemovedRiskCount
		}
	}
	return ref
}

func liveAgentVoiceIdentityKey(identity model.LiveAgentVoiceIdentity) string {
	raw := strings.Join([]string{
		strings.TrimSpace(identity.Source),
		strings.TrimSpace(identity.Provider),
		strings.TrimSpace(identity.VoiceID),
		strconv.FormatInt(identity.ProfileID, 10),
		strconv.FormatInt(identity.BindingID, 10),
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
	identity.TTSPool = strings.TrimSpace(identity.TTSPool)
	if err := ttsscheduler.ValidateVoicePool(identity.TTSPool, identity.Provider, identity.Model); err != nil {
		return err
	}
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

// recordingScriptOnlyInput 允许“话术优先 + AI 声音”只用文本框内容保存：
// 不上传原音，也不需要 CUSTOM 稿；直播间按 recording_script 逐段克隆合成。
func recordingScriptOnlyInput(input *model.CreateLiveAgentPlanVersionInput) bool {
	if input == nil {
		return false
	}
	return recordingScriptOnlyContext(input.GenerationContext)
}

func recordingScriptOnlyVersion(version model.LiveAgentPlanVersion) bool {
	return recordingScriptOnlyContext(version.GenerationContext)
}

func recordingScriptOnlyContext(context map[string]any) bool {
	mode, _ := context["content_mode"].(string)
	voiceMode, _ := context["recording_voice_mode"].(string)
	if strings.TrimSpace(mode) != model.LiveContentUserAudio || strings.TrimSpace(voiceMode) != "stream_clone" {
		return false
	}
	script, ok := recordingScriptFromContext(context)
	if !ok {
		return false
	}
	for _, tab := range script.Tabs {
		for _, text := range tab.texts() {
			if strings.TrimSpace(text) != "" {
				return true
			}
		}
	}
	return false
}

func validateLiveAgentPlanVersionInput(input *model.CreateLiveAgentPlanVersionInput) error {
	if input.RoomID < 0 {
		return errors.New("room_id 不能为负数")
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
		if isRealtimeScheme(input.GenerationContext) {
			_, err := realtimeScheme(input.GenerationContext)
			return err
		}
		if recordingScriptOnlyInput(input) {
			return nil
		}
		return errors.New("版本至少需要一套稿件")
	}
	if len(input.Variants) > 10 {
		return errors.New("单个版本最多保存 10 套稿件")
	}
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

	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	input.RoomID = 0
	if !s.allowPlanSave(w, r, tenantID, planID) {
		return
	}
	if input.GenerationContext == nil {
		input.GenerationContext = map[string]any{}
	}
	delete(input.GenerationContext, "content_policy")
	delete(input.GenerationContext, "dynamic_authorized")
	settings, err := s.store.GetPlanAssetSettings(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 404, "方案不存在或配置读取失败")
		return
	}
	input.GenerationContext["content_mode"] = settings.ContentMode
	w, r = anchorAnalysisStream(w, r)
	reportAnchorAnalysis(r.Context(), 5, "正在核对方案参数与主播声音")
	if err := s.validateContentModeAssets(r.Context(), tenantID, settings.ContentMode, input.Variants); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	if err := s.validatePlanVoiceIdentity(r.Context(), tenantID, &input.VoiceIdentity); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	input.GenerationContext["reading_rules"] = settings.ReadingRules

	if err := s.captureRealtimeScheme(r.Context(), tenantID, planID, &input); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	// 话术优先模式允许多个文本框；单框是 N=1 的退化，旧快照没有该字段时不动。
	if err := normalizeRecordingScript(input.GenerationContext); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := normalizeScriptPlaybackVoices(&input, func(identity *model.LiveAgentVoiceIdentity) error {
		return s.validatePlanVoiceIdentity(r.Context(), tenantID, identity)
	}); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	paragraphVersion := model.LiveAgentPlanVersion{GenerationContext: input.GenerationContext, VoiceIdentity: input.VoiceIdentity, Variants: input.Variants}
	if usesScriptFirstParagraphs(paragraphVersion) && !hasGroupedRecordingScript(paragraphVersion) {
		if err := validateScriptFirstTexts(recordingStreamTexts(paragraphVersion)); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := validateLiveAgentPlanVersionInput(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.validateLiveAgentVersionAssets(r.Context(), tenantID, input.Variants); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if actor.IsInternalStaff() {
		for _, selected := range scriptVoicesForPlayback(paragraphVersion) {
			if selected.Identity == nil {
				continue
			}
			identity := selected.Identity
			if !s.requireLiveSupportSelectedVoice(w, r, actor, tenantID, map[string]any{"profile_id": identity.ProfileID, "binding_id": identity.BindingID, "voice_id": identity.VoiceID}) {
				return
			}
		}
		voice := map[string]any{"profile_id": input.VoiceIdentity.ProfileID, "binding_id": input.VoiceIdentity.BindingID, "voice_id": input.VoiceIdentity.VoiceID}
		if !s.requireLiveSupportSelectedVoice(w, r, actor, tenantID, voice) {
			return
		}
		for _, variant := range input.Variants {
			if variant.AudioAssetID <= 0 {
				continue
			}
			asset, err := s.store.GetMediaAsset(r.Context(), tenantID, variant.AudioAssetID)
			if err != nil {
				writeError(w, http.StatusBadRequest, "音频资产不存在")
				return
			}
			if !s.requireLiveSupportMedia(w, r, actor, asset) {
				return
			}
		}
	}
	if input.GenerationContext == nil {
		input.GenerationContext = map[string]any{}
	}
	// 业务模式快照：保存方案时把方案当前的业务模式 + 该模式下的行业选择存进版本，
	// 只用于版本记录/切换时的口径一致（复用），不参与话术生成——生成链路始终读
	// 方案当前的业务设置（loadIndustryPackForPlan），不受版本快照影响。
	if settings, bizErr := s.store.GetPlanBusinessSettings(r.Context(), tenantID, planID); bizErr == nil {
		input.GenerationContext["plan_business"] = map[string]any{
			"biz_domain": settings.BizDomain,
			"industry":   strings.TrimSpace(settings.Industries[settings.BizDomain]),
		}
	}
	// 自动资料整理和互动资料生成已暂停：只保存用户输入，实时模式
	// 仅引用来源一致的已准备缓存，不在缓存缺失时偷偷调用模型。
	if err := s.capturePlanSaveWithoutAnalysis(r.Context(), tenantID, planID, &input); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	// 话术违规检测：P0 命中就不允许入库，避免违规话术（极限词、编造承诺等）被保存。
	if err := s.validateScriptEntriesAgainstP0(r.Context(), &input); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	// 保存只做基础校验与资产生成；方案级匹配核对（validateRealtimeScheme）不卡保存入口，
	// 其存在性检查仍由内容策略/工作区检查/派生安全三处链路承担。
	reportAnchorAnalysis(r.Context(), 92, "检查已通过，正在保存智能体方案")
	item, err := s.store.CreateLiveAgentPlanVersion(r.Context(), tenantID, planID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanInUse) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, appdb.ErrPoolSourceChanged) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
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
	reportAnchorAnalysis(r.Context(), 100, "智能体方案已保存，下次开播直接使用")
	// 保存即预生成"开场锚点那一条"：只一条，花费很小，换来开播立刻有声。
	// 长片段方案若不在保存时预热，第一次开播要干等整段合成。
	// Paid media remains available to existing live snapshots and later reuse.
	s.warmPlanFirstParagraph(tenantID, planID, item)
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

	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return
	}
	items, err := s.store.ListLiveAgentPlanVersionsForPlan(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体版本失败")
		return
	}
	_, _ = s.store.BuildPlanPurposes(r.Context(), tenantID, planID)
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func selectLiveAgentPlanWorkspaceVersion(items []model.LiveAgentPlanVersion, contentMode string) (*model.LiveAgentPlanVersion, string) {
	var latestDraft *model.LiveAgentPlanVersion
	var latestPublished *model.LiveAgentPlanVersion
	for index := range items {
		if model.LiveContentModeFromGenerationContext(items[index].GenerationContext) != contentMode {
			continue
		}
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

func selectLatestLiveAgentPlanWorkspaceTemplate(items []model.LiveAgentPlanVersion, contentMode string) (*model.LiveAgentPlanVersion, string) {
	for index := range items {
		if model.LiveContentModeFromGenerationContext(items[index].GenerationContext) != contentMode {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(items[index].LifecycleStatus))
		if status != "draft" && status != "published" {
			continue
		}
		item := items[index]
		return &item, status
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

	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return
	}
	settings, err := s.store.GetPlanAssetSettings(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 404, "方案不存在")
		return
	}
	contentMode := strings.TrimSpace(r.URL.Query().Get("content_mode"))
	if contentMode == "" {
		contentMode = settings.ContentMode
	}
	if contentMode != model.LiveContentUserAudio && contentMode != model.LiveContentAIDynamic && contentMode != model.LiveContentAIPregenerated {
		writeError(w, 400, "无效的方案内容模式")
		return
	}
	items, err := s.store.ListLiveAgentPlanVersionsForPlan(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取方案版本失败")
		return
	}
	version, source := selectLatestLiveAgentPlanWorkspaceTemplate(items, contentMode)
	if version == nil {
		// A completed upload can precede the very first Save. Recover only that
		// unsaved workspace; a saved version remains authoritative (including
		// deliberately removed recordings). Never replay paid upload processing.
		uploads := []model.CustomMainlineUpload{}
		if contentMode == model.LiveContentUserAudio {
			uploads, err = s.store.ListReadyCustomMainlineUploads(r.Context(), tenantID, planID, actor.UserID)
			if err != nil {
				writeError(w, 500, "读取已完成录音失败，请重试；无需重新上传")
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, map[string]any{"version": nil, "source": "", "inherited": false, "recording_uploads": uploads})
		return
	}

	hydrateLiveAgentPlanVersionForWorkspace(version)
	writeJSON(w, http.StatusOK, map[string]any{
		"version":        version,
		"source":         source,
		"inherited":      false,
		"shared":         true,
		"source_room_id": version.SavedFromRoomID,
		"checks":         s.liveAgentPlanWorkspaceChecks(r.Context(), tenantID, planID, version),
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
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, input.RoomID) {
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
	// Compatibility for old browser clients. Saving is now the only activation
	// step; this endpoint cannot select or rewrite an older saved snapshot.
	latest, latestErr := s.store.GetPublishedLiveAgentPlanVersionForRoom(r.Context(), tenantID, input.RoomID)
	if latestErr != nil || latest.ID != pending.ID {
		writeError(w, http.StatusConflict, "方案保存后即生效，不再单独发布或回滚；请保存当前方案后开始直播")
		return
	}
	writeJSON(w, http.StatusOK, latest)
	return
}

type publishRoomEmotionInput struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) liveAgentPlanPublishRoomEmotion(w http.ResponseWriter, r *http.Request) {
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
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, roomID) {
		return
	}
	var input publishRoomEmotionInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "情感开关参数格式错误")
		return
	}
	if input.Enabled && !model.LiveSpeechEmotionEnabled {
		writeError(w, http.StatusConflict, "实时情感功能已关闭")
		return
	}
	published, err := s.store.GetPublishedLiveAgentPlanVersionForRoom(r.Context(), tenantID, roomID)
	if errors.Is(err, appdb.ErrLiveAgentPlanVersionNotFound) {
		writeError(w, http.StatusConflict, "当前直播间还没有已发布智能体版本")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取当前已发布智能体版本失败")
		return
	}
	identity := published.VoiceIdentity
	enabled := input.Enabled
	identity.EmotionEnabled = &enabled
	inputVersion := model.CreateLiveAgentPlanVersionInput{
		RoomID: published.RoomID, DurationMinutes: published.DurationMinutes, RoundMinutes: published.RoundMinutes,
		VoiceIdentity: identity, Variants: append([]model.LiveAgentPlanVersionVariant(nil), published.Variants...),
		GenerationContext: published.GenerationContext,
	}
	if err := s.validateLiveContentVersion(r.Context(), tenantID, roomID, published); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := s.refreshVersionDerivedSafety(r.Context(), tenantID, published.PlanID, actor.UserID, &inputVersion); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := validateLiveAgentPlanVersionInput(&inputVersion); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.validatePlanVoiceIdentity(r.Context(), tenantID, &inputVersion.VoiceIdentity); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	created, err := s.store.CreateLiveAgentPlanVersion(r.Context(), tenantID, published.PlanID, actor.UserID, inputVersion)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存新的实时情感版本失败")
		return
	}
	result, err := s.store.PublishLiveAgentPlanVersion(r.Context(), tenantID, published.PlanID, roomID, created.ID, actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "发布新的实时情感版本失败")
		return
	}
	_, _ = s.store.BuildPlanPurposes(r.Context(), tenantID, result.PlanID)
	writeJSON(w, http.StatusOK, result)
}

type publishRoomVoiceRateInput struct {
	Rate float64 `json:"rate"`
}

func normalizePublishedVoiceRate(rate float64) (float64, bool) {
	return model.NormalizePlanVoiceRate(rate)
}

func (s *Server) liveAgentPlanPublishRoomVoiceRate(w http.ResponseWriter, r *http.Request) {
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
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, roomID) {
		return
	}
	var input publishRoomVoiceRateInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "语速参数格式错误")
		return
	}
	rate, valid := normalizePublishedVoiceRate(input.Rate)
	if !valid {
		writeError(w, http.StatusBadRequest, appdb.ErrInvalidPlanVoiceRate.Error())
		return
	}
	published, err := s.store.GetPublishedLiveAgentPlanVersionForRoom(r.Context(), tenantID, roomID)
	if errors.Is(err, appdb.ErrLiveAgentPlanVersionNotFound) {
		writeError(w, http.StatusConflict, "当前直播间还没有已发布智能体版本")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取当前已发布智能体版本失败")
		return
	}
	if _, err := s.store.SetPlanVoiceRate(r.Context(), tenantID, published.PlanID, rate); err != nil {
		writeError(w, http.StatusInternalServerError, "保存方案语速失败")
		return
	}
	published.VoiceIdentity.Rate = rate
	writeJSON(w, http.StatusOK, published)
}

type publishRoomVoiceBindingInput struct {
	BindingID int64 `json:"binding_id"`
}

func (s *Server) liveAgentPlanPublishRoomVoiceBinding(w http.ResponseWriter, r *http.Request) {
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
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, roomID) {
		return
	}
	var input publishRoomVoiceBindingInput
	if err := readJSON(w, r, &input); err != nil || input.BindingID <= 0 {
		writeError(w, http.StatusBadRequest, "请选择要发布的声音模型")
		return
	}
	binding, err := s.store.GetVoiceModelBinding(r.Context(), tenantID, input.BindingID)
	if err != nil || !strings.EqualFold(strings.TrimSpace(binding.Status), "ready") || strings.TrimSpace(binding.VoiceID) == "" {
		writeError(w, http.StatusBadRequest, "声音模型绑定不存在或尚未准备好")
		return
	}
	profile, err := s.store.GetVoiceProfile(r.Context(), tenantID, binding.ProfileID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "声音档案不存在")
		return
	}
	if !s.requireLiveSupportVoice(w, r, actor, tenantID, profile, false) {
		return
	}
	published, err := s.store.GetPublishedLiveAgentPlanVersionForRoom(r.Context(), tenantID, roomID)
	if errors.Is(err, appdb.ErrLiveAgentPlanVersionNotFound) {
		writeError(w, http.StatusConflict, "当前直播间还没有已发布智能体版本")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取当前已发布智能体版本失败")
		return
	}
	rate := published.VoiceIdentity.Rate
	if rate < 0.5 || rate > 2 {
		rate = binding.Rate
	}
	if rate < 0.5 || rate > 2 {
		rate = 1
	}
	identity := model.LiveAgentVoiceIdentity{
		TTSPool: ttsscheduler.PoolForProvider(binding.Provider),
		Name:    profile.Name, Version: "V1", Source: "clone", Provider: binding.Provider,
		VoiceID: binding.VoiceID, ProfileID: profile.ID, BindingID: binding.ID,
		Model: binding.Model, Rate: rate,
		EmotionEnabled: published.VoiceIdentity.EmotionEnabled,
		Emotion:        published.VoiceIdentity.Emotion,
		Style:          published.VoiceIdentity.Style,
	}
	inputVersion := model.CreateLiveAgentPlanVersionInput{
		RoomID: published.RoomID, DurationMinutes: published.DurationMinutes, RoundMinutes: published.RoundMinutes,
		VoiceIdentity: identity, Variants: append([]model.LiveAgentPlanVersionVariant(nil), published.Variants...),
		GenerationContext: published.GenerationContext,
	}
	published.VoiceIdentity = identity
	if err := s.validateLiveContentVersion(r.Context(), tenantID, roomID, published); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := s.refreshVersionDerivedSafety(r.Context(), tenantID, published.PlanID, actor.UserID, &inputVersion); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := validateLiveAgentPlanVersionInput(&inputVersion); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	created, err := s.store.CreateLiveAgentPlanVersion(r.Context(), tenantID, published.PlanID, actor.UserID, inputVersion)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存新的实时声音版本失败")
		return
	}
	result, err := s.store.PublishLiveAgentPlanVersion(r.Context(), tenantID, published.PlanID, roomID, created.ID, actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "发布新的实时声音版本失败")
		return
	}
	_, _ = s.store.BuildPlanPurposes(r.Context(), tenantID, result.PlanID)
	writeJSON(w, http.StatusOK, result)
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
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, roomID) {
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

// validatePlanVoiceIdentity refuses to snapshot a voice that no longer exists
// or is disabled: such a plan would fail at live start with 「声音已停用」.
func (s *Server) validatePlanVoiceIdentity(ctx context.Context, tenantID int64, identity *model.LiveAgentVoiceIdentity) error {
	if identity == nil {
		return errors.New("请先选择方案声音")
	}
	if err := normalizeLiveAgentVoiceIdentity(identity); err != nil {
		return err
	}
	if identity.Source == "official" {
		// Shared pool voices do not create private profiles or consume their quota.
		if identity.Provider == ttsgateway.ProviderVolcengine || identity.Model == ttsgateway.VolcengineICL2 || identity.TTSPool == ttsscheduler.PoolHighFidelity {
			if identity.Provider != ttsgateway.ProviderVolcengine || identity.Model != ttsgateway.VolcengineICL2 || identity.TTSPool != ttsscheduler.PoolHighFidelity || identity.ProfileID != 0 || identity.BindingID != 0 {
				return errors.New("高仿真声音身份不匹配，请重新选择声音")
			}
			voice, err := s.findHighFidelityPoolVoice(ctx, identity.VoiceID)
			if err == nil {
				// Only a newly saved snapshot adopts current display data. Never
				// rewrite historical versions or the running program's identity.
				identity.Name = voice.Name
			}
			return err
		}
		settings, err := s.store.VoiceModelSettings(ctx)
		if err != nil {
			return errors.New("读取官方声音配置失败")
		}
		configured := ttsgateway.QwenConfigFromEnv().APIKey != ""
		if settings.OfficialLibrary == ttsgateway.TencentPremiumModel {
			configured = ttsgateway.TencentConfigured()
		}
		library := settings.OfficialLibrary
		if identity.TTSPool != "" {
			if identity.TTSPool != ttsscheduler.PoolStandard {
				return errors.New("高仿真语音池请在我的声音中选择已配置的声音")
			}
			library = ttsgateway.ProviderQwen
			configured, err = s.ttsPoolConfigured(ctx, identity.TTSPool)
			if err != nil {
				return errors.New("读取语音池状态失败")
			}
		}
		for _, v := range voicecatalog.CustomerOfficial(library) {
			provider := v.Provider
			if provider == "" {
				provider = "aliyun_qwen"
			}
			if configured && v.ID == identity.VoiceID && v.Model == identity.Model && provider == identity.Provider {
				return nil
			}
		}
		return errors.New("该官方声音暂不可用，请重新选择声音后再保存")
	}
	profile, err := s.store.GetVoiceProfile(ctx, tenantID, identity.ProfileID)
	if err != nil || selectedCloneProfileError(profile) != nil {
		return errors.New("该声音已失效或已停用，请重新选择声音后再保存")
	}
	if identity.BindingID == 0 {
		voice, modelName, resolveErr := resolveSelectedCloneVoice(profile, fullShowVariantVoiceInput{Source: "clone", ProfileID: identity.ProfileID, VoiceID: identity.VoiceID, Model: identity.Model}, nil)
		if resolveErr != nil || profile.Provider != identity.Provider || voice != identity.VoiceID || modelName != identity.Model {
			return errors.New("所选声音与原始档案不匹配，请重新选择声音后保存")
		}
		return nil
	}
	binding, err := s.store.GetVoiceModelBinding(ctx, tenantID, identity.BindingID)
	if err != nil || binding.Status != "ready" || binding.VoiceID != identity.VoiceID || binding.Model != identity.Model || binding.Provider != identity.Provider || binding.ProfileID != identity.ProfileID {
		return errors.New("该声音已失效或已停用，请重新选择声音后再保存")
	}
	return nil
}

// validateScriptEntriesAgainstP0 用当前 P0 规则检查每条独立话术。
//
// 命中就返回错误，保存入口拦下；错误里逐条列出第几条、命中了什么词、属于哪条规则，
// 前端可以直接展示给用户改。
func (s *Server) validateScriptEntriesAgainstP0(ctx context.Context, input *model.CreateLiveAgentPlanVersionInput) error {
	script, ok := recordingScriptFromContext(input.GenerationContext)
	if !ok || script.Version != 2 {
		return nil
	}
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		return fmt.Errorf("话术违规检测不可用（P0 规则缺失），本次不保存: %w", err)
	}
	var problems []string
	for tabIndex, tab := range script.Tabs {
		for entryIndex, entry := range tab.Entries {
			text := strings.TrimSpace(entry.Text)
			if text == "" {
				continue
			}
			result, err := contentguard.Annotate(text, p0, p1, p2)
			if err != nil {
				return fmt.Errorf("话术违规检测失败，本次不保存: %w", err)
			}
			for _, hit := range result.Hits {
				// 只给中文原因：前端会把含英文标识的整行过滤掉，导致只剩"后台未提供原因"。
				reason := "需核对对应资质或依据"
				if utf8.RuneCountInString(hit.Category) > 0 && !hasASCIILetters(hit.Category) {
					reason = hit.Category
				}
				problems = append(problems, fmt.Sprintf("话术标签「%s」第 %d 条：命中「%s」，%s",
					tab.Title, entryIndex+1, hit.Text, reason))
			}
		}
		_ = tabIndex
	}
	if len(problems) == 0 {
		return nil
	}
	if len(problems) > 10 {
		problems = append(problems[:10], fmt.Sprintf("……另有 %d 处", len(problems)-10))
	}
	return fmt.Errorf("话术有违规内容，修改后才能保存：%s", strings.Join(problems, "；"))
}

// hasASCIILetters 判断字符串里有没有英文字母（用于过滤不适合展示给用户的英文标识）。
func hasASCIILetters(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}
