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

func validateLiveAgentPlanProductLinkCandidate(candidate model.LiveAgentPlanProductLinkCandidate) error {
	if strings.TrimSpace(candidate.LinkKey) == "" {
		return errors.New("商品链接编号不能为空")
	}
	if utf8.RuneCountInString(candidate.LinkKey) > 64 {
		return errors.New("商品链接编号过长")
	}
	if strings.TrimSpace(candidate.ProductName) == "" {
		return errors.New("商品名称不能为空")
	}
	if utf8.RuneCountInString(candidate.ProductName) > 255 {
		return errors.New("商品名称不能超过255字")
	}
	if utf8.RuneCountInString(candidate.Spec) > 255 ||
		utf8.RuneCountInString(candidate.DailyPrice) > 255 ||
		utf8.RuneCountInString(candidate.Quantity) > 255 {
		return errors.New("商品规格、日常价或数量内容过长")
	}
	if utf8.RuneCountInString(candidate.Audience) > 512 {
		return errors.New("商品适用说明不能超过512字")
	}
	if len(candidate.SourceQuotes) > 20 {
		return errors.New("商品来源依据最多20条")
	}
	for _, quote := range candidate.SourceQuotes {
		if utf8.RuneCountInString(quote) > 2000 {
			return errors.New("商品来源依据单条不能超过2000字")
		}
	}
	switch strings.TrimSpace(candidate.ReviewBucket) {
	case "", "adoptable", "conflict", "discuss", "violation":
	default:
		return errors.New("商品审核分类无效")
	}
	return nil
}

func (s *Server) liveAgentPlanProductLinkList(w http.ResponseWriter, r *http.Request) {
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
	items, err := s.store.ListLiveAgentPlanProductLinks(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取正式商品链接失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveAgentPlanProductLinkAdopt(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input model.AdoptLiveAgentPlanProductLinksInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "采纳商品链接格式错误")
		return
	}
	if len(input.Links) == 0 {
		writeError(w, http.StatusBadRequest, "请至少选择一条商品链接")
		return
	}
	if len(input.Links) > 30 {
		writeError(w, http.StatusBadRequest, "单次最多采纳30条商品链接")
		return
	}
	if utf8.RuneCountInString(input.SourceRef) > 255 {
		writeError(w, http.StatusBadRequest, "商品来源标识过长")
		return
	}
	for _, candidate := range input.Links {
		if err := validateLiveAgentPlanProductLinkCandidate(candidate); err != nil {
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

	output := model.AdoptLiveAgentPlanProductLinksOutput{Results: make([]model.LiveAgentPlanProductLinkAdoptionResult, 0, len(input.Links))}
	for _, candidate := range input.Links {
		candidate.LinkKey = canonicalPlanProductLinkKey(candidate.LinkKey)
		candidate.ProductName = strings.TrimSpace(candidate.ProductName)
		candidate.Spec = strings.TrimSpace(candidate.Spec)
		candidate.DailyPrice = strings.TrimSpace(candidate.DailyPrice)
		candidate.Quantity = strings.TrimSpace(candidate.Quantity)
		candidate.Audience = strings.TrimSpace(candidate.Audience)
		candidate.ReviewBucket = strings.TrimSpace(candidate.ReviewBucket)
		if candidate.ReviewBucket == "" {
			candidate.ReviewBucket = "discuss"
		}
		if candidate.ReviewBucket == "violation" {
			output.Blocked++
			output.Results = append(output.Results, model.LiveAgentPlanProductLinkAdoptionResult{
				Candidate: candidate, Status: "blocked", Message: "严重违规商品候选不能采纳为正式商品链接",
			})
			continue
		}
		if candidate.ReviewBucket == "conflict" {
			output.Conflicts++
			output.Results = append(output.Results, model.LiveAgentPlanProductLinkAdoptionResult{
				Candidate: candidate, Status: "conflict", Message: "该商品候选存在矛盾，需要先通过智能体确认后再采纳",
			})
			continue
		}
		result, err := s.store.AdoptLiveAgentPlanProductLink(
			r.Context(), tenantID, planID, actor.UserID, candidate, strings.TrimSpace(input.SourceRef),
		)
		if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
			writeError(w, http.StatusNotFound, "直播智能体方案不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "采纳商品链接失败："+err.Error())
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

func liveAgentPlanProductLinkPathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value := strings.TrimSpace(r.PathValue("productLinkID"))
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "商品链接记录编号无效")
		return 0, false
	}
	return id, true
}

func (s *Server) liveAgentPlanProductLinkUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	productLinkID, ok := liveAgentPlanProductLinkPathID(w, r)
	if !ok {
		return
	}

	var input model.UpdateLiveAgentPlanProductLinkInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "修改商品链接格式错误")
		return
	}
	candidate := model.LiveAgentPlanProductLinkCandidate{
		LinkKey:      strings.TrimSpace(input.LinkKey),
		ProductName:  strings.TrimSpace(input.ProductName),
		Spec:         strings.TrimSpace(input.Spec),
		DailyPrice:   strings.TrimSpace(input.DailyPrice),
		Quantity:     strings.TrimSpace(input.Quantity),
		Audience:     strings.TrimSpace(input.Audience),
		ReviewBucket: "adoptable",
	}
	if err := validateLiveAgentPlanProductLinkCandidate(candidate); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.LinkKey = canonicalPlanProductLinkKey(candidate.LinkKey)
	input.ProductName = candidate.ProductName
	input.Spec = candidate.Spec
	input.DailyPrice = candidate.DailyPrice
	input.Quantity = candidate.Quantity
	input.Audience = candidate.Audience

	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	updated, err := s.store.UpdateLiveAgentPlanProductLink(r.Context(), tenantID, planID, productLinkID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanProductLinkNotFound) {
		writeError(w, http.StatusNotFound, "正式商品链接不存在")
		return
	}
	if errors.Is(err, appdb.ErrLiveAgentPlanProductLinkVersionConflict) {
		writeError(w, http.StatusConflict, "商品链接已经产生新版本，请刷新后重新修改")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "same product link key already exists") {
			writeError(w, http.StatusConflict, "当前方案已经存在相同链接编号")
			return
		}
		writeError(w, http.StatusInternalServerError, "修改正式商品链接失败")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) liveAgentPlanProductLinkDelete(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	productLinkID, ok := liveAgentPlanProductLinkPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return
	}
	if err := s.store.DeleteLiveAgentPlanProductLink(r.Context(), tenantID, planID, productLinkID, actor.UserID); errors.Is(err, appdb.ErrLiveAgentPlanProductLinkNotFound) {
		writeError(w, http.StatusNotFound, "正式商品链接不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "删除正式商品链接失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
