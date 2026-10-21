package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/stylecontract"
)

func (s *Server) registerLiveAnchorStyleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/live-anchor-styles", s.liveAnchorStyleList)
	mux.HandleFunc("POST /api/v1/live-anchor-styles", s.liveAnchorStyleCreate)
	mux.HandleFunc("GET /api/v1/live-anchor-styles/{styleID}", s.liveAnchorStyleGet)
	mux.HandleFunc("PUT /api/v1/live-anchor-styles/{styleID}", s.liveAnchorStyleUpdate)
	mux.HandleFunc("PATCH /api/v1/live-anchor-styles/{styleID}/avatar", s.liveAnchorStyleAvatar)
	mux.HandleFunc("PATCH /api/v1/live-anchor-styles/{styleID}/description", s.liveAnchorDescription)
	mux.HandleFunc("GET /api/v1/live-anchor-styles/{styleID}/analysis-versions", s.liveAnchorAnalysisVersions)
	mux.HandleFunc("POST /api/v1/live-anchor-styles/{styleID}/analysis-versions", s.liveAnchorAnalysisEdit)
	mux.HandleFunc("POST /api/v1/live-anchor-styles/{styleID}/analysis-versions/{versionID}/use", s.liveAnchorAnalysisUse)
	mux.HandleFunc("POST /api/v1/live-anchor-styles/{styleID}/analyze-multiple", s.liveAnchorAnalysisFuse)
	mux.HandleFunc("DELETE /api/v1/live-anchor-styles/{styleID}", s.liveAnchorStyleDelete)
	mux.HandleFunc("GET /api/v1/live-anchor-styles/{styleID}/samples", s.liveAnchorStyleSamples)
	mux.HandleFunc("POST /api/v1/live-anchor-styles/{styleID}/samples", s.liveAnchorStyleSamples)
	mux.HandleFunc("DELETE /api/v1/live-anchor-styles/{styleID}/samples/{sampleID}", s.liveAnchorStyleSampleDelete)
	mux.HandleFunc("POST /api/v1/live-anchor-styles/{styleID}/samples/{sampleID}/analyze", s.liveAnchorStyleSampleAnalyze)
	mux.HandleFunc("GET /api/v1/live-anchor-styles/{styleID}/trainings", s.liveAnchorStyleTrainings)
	mux.HandleFunc("POST /api/v1/live-anchor-styles/{styleID}/trainings", s.liveAnchorStyleTrainings)
	mux.HandleFunc("PUT /api/v1/live-anchor-styles/{styleID}/trainings/{trainingID}", s.liveAnchorStyleTrainingUpdate)
	mux.HandleFunc("DELETE /api/v1/live-anchor-styles/{styleID}/trainings/{trainingID}", s.liveAnchorStyleTrainingDelete)
	mux.HandleFunc("PUT /api/v1/live-anchor-styles/{styleID}/plugins", s.liveAnchorStylePluginUpsert)
	mux.HandleFunc("DELETE /api/v1/live-anchor-styles/{styleID}/plugins/{pluginID}", s.liveAnchorStylePluginDelete)
	mux.HandleFunc("POST /api/v1/live-anchor-styles/{styleID}/bind-plan", s.liveAnchorStyleBindPlan)
	mux.HandleFunc("DELETE /api/v1/live-anchor-styles/{styleID}/bind-plan", s.liveAnchorStyleUnbindPlan)
}

func liveAnchorStylePathID(w http.ResponseWriter, r *http.Request, key, label string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue(key)), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, label+" ID 无效")
		return 0, false
	}
	return id, true
}

func (s *Server) liveAnchorStyleTenant(w http.ResponseWriter, r *http.Request, write bool) (model.Actor, int64, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, 0, false
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), write)
	return actor, tenantID, ok
}

