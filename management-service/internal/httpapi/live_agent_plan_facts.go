package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func validateLiveAgentPlanFactCandidate(candidate model.LiveAgentPlanFactCandidate) error {
	candidate.Key = strings.TrimSpace(candidate.Key)
	candidate.Value = strings.TrimSpace(candidate.Value)
	if candidate.Key == "" || candidate.Value == "" {
		return errors.New("事实名称和事实内容不能为空")
	}
	if utf8.RuneCountInString(candidate.Key) > 255 {
		return errors.New("事实名称不能超过 255 字")
	}
	if utf8.RuneCountInString(candidate.Value) > 4000 {
		return errors.New("事实内容不能超过 4000 字")
	}
	if utf8.RuneCountInString(candidate.SourceQuote) > 2000 {
		return errors.New("原文依据不能超过 2000 字")
	}
	switch strings.TrimSpace(candidate.Category) {
	case "", "product", "link", "trade", "fulfillment", "identity_location", "other":
	default:
		return errors.New("事实分类无效")
	}
	switch strings.TrimSpace(candidate.ReviewBucket) {
	case "", "adoptable", "conflict", "discuss", "violation":
	default:
		return errors.New("事实审核分类无效")
	}
	return nil
}

func (s *Server) liveAgentPlanFactList(w http.ResponseWriter, r *http.Request) {
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
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}
	items, err := s.store.ListLiveAgentPlanFacts(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取正式事实依据失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveAgentPlanFactAdopt(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input model.AdoptLiveAgentPlanFactsInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "采纳事实格式错误")
		return
	}
	if len(input.Facts) == 0 {
		writeError(w, http.StatusBadRequest, "请至少选择一条事实")
		return
	}
	if len(input.Facts) > 100 {
		writeError(w, http.StatusBadRequest, "单次最多采纳 100 条事实")
		return
	}
	if utf8.RuneCountInString(input.SourceRef) > 255 {
		writeError(w, http.StatusBadRequest, "事实来源标识过长")
		return
	}
	for _, candidate := range input.Facts {
		if err := validateLiveAgentPlanFactCandidate(candidate); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}

	output := model.AdoptLiveAgentPlanFactsOutput{
		Results: make([]model.LiveAgentPlanFactAdoptionResult, 0, len(input.Facts)),
	}
	for _, candidate := range input.Facts {
		candidate.Category = strings.TrimSpace(candidate.Category)
		if candidate.Category == "" {
			candidate.Category = "other"
		}
		candidate.Key = strings.TrimSpace(candidate.Key)
		candidate.Value = strings.TrimSpace(candidate.Value)
		candidate.ReviewBucket = strings.TrimSpace(candidate.ReviewBucket)
		if candidate.ReviewBucket == "" {
			candidate.ReviewBucket = "discuss"
		}
		if candidate.ReviewBucket == "violation" {
			output.Blocked++
			output.Results = append(output.Results, model.LiveAgentPlanFactAdoptionResult{
				Candidate: candidate,
				Status:    "blocked",
				Message:   "严重违规事实不能作为正式直播事实采纳",
			})
			continue
		}
		if candidate.ReviewBucket == "conflict" {
			output.Conflicts++
			output.Results = append(output.Results, model.LiveAgentPlanFactAdoptionResult{
				Candidate: candidate,
				Status:    "conflict",
				Message:   "该条分析结果本身存在矛盾，需要先处理冲突后再采纳",
			})
			continue
		}
		result, err := s.store.AdoptLiveAgentPlanFact(
			r.Context(),
			tenantID,
			planID,
			actor.UserID,
			candidate,
			strings.TrimSpace(input.SourceRef),
		)
		if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
			writeError(w, http.StatusNotFound, "直播智能体方案不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "采纳事实到直播方案失败")
			return
		}
		switch result.Status {
		case "adopted":
			output.Adopted++
		case "conflict":
			output.Conflicts++
		default:
			output.Skipped++
		}
		output.Results = append(output.Results, result)
	}
	writeJSON(w, http.StatusOK, output)
}

func liveAgentPlanFactPathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value := strings.TrimSpace(r.PathValue("factID"))
	factID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || factID <= 0 {
		writeError(w, http.StatusBadRequest, "事实编号无效")
		return 0, false
	}
	return factID, true
}

func (s *Server) liveAgentPlanFactUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	factID, ok := liveAgentPlanFactPathID(w, r)
	if !ok {
		return
	}
	var input model.UpdateLiveAgentPlanFactInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "修改事实格式错误")
		return
	}
	candidate := model.LiveAgentPlanFactCandidate{
		Category: strings.TrimSpace(input.Category),
		Key:      strings.TrimSpace(input.Key),
		Value:    strings.TrimSpace(input.Value),
	}
	if err := validateLiveAgentPlanFactCandidate(candidate); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	updated, err := s.store.UpdateLiveAgentPlanFact(
		r.Context(), tenantID, planID, factID, actor.UserID, candidate.Category, candidate.Key, candidate.Value,
	)
	if errors.Is(err, appdb.ErrLiveAgentPlanFactNotFound) {
		writeError(w, http.StatusNotFound, "正式事实不存在")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "same fact key already exists") {
			writeError(w, http.StatusConflict, "同分类下已经存在相同事实名称")
			return
		}
		writeError(w, http.StatusInternalServerError, "修改正式事实失败")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) liveAgentPlanFactDelete(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	factID, ok := liveAgentPlanFactPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return
	}
	if err := s.store.DeleteLiveAgentPlanFact(r.Context(), tenantID, planID, factID, actor.UserID); errors.Is(err, appdb.ErrLiveAgentPlanFactNotFound) {
		writeError(w, http.StatusNotFound, "正式事实不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "删除正式事实失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
