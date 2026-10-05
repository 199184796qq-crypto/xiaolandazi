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
	if utf8.RuneCountInString(input.RawText) > 30000 {
		writeError(w, http.StatusBadRequest, "单份样本最多3万字，请拆分后再导入")
		return
	}
	item, err := s.store.CreateLiveAnchorStyleSample(r.Context(), tenantID, styleID, actor.UserID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
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
	_, _ = s.store.UpdateLiveAnchorStyleSampleAnalysis(r.Context(), tenantID, styleID, sampleID, sample.Analysis, "", "", 0, "analyzing", 10, "正在读取样本，提取主播表达习惯", "")
	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	profile, response, err := analyzeLiveAgentAnchorStyle(ctx, sample.ReadableText)
	if err != nil {
		_, _ = s.store.UpdateLiveAnchorStyleSampleAnalysis(r.Context(), tenantID, styleID, sampleID, sample.Analysis, response.Provider, response.Model, response.LatencyMS, "failed", 100, "分析失败", "主播风格分析失败，请检查样本后重试")
		writeError(w, http.StatusBadGateway, "主播风格分析失败，请稍后重试")
		return
	}
	qc, qcResponse, _ := s.evaluateAnchorStyleAnalysis(ctx, sample.ReadableText, profile)
	updated, err := s.store.UpdateLiveAnchorStyleSampleAnalysis(r.Context(), tenantID, styleID, sampleID, profile, response.Provider, response.Model, response.LatencyMS+qcResponse.LatencyMS, "analyzed", 100, "分析完成", "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存主播样本分析失败")
		return
	}
	style, err := s.store.GetLiveAnchorStyle(r.Context(), tenantID, styleID)
	if err != nil {
		writeError(w, http.StatusNotFound, "主播风格不存在")
		return
	}
	samples, _ := s.store.ListLiveAnchorStyleSamples(r.Context(), tenantID, styleID)
	merged := style.Profile
	count := 0
	for _, item := range samples {
		if item.AnalysisStatus == "analyzed" {
			count++
			merged = mergeLiveAnchorStyleProfiles(merged, item.Analysis, item.ReadableText, count)
		}
	}
	style, err = s.store.SaveLiveAnchorStyleProfile(r.Context(), tenantID, styleID, actor.UserID, merged)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存主播风格编译结果失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sample": updated, "style": style, "analysis": profile, "style_qc": qc, "provider": response.Provider, "model": response.Model, "latency_ms": response.LatencyMS + qcResponse.LatencyMS})
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
		writeError(w, 500, "绑定主播风格失败")
		return
	}
	writeJSON(w, 200, map[string]any{"bound": true, "style_id": styleID, "plan_id": input.PlanID})
}