func (s *Server) liveAnchorStyleList(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := s.liveAnchorStyleTenant(w, r, false)
	if !ok {
		return
	}
	items, err := s.store.ListLiveAnchorStyles(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取主播风格失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveAnchorStyleCreate(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	var input model.CreateLiveAnchorStyleInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "主播风格格式错误")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if utf8.RuneCountInString(input.Name) > 80 || utf8.RuneCountInString(input.Description) > 500 {
		writeError(w, http.StatusBadRequest, "主播名称最多80字，介绍最多500字")
		return
	}
	item, err := s.store.CreateLiveAnchorStyle(r.Context(), tenantID, actor.UserID, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建主播风格失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) liveAnchorStyleGet(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := s.liveAnchorStyleTenant(w, r, false)
	if !ok {
		return
	}
	styleID, ok := liveAnchorStylePathID(w, r, "styleID", "主播风格")
	if !ok {
		return
	}
	item, err := s.store.GetLiveAnchorStyle(r.Context(), tenantID, styleID)
	if errors.Is(err, appdb.ErrLiveAnchorStyleNotFound) {
		writeError(w, http.StatusNotFound, "主播风格不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取主播风格失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveAnchorStyleUpdate(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	styleID, ok := liveAnchorStylePathID(w, r, "styleID", "主播风格")
	if !ok {
		return
	}
	var input model.UpdateLiveAnchorStyleInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "主播风格格式错误")
		return
	}
	if input.Profile != nil && input.Profile.Delivery != nil {
		input.Profile.Delivery.AddressingOverride = stylecontract.NormalizeAddressingOverride(input.Profile.Delivery.AddressingOverride)
		input.Profile.Delivery.MetaPromptMD = stylecontract.RenderMetaPrompt(*input.Profile)
		input.Profile.Delivery.Rulebook = stylecontract.Render(*input.Profile)
	}
	if input.StyleCard != nil && input.StyleCard.Delivery != nil {
		input.StyleCard.Delivery.AddressingOverride = stylecontract.NormalizeAddressingOverride(input.StyleCard.Delivery.AddressingOverride)
		input.StyleCard.Delivery.MetaPromptMD = stylecontract.RenderMetaPrompt(*input.StyleCard)
		input.StyleCard.Delivery.Rulebook = stylecontract.Render(*input.StyleCard)
	}
	item, err := s.store.UpdateLiveAnchorStyle(r.Context(), tenantID, styleID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAnchorStyleNotFound) {
		writeError(w, http.StatusNotFound, "主播风格不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveAnchorStyleDelete(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	styleID, ok := liveAnchorStylePathID(w, r, "styleID", "主播风格")
	if !ok {
		return
	}
	err := s.store.DeleteLiveAnchorStyle(r.Context(), tenantID, styleID)
	if errors.Is(err, appdb.ErrLiveAnchorStyleNotFound) {
		writeError(w, http.StatusNotFound, "主播风格不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "删除主播风格失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) liveAnchorStyleSamples(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, ok := s.liveAnchorStyleTenant(w, r, r.Method != http.MethodGet)
	if !ok {
		return
	}
	styleID, ok := liveAnchorStylePathID(w, r, "styleID", "主播风格")
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAnchorStyle(r.Context(), tenantID, styleID); err != nil {
		writeError(w, http.StatusNotFound, "主播风格不存在")
		return
	}
	if r.Method == http.MethodGet {
		items, err := s.store.ListLiveAnchorStyleSamples(r.Context(), tenantID, styleID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取主播样本失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	var input model.CreateLiveAnchorStyleSampleInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "主播样本格式错误")
		return
	}
	var rawCut, readableCut bool
	input.RawText, rawCut = model.TruncateAnchorSample(input.RawText)
	input.ReadableText, readableCut = model.TruncateAnchorSample(input.ReadableText)
	item, err := s.store.CreateLiveAnchorStyleSample(r.Context(), tenantID, styleID, actor.UserID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item.Truncated = rawCut || readableCut
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) liveAnchorStyleSampleDelete(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	styleID, ok := liveAnchorStylePathID(w, r, "styleID", "主播风格")
	if !ok {
		return
	}
	sampleID, ok := liveAnchorStylePathID(w, r, "sampleID", "主播样本")
	if !ok {
		return
	}
	if err := s.store.DeleteLiveAnchorStyleSample(r.Context(), tenantID, styleID, sampleID); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func mergeLiveAnchorStyleProfiles(base, next model.LiveAgentPlanAnchorStyleProfile, source string, sampleCount int) model.LiveAgentPlanAnchorStyleProfile {
	if len(base.Dimensions) == 0 && len(base.ReusableRules) == 0 && len(base.CandidatePatterns) == 0 && base.Delivery == nil {
		return stylecontract.Normalize(next, source)
	}
	result := base
	byKey := map[string]int{}
	for i, item := range result.Dimensions {
		byKey[item.Key] = i
	}
	for _, item := range next.Dimensions {
		if index, exists := byKey[item.Key]; exists {
			if strings.TrimSpace(item.Rule) != "" {
				result.Dimensions[index].Rule = item.Rule
			}
			result.Dimensions[index].EvidenceQuotes = appendUniqueStrings(result.Dimensions[index].EvidenceQuotes, item.EvidenceQuotes...)
			if item.Confidence != "" {
				result.Dimensions[index].Confidence = item.Confidence
			}
		} else {
			result.Dimensions = append(result.Dimensions, item)
			byKey[item.Key] = len(result.Dimensions) - 1
		}
	}
	result.ReusableRules = appendUniqueStrings(result.ReusableRules, next.ReusableRules...)
	result.CandidatePatterns = appendUniqueStrings(result.CandidatePatterns, next.CandidatePatterns...)
	result.ExcludedFromStyle = appendUniqueStrings(result.ExcludedFromStyle, next.ExcludedFromStyle...)
	if result.Delivery == nil {
		result.Delivery = next.Delivery
	} else if next.Delivery != nil {
		result.Delivery.Habits = appendUniqueHabits(result.Delivery.Habits, next.Delivery.Habits...)
	}
	result.Summary = fmt.Sprintf("已综合%d份主播样本，保留跨商品稳定的说话习惯与推进方式。", sampleCount)
	return stylecontract.Normalize(result, source)
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := map[string]bool{}
	for _, value := range dst {
		seen[strings.TrimSpace(value)] = true
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			dst = append(dst, value)
			seen[value] = true
		}
	}
	return dst
}

func appendUniqueHabits(dst []model.LiveAnchorLiteralHabit, values ...model.LiveAnchorLiteralHabit) []model.LiveAnchorLiteralHabit {
	seen := map[string]bool{}
	for _, value := range dst {
		seen[value.Kind+"\x00"+value.Text+"\x00"+value.Position] = true
	}
	for _, value := range values {
		key := value.Kind + "\x00" + value.Text + "\x00" + value.Position
		if strings.TrimSpace(value.Text) != "" && !seen[key] {
			dst = append(dst, value)
			seen[key] = true
		}
	}
	return dst
}

func (s *Server) liveAnchorStyleSampleAnalyze(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	styleID, ok := liveAnchorStylePathID(w, r, "styleID", "主播风格")
	if !ok {
		return
	}
	sampleID, ok := liveAnchorStylePathID(w, r, "sampleID", "主播样本")
	if !ok {
		return
	}
	sample, err := s.store.GetLiveAnchorStyleSample(r.Context(), tenantID, styleID, sampleID)
	if err != nil {
		writeError(w, http.StatusNotFound, "主播样本不存在")
		return
	}
	state, err := s.store.LiveAnchorAnalysisState(r.Context(), tenantID, styleID)
	if err != nil {
		writeError(w, 500, "读取综合风格版本失败")
		return
	}
	w, r = anchorAnalysisStream(w, r)
	reportAnchorAnalysis(r.Context(), 5, "正在读取这份主播话术")
	_, _ = s.store.UpdateLiveAnchorStyleSampleAnalysis(r.Context(), tenantID, styleID, sampleID, sample.Analysis, "", "", 0, "analyzing", 10, "正在读取样本，提取主播表达习惯", "")
	ctx, cancel := context.WithTimeout(r.Context(), 240*time.Second)
	defer cancel()
	source, _ := model.TruncateAnchorSample(sample.ReadableText)
	profile, response, err := s.compileProductionAnchorStyle(ctx, tenantID, styleID, source)
	if err != nil {
		_, _ = s.store.UpdateLiveAnchorStyleSampleAnalysis(r.Context(), tenantID, styleID, sampleID, sample.Analysis, response.Provider, response.Model, response.LatencyMS, "failed", 100, "分析失败", "主播风格分析失败，请检查样本后重试")
		writeError(w, http.StatusBadGateway, "主播风格调教失败："+err.Error())
		return
	}
	currentSample, sampleErr := s.store.GetLiveAnchorStyleSample(ctx, tenantID, styleID, sampleID)
	if sampleErr != nil || currentSample.ReadableText != sample.ReadableText {
		writeError(w, http.StatusConflict, "样本已变化，本次返回已记录但未覆盖新样本")
		return
	}
	updated, err := s.store.UpdateLiveAnchorStyleSampleAnalysis(r.Context(), tenantID, styleID, sampleID, profile, response.Provider, response.Model, response.LatencyMS, "analyzed", 100, "元提示词与程序校对评分基准已保存", "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存主播样本分析失败")
		return
	}
	style, err := s.store.GetLiveAnchorStyle(r.Context(), tenantID, styleID)
	if err != nil {
		writeError(w, http.StatusNotFound, "主播风格不存在")
		return
	}
	samples, err := s.store.ListLiveAnchorStyleSamples(ctx, tenantID, styleID)
	if err != nil {
		writeError(w, 500, "读取已分析样本失败")
		return
	}
	overallUpdated := false
	if model.CanAutoPromoteAnchorSample(samples, sampleID) {
		_, err = s.store.SaveLiveAnchorAnalysisVersion(ctx, tenantID, styleID, actor.UserID, state.ActiveVersionID, "single_sample", []int64{sampleID}, source, profile)
		if err != nil && !errors.Is(err, appdb.ErrAnchorSinglePromotionSkipped) {
			writeAnchorAnalysisError(w, err)
			return
		}
		overallUpdated = err == nil
		style, err = s.store.GetLiveAnchorStyle(ctx, tenantID, styleID)
		if err != nil {
			writeError(w, 500, "读取保存后的风格失败")
			return
		}
	}
	message := "单样本分析已保存，总风格未改变；勾选多样本分析后才能更新总风格"
	if overallUpdated {
		message = "唯一样本分析已保存，并已进入总风格新版本"
	}
	reportAnchorAnalysis(ctx, 100, message)
	updated.Provider, updated.Model = "", ""
	writeJSON(w, http.StatusOK, map[string]any{"sample": updated, "style": style, "analysis": profile, "latency_ms": response.LatencyMS, "overall_updated": overallUpdated, "notice": message})
}

func (s *Server) liveAnchorStyleTrainings(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, ok := s.liveAnchorStyleTenant(w, r, r.Method != http.MethodGet)
	if !ok {
		return
	}
	styleID, ok := liveAnchorStylePathID(w, r, "styleID", "主播风格")
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		items, err := s.store.ListLiveAnchorStyleTrainings(r.Context(), tenantID, styleID)
		if err != nil {
			writeError(w, 500, "读取训练记录失败")
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
		return
	}
	var input model.LiveAnchorStyleTrainingInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "训练记录格式错误")
		return
	}
	item, err := s.store.SaveLiveAnchorStyleTraining(r.Context(), tenantID, styleID, actor.UserID, input)
	if err != nil {
		writeError(w, 500, "保存训练记录失败")
		return
	}
	writeJSON(w, 201, item)
}

func (s *Server) liveAnchorStyleTrainingUpdate(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	styleID, ok := liveAnchorStylePathID(w, r, "styleID", "主播风格")
	if !ok {
		return
	}
	trainingID, ok := liveAnchorStylePathID(w, r, "trainingID", "训练记录")
	if !ok {
		return
	}
	var input model.LiveAnchorStyleTrainingInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "训练记录格式错误")
		return
	}
	item, err := s.store.UpdateLiveAnchorStyleTraining(r.Context(), tenantID, styleID, trainingID, input)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) liveAnchorStyleTrainingDelete(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	styleID, ok := liveAnchorStylePathID(w, r, "styleID", "主播风格")
	if !ok {
		return
	}
	trainingID, ok := liveAnchorStylePathID(w, r, "trainingID", "训练记录")
	if !ok {
		return
	}
	if err := s.store.DeleteLiveAnchorStyleTraining(r.Context(), tenantID, styleID, trainingID); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": true})
}

func (s *Server) liveAnchorStylePluginUpsert(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	styleID, ok := liveAnchorStylePathID(w, r, "styleID", "主播风格")
	if !ok {
		return
	}
	var input model.LiveAnchorStylePluginSetting
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "个性插件设置格式错误")
		return
	}
	if strings.TrimSpace(input.PluginID) == "" {
		writeError(w, 400, "请选择个性插件")
		return
	}
	items, err := s.store.UpsertLiveAnchorStylePlugin(r.Context(), tenantID, styleID, actor.UserID, input)
	if err != nil {
		writeError(w, 500, "保存个性插件设置失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) liveAnchorStylePluginDelete(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	styleID, ok := liveAnchorStylePathID(w, r, "styleID", "主播风格")
	if !ok {
		return
	}
	if err := s.store.DeleteLiveAnchorStylePlugin(r.Context(), tenantID, styleID, r.PathValue("pluginID")); err != nil {
		writeError(w, 500, "卸载个性插件失败")
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": true})
}

func (s *Server) liveAnchorStyleBindPlan(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	styleID, ok := liveAnchorStylePathID(w, r, "styleID", "主播风格")
	if !ok {
		return
	}
	var input struct {
		PlanID int64 `json:"plan_id"`
	}
	if err := readJSON(w, r, &input); err != nil || input.PlanID <= 0 {
		writeError(w, 400, "智能体方案 ID 无效")
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, input.PlanID); err != nil {
		writeError(w, 404, "直播智能体方案不存在")
		return
	}
	if err := s.store.BindLiveAnchorStyleToPlan(r.Context(), tenantID, styleID, input.PlanID, actor.UserID); err != nil {
		if errors.Is(err, appdb.ErrLiveAnchorStyleNotFound) || errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
			writeError(w, 404, "当前方案或主播不存在")
			return
		}
		writeError(w, 500, "绑定主播风格失败")
		return
	}
	writeJSON(w, 200, map[string]any{"bound": true, "style_id": styleID, "plan_id": input.PlanID})
}

func (s *Server) liveAnchorStyleUnbindPlan(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	styleID, ok := liveAnchorStylePathID(w, r, "styleID", "主播风格")
	if !ok {
		return
	}
	var input struct {
		PlanID int64 `json:"plan_id"`
	}
	if err := readJSON(w, r, &input); err != nil || input.PlanID <= 0 {
		writeError(w, 400, "智能体方案 ID 无效")
		return
	}
	err := s.store.UnbindLiveAnchorStyleFromPlan(r.Context(), tenantID, styleID, input.PlanID)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, 404, "当前智能体方案不存在")
		return
	}
	if errors.Is(err, appdb.ErrLiveAnchorBindingChanged) {
		writeError(w, 409, "当前使用的主播已变化，请刷新后重试")
		return
	}
	if err != nil {
		writeError(w, 500, "取消使用主播失败，请重试")
		return
	}
	writeJSON(w, 200, map[string]any{"unbound": true, "style_id": styleID, "plan_id": input.PlanID})
}
