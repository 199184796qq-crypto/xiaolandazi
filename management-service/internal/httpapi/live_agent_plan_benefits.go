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

func validateLiveAgentPlanBenefitCandidate(candidate model.LiveAgentPlanBenefitCandidate) error {
	if strings.TrimSpace(candidate.ActivityPrice) == "" && strings.TrimSpace(candidate.Gift) == "" && strings.TrimSpace(candidate.Activity) == "" {
		return errors.New("活动福利至少要有活动价、赠品或活动内容中的一项")
	}
	if utf8.RuneCountInString(candidate.Key) > 255 {
		return errors.New("活动标识不能超过255字")
	}
	if utf8.RuneCountInString(candidate.LinkKey) > 64 {
		return errors.New("商品链接标识过长")
	}
	if utf8.RuneCountInString(candidate.ProductName) > 255 {
		return errors.New("商品名称不能超过255字")
	}
	if utf8.RuneCountInString(candidate.ActivityPrice) > 255 {
		return errors.New("活动价格内容过长")
	}
	if utf8.RuneCountInString(candidate.Gift) > 512 {
		return errors.New("赠品内容过长")
	}
	if utf8.RuneCountInString(candidate.Activity) > 4000 {
		return errors.New("活动内容不能超过4000字")
	}
	if len(candidate.SourceQuotes) > 20 {
		return errors.New("活动来源依据最多20条")
	}
	for _, quote := range candidate.SourceQuotes {
		if utf8.RuneCountInString(quote) > 2000 {
			return errors.New("活动来源依据单条不能超过2000字")
		}
	}
	switch strings.TrimSpace(candidate.ReviewBucket) {
	case "", "adoptable", "conflict", "discuss", "violation":
	default:
		return errors.New("活动审核分类无效")
	}
	return nil
}

func (s *Server) liveAgentPlanBenefitList(w http.ResponseWriter, r *http.Request) {
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
	items, err := s.store.ListLiveAgentPlanBenefits(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取活动福利失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveAgentPlanBenefitAdopt(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input model.AdoptLiveAgentPlanBenefitsInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "采纳活动福利格式错误")
		return
	}
	if len(input.Benefits) == 0 {
		writeError(w, http.StatusBadRequest, "请至少选择一条活动福利")
		return
	}
	if len(input.Benefits) > 50 {
		writeError(w, http.StatusBadRequest, "单次最多采纳50条活动福利")
		return
	}
	if utf8.RuneCountInString(input.SourceRef) > 255 {
		writeError(w, http.StatusBadRequest, "活动来源标识过长")
		return
	}
	for _, candidate := range input.Benefits {
		if err := validateLiveAgentPlanBenefitCandidate(candidate); err != nil {
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

	output := model.AdoptLiveAgentPlanBenefitsOutput{Results: make([]model.LiveAgentPlanBenefitAdoptionResult, 0, len(input.Benefits))}
	for _, candidate := range input.Benefits {
		candidate.Key = strings.TrimSpace(candidate.Key)
		candidate.LinkKey = strings.TrimSpace(candidate.LinkKey)
		candidate.ProductName = strings.TrimSpace(candidate.ProductName)
		candidate.ActivityPrice = strings.TrimSpace(candidate.ActivityPrice)
		candidate.Gift = strings.TrimSpace(candidate.Gift)
		candidate.Activity = strings.TrimSpace(candidate.Activity)
		candidate.ReviewBucket = strings.TrimSpace(candidate.ReviewBucket)
		if candidate.ReviewBucket == "" {
			candidate.ReviewBucket = "discuss"
		}
		if candidate.ReviewBucket == "violation" {
			output.Blocked++
			output.Results = append(output.Results, model.LiveAgentPlanBenefitAdoptionResult{
				Candidate: candidate, Status: "blocked", Message: "严重违规活动内容不能采纳为正式活动福利",
			})
			continue
		}
		if candidate.ReviewBucket == "conflict" {
			output.Conflicts++
			output.Results = append(output.Results, model.LiveAgentPlanBenefitAdoptionResult{
				Candidate: candidate, Status: "conflict", Message: "该活动候选存在矛盾，需要先通过智能体确认后再采纳",
			})
			continue
		}
		result, err := s.store.AdoptLiveAgentPlanBenefit(
			r.Context(), tenantID, planID, actor.UserID, candidate, strings.TrimSpace(input.SourceRef),
		)
		if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
			writeError(w, http.StatusNotFound, "直播智能体方案不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "采纳活动福利失败："+err.Error())
			return
		}
		switch result.Status {
		case "adopted":
			output.Adopted++
		case "drafted":
			output.Drafted++
		case "conflict":
			output.Conflicts++
		default:
			output.Skipped++
		}
		output.Results = append(output.Results, result)
	}
	writeJSON(w, http.StatusOK, output)
}

func (s *Server) liveAgentPlanBenefitUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	benefitID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("benefitID")), 10, 64)
	if err != nil || benefitID <= 0 {
		writeError(w, http.StatusBadRequest, "活动福利ID无效")
		return
	}
	var input model.UpdateLiveAgentPlanBenefitInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "修改活动福利格式错误")
		return
	}
	candidate := model.LiveAgentPlanBenefitCandidate{
		Key:           strings.TrimSpace(input.Key),
		LinkKey:       strings.TrimSpace(input.LinkKey),
		ProductName:   strings.TrimSpace(input.ProductName),
		ActivityPrice: strings.TrimSpace(input.ActivityPrice),
		Gift:          strings.TrimSpace(input.Gift),
		Activity:      strings.TrimSpace(input.Activity),
		StartsAt:      strings.TrimSpace(input.StartsAt),
		EndsAt:        strings.TrimSpace(input.EndsAt),
		ReviewBucket:  "adoptable",
	}
	if err := validateLiveAgentPlanBenefitCandidate(candidate); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Key = candidate.Key
	input.LinkKey = candidate.LinkKey
	input.ProductName = candidate.ProductName
	input.ActivityPrice = candidate.ActivityPrice
	input.Gift = candidate.Gift
	input.Activity = candidate.Activity
	input.StartsAt = candidate.StartsAt
	input.EndsAt = candidate.EndsAt
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	updated, err := s.store.UpdateLiveAgentPlanBenefit(r.Context(), tenantID, planID, benefitID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanBenefitNotFound) {
		writeError(w, http.StatusNotFound, "正式活动福利不存在")
		return
	}
	if errors.Is(err, appdb.ErrLiveAgentPlanBenefitVersionConflict) {
		writeError(w, http.StatusConflict, "活动福利已经产生新版本，请重新发起修改")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "修改活动福利失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) liveAgentPlanBenefitDelete(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	benefitID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("benefitID")), 10, 64)
	if err != nil || benefitID <= 0 {
		writeError(w, http.StatusBadRequest, "活动福利ID无效")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return
	}
	if err := s.store.DeleteLiveAgentPlanBenefit(r.Context(), tenantID, planID, benefitID, actor.UserID); errors.Is(err, appdb.ErrLiveAgentPlanBenefitNotFound) {
		writeError(w, http.StatusNotFound, "正式活动福利不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "停用活动福利失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
