package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type liveStrategyExecuteAction struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type liveStrategyExecuteRequest struct {
	Action liveStrategyExecuteAction `json:"action"`
}

type liveStrategyExecutePayload struct {
	PlanID             int64  `json:"plan_id,omitempty"`
	RoomID             int64  `json:"room_id,omitempty"`
	ProductLinkID      int64  `json:"product_link_id,omitempty"`
	BenefitID          int64  `json:"benefit_id,omitempty"`
	FactID             int64  `json:"fact_id,omitempty"`
	CurrentVersionNo   int64  `json:"current_version_no,omitempty"`
	LinkKey            string `json:"link_key,omitempty"`
	ProductName        string `json:"product_name,omitempty"`
	Spec               string `json:"spec,omitempty"`
	DailyPrice         string `json:"daily_price,omitempty"`
	Quantity           string `json:"quantity,omitempty"`
	Audience           string `json:"audience,omitempty"`
	BenefitKey         string `json:"benefit_key,omitempty"`
	ActivityPrice      string `json:"activity_price,omitempty"`
	Gift               string `json:"gift,omitempty"`
	Activity           string `json:"activity,omitempty"`
	StartsAt           string `json:"starts_at,omitempty"`
	EndsAt             string `json:"ends_at,omitempty"`
	ReviewBucket       string `json:"review_bucket,omitempty"`
	ReviewReason       string `json:"review_reason,omitempty"`
	FactCategory       string `json:"fact_category,omitempty"`
	FactKey            string `json:"fact_key,omitempty"`
	FactValue          string `json:"fact_value,omitempty"`
	CurrentFactValue   string `json:"current_fact_value,omitempty"`
	ScriptReferenceID  int64  `json:"script_reference_id,omitempty"`
	ScriptReferenceKey string `json:"script_reference_key,omitempty"`
	ScriptTitle        string `json:"script_title,omitempty"`
	ScriptText         string `json:"script_text,omitempty"`
	CurrentScriptText  string `json:"current_script_text,omitempty"`
	ScriptGoal         string `json:"script_goal,omitempty"`
	ScriptTransition   string `json:"script_transition,omitempty"`
	ExecutionMode      string `json:"execution_mode,omitempty"`
	TargetPlanID       int64  `json:"target_plan_id,omitempty"`
	TargetPlanName     string `json:"target_plan_name,omitempty"`
	CurrentPlanID      int64  `json:"current_plan_id,omitempty"`
	SourceText         string `json:"source_text,omitempty"`
}

var liveStrategyExecutableActions = map[string]struct{}{
	"add_live_product":                      {},
	"confirm_live_product_update":           {},
	"confirm_live_product_disable":          {},
	"add_live_benefit":                      {},
	"confirm_live_benefit_update":           {},
	"confirm_live_benefit_disable":          {},
	"add_live_fact":                         {},
	"confirm_live_fact_update":              {},
	"confirm_live_fact_disable":             {},
	"add_live_script_reference":             {},
	"confirm_live_script_reference_update":  {},
	"confirm_live_script_reference_disable": {},
	"confirm_live_plan_bind":                {},
	"confirm_live_plan_unbind":              {},
	"confirm_live_plan_switch":              {},
}

func (s *Server) liveStrategyExecuteAction(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, roomID, ok := s.requireLiveStrategyAgentWrite(w, r)
	if !ok {
		return
	}
	var input liveStrategyExecuteRequest
	if err := readJSON(w, r, &input); err != nil {
		writeLiveStrategyActionFailure(w, "invalid_request", "确认动作格式错误。", nil)
		return
	}
	actionType := strings.TrimSpace(input.Action.Type)
	if _, supported := liveStrategyExecutableActions[actionType]; !supported {
		writeLiveStrategyActionFailure(w, "unsupported_action", "这个动作还没有接入直播策略统一执行器。", nil)
		return
	}
	var payload liveStrategyExecutePayload
	if err := json.Unmarshal(input.Action.Payload, &payload); err != nil {
		writeLiveStrategyActionFailure(w, "invalid_payload", "确认动作参数格式错误。", nil)
		return
	}
	if payload.RoomID != 0 && payload.RoomID != roomID {
		writeLiveStrategyActionFailure(w, "room_mismatch", "确认卡所属直播间已经变化，请重新发起操作。", nil)
		return
	}
	payload.RoomID = roomID

	var result systemAgentChatOutput
	switch actionType {
	case "add_live_product":
		result = s.executeLiveStrategyAddProduct(r, actor, tenantID, payload)
	case "confirm_live_product_update":
		result = s.executeLiveStrategyUpdateProduct(r, actor, tenantID, payload)
	case "confirm_live_product_disable":
		result = s.executeLiveStrategyDisableProduct(r, actor, tenantID, payload)
	case "add_live_benefit":
		result = s.executeLiveStrategyAddBenefit(r, actor, tenantID, payload)
	case "confirm_live_benefit_update":
		result = s.executeLiveStrategyUpdateBenefit(r, actor, tenantID, payload)
	case "confirm_live_benefit_disable":
		result = s.executeLiveStrategyDisableBenefit(r, actor, tenantID, payload)
	case "add_live_fact":
		result = s.executeLiveStrategyAddFact(r, actor, tenantID, payload)
	case "confirm_live_fact_update":
		result = s.executeLiveStrategyUpdateFact(r, actor, tenantID, payload)
	case "confirm_live_fact_disable":
		result = s.executeLiveStrategyDisableFact(r, actor, tenantID, payload)
	case "add_live_script_reference":
		result = s.executeLiveStrategyAddScriptReference(r, actor, tenantID, payload)
	case "confirm_live_script_reference_update":
		result = s.executeLiveStrategyUpdateScriptReference(r, actor, tenantID, payload)
	case "confirm_live_script_reference_disable":
		result = s.executeLiveStrategyDisableScriptReference(r, actor, tenantID, payload)
	case "confirm_live_plan_bind":
		result = s.executeLiveStrategyBindPlan(r, actor, tenantID, payload)
	case "confirm_live_plan_unbind":
		result = s.executeLiveStrategyUnbindPlan(r, actor, tenantID, payload)
	case "confirm_live_plan_switch":
		result = s.executeLiveStrategySwitchPlan(r, actor, tenantID, payload)
	}
	if result.State == "" {
		result.State = agentStateFailed
	}
	if result.Capabilities == nil {
		result.Capabilities = []string{"live_strategy"}
	}
	s.auditLiveStrategyAgentCommand(r, actor, actionType, result.State, payload, result.Code)
	writeAgentChatOutput(w, http.StatusOK, result)
}

func (s *Server) requireLiveStrategyAgentWrite(
	w http.ResponseWriter,
	r *http.Request,
) (model.Actor, int64, int64, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, 0, 0, false
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return model.Actor{}, 0, 0, false
	}

	if actor.IsInternalStaff() && !actor.IsPlatformAdmin() {
		access, err := s.staffAccessForActor(r, actor)
		if err != nil || !staffHasPermission(access, "liveops.configure") {
			writeAgentChatOutput(w, http.StatusOK, systemAgentChatOutput{
				State:              agentStatePermissionDenied,
				Code:               "permission_denied",
				RequiredPermission: "liveops.configure",
				Reply:              "你当前没有直播智能体方案配置权限，不能通过智能体修改客户直播策略。",
				Capabilities:       []string{"live_strategy"},
			})
			return model.Actor{}, 0, 0, false
		}
	}

	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return model.Actor{}, 0, 0, false
	}
	if _, err := s.getCoreRoomState(r.Context(), tenantID, roomID); err != nil {
		writeLiveStrategyActionFailure(w, "room_not_found", "直播间不存在或不属于当前账号范围。", nil)
		return model.Actor{}, 0, 0, false
	}
	return actor, tenantID, roomID, true
}

func (s *Server) requireBoundLiveStrategyPlan(
	r *http.Request,
	tenantID, roomID, planID int64,
) (model.LiveAgentPlan, error) {
	if planID <= 0 {
		return model.LiveAgentPlan{}, errors.New("plan_required")
	}
	plan, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID)
	if err != nil || plan.Status != "active" {
		return model.LiveAgentPlan{}, errors.New("plan_not_found")
	}
	bound, err := s.store.IsRoomBoundToLiveAgentPlan(r.Context(), tenantID, roomID, planID)
	if err != nil {
		return model.LiveAgentPlan{}, err
	}
	if !bound {
		return model.LiveAgentPlan{}, errors.New("plan_not_bound")
	}
	return plan, nil
}

func liveStrategyFailed(code, reply string, data any) systemAgentChatOutput {
	return systemAgentChatOutput{
		State: agentStateFailed, Code: code, Reply: reply,
		Capabilities: []string{"live_strategy"}, Data: data,
	}
}

func liveStrategySucceeded(code, reply string, data any) systemAgentChatOutput {
	return systemAgentChatOutput{
		State: agentStateSucceeded, Code: code, Reply: reply,
		Capabilities: []string{"live_strategy"}, Data: data,
	}
}

func writeLiveStrategyActionFailure(w http.ResponseWriter, code, reply string, data any) {
	writeAgentChatOutput(w, http.StatusOK, liveStrategyFailed(code, reply, data))
}

func (s *Server) executeLiveStrategyAddProduct(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if _, err := s.requireBoundLiveStrategyPlan(r, tenantID, p.RoomID, p.PlanID); err != nil {
		return liveStrategyFailed("plan_scope_changed", "当前方案已经不再绑定这个直播间，请刷新后重新发起。", nil)
	}
	candidate := model.LiveAgentPlanProductLinkCandidate{
		LinkKey: canonicalPlanProductLinkKey(p.LinkKey), ProductName: strings.TrimSpace(p.ProductName),
		Spec: strings.TrimSpace(p.Spec), DailyPrice: strings.TrimSpace(p.DailyPrice),
		Quantity: strings.TrimSpace(p.Quantity), Audience: strings.TrimSpace(p.Audience),
		ReviewBucket: "adoptable", SourceQuotes: []string{strings.TrimSpace(p.SourceText)},
	}
	if err := validateLiveAgentPlanProductLinkCandidate(candidate); err != nil {
		return liveStrategyFailed("invalid_product", err.Error(), nil)
	}
	current, err := s.store.ListLiveAgentPlanProductLinks(r.Context(), tenantID, p.PlanID)
	if err != nil {
		return liveStrategyFailed("product_lookup_failed", "读取当前商品链接失败，请稍后重试。", nil)
	}
	for _, item := range current {
		if item.LinkKey == candidate.LinkKey {
			return liveStrategyFailed("stale_confirmation", "确认期间这个链接已经存在。为避免把新增悄悄变成修改，请重新发起。", nil)
		}
	}
	result, err := s.store.AdoptLiveAgentPlanProductLink(r.Context(), tenantID, p.PlanID, actor.UserID, candidate, "system-agent:intent-product")
	if err != nil || result.Status != "adopted" || result.Saved == nil {
		msg := result.Message
		if msg == "" {
			msg = "商品链接没有写入，请重新核对。"
		}
		return liveStrategyFailed("product_add_failed", msg, nil)
	}
	return liveStrategySucceeded("product_added", "已确认添加“"+candidate.LinkKey+" · "+candidate.ProductName+"”，正式版本为 V"+strconv.FormatInt(result.Saved.VersionNo, 10)+"。", map[string]any{"module": "products", "plan_id": p.PlanID, "version_no": result.Saved.VersionNo})
}

func (s *Server) executeLiveStrategyUpdateProduct(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if _, err := s.requireBoundLiveStrategyPlan(r, tenantID, p.RoomID, p.PlanID); err != nil {
		return liveStrategyFailed("plan_scope_changed", "当前方案已经不再绑定这个直播间，请重新发起修改。", nil)
	}
	items, err := s.store.ListLiveAgentPlanProductLinks(r.Context(), tenantID, p.PlanID)
	if err != nil {
		return liveStrategyFailed("product_lookup_failed", "读取当前商品链接失败。", nil)
	}
	var current *model.LiveAgentPlanProductLink
	for i := range items {
		if items[i].ID == p.ProductLinkID && items[i].LinkKey == strings.TrimSpace(p.LinkKey) {
			current = &items[i]
			break
		}
	}
	if current == nil || (p.CurrentVersionNo > 0 && current.VersionNo != p.CurrentVersionNo) {
		return liveStrategyFailed("stale_confirmation", "这条商品链接已经变化，请重新发起修改，避免覆盖新版本。", nil)
	}
	input := model.UpdateLiveAgentPlanProductLinkInput{
		ExpectedVersionNo: current.VersionNo,
		LinkKey:           current.LinkKey, ProductName: strings.TrimSpace(p.ProductName), Spec: strings.TrimSpace(p.Spec),
		DailyPrice: strings.TrimSpace(p.DailyPrice), Quantity: strings.TrimSpace(p.Quantity), Audience: strings.TrimSpace(p.Audience),
	}
	candidate := model.LiveAgentPlanProductLinkCandidate{LinkKey: input.LinkKey, ProductName: input.ProductName, Spec: input.Spec, DailyPrice: input.DailyPrice, Quantity: input.Quantity, Audience: input.Audience, ReviewBucket: "adoptable"}
	if err := validateLiveAgentPlanProductLinkCandidate(candidate); err != nil {
		return liveStrategyFailed("invalid_product", err.Error(), nil)
	}
	if current.ProductName == input.ProductName && current.Spec == input.Spec && current.DailyPrice == input.DailyPrice && current.Quantity == input.Quantity && current.Audience == input.Audience {
		return liveStrategyFailed("no_change", "新值和当前正式商品数据一致，没有生成新版本。", nil)
	}
	updated, err := s.store.UpdateLiveAgentPlanProductLink(r.Context(), tenantID, p.PlanID, current.ID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanProductLinkVersionConflict) {
		return liveStrategyFailed("stale_confirmation", "这条商品链接已经产生新版本，请重新发起修改。", nil)
	}
	if err != nil {
		return liveStrategyFailed("product_update_failed", "修改商品链接失败，请刷新后重试。", nil)
	}
	return liveStrategySucceeded("product_updated", "已确认修改“"+updated.LinkKey+"”，正式版本更新为 V"+strconv.FormatInt(updated.VersionNo, 10)+"。", map[string]any{"module": "products", "plan_id": p.PlanID, "version_no": updated.VersionNo})
}

func (s *Server) executeLiveStrategyDisableProduct(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if _, err := s.requireBoundLiveStrategyPlan(r, tenantID, p.RoomID, p.PlanID); err != nil {
		return liveStrategyFailed("plan_scope_changed", "当前方案已经不再绑定这个直播间。", nil)
	}
	items, err := s.store.ListLiveAgentPlanProductLinks(r.Context(), tenantID, p.PlanID)
	if err != nil {
		return liveStrategyFailed("product_lookup_failed", "读取当前商品链接失败。", nil)
	}
	var current *model.LiveAgentPlanProductLink
	for i := range items {
		if items[i].ID == p.ProductLinkID {
			current = &items[i]
			break
		}
	}
	if current == nil || (p.CurrentVersionNo > 0 && current.VersionNo != p.CurrentVersionNo) {
		return liveStrategyFailed("stale_confirmation", "这条商品链接已经变化或被停用，请重新发起。", nil)
	}
	if err := s.store.DeleteLiveAgentPlanProductLinkWithExpectedVersion(r.Context(), tenantID, p.PlanID, current.ID, actor.UserID, current.VersionNo); errors.Is(err, appdb.ErrLiveAgentPlanProductLinkVersionConflict) {
		return liveStrategyFailed("stale_confirmation", "这条商品链接已经产生新版本，请重新发起停用。", nil)
	} else if err != nil {
		return liveStrategyFailed("product_disable_failed", "停用商品链接失败，请稍后重试。", nil)
	}
	return liveStrategySucceeded("product_disabled", "已确认停用“"+current.LinkKey+"”。历史版本和审计记录仍保留。", map[string]any{"module": "products", "plan_id": p.PlanID})
}

func (s *Server) executeLiveStrategyAddBenefit(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if _, err := s.requireBoundLiveStrategyPlan(r, tenantID, p.RoomID, p.PlanID); err != nil {
		return liveStrategyFailed("plan_scope_changed", "当前方案已经不再绑定这个直播间。", nil)
	}
	candidate := model.LiveAgentPlanBenefitCandidate{
		Key: strings.TrimSpace(p.BenefitKey), LinkKey: strings.TrimSpace(p.LinkKey), ProductName: strings.TrimSpace(p.ProductName),
		ActivityPrice: strings.TrimSpace(p.ActivityPrice), Gift: strings.TrimSpace(p.Gift), Activity: strings.TrimSpace(p.Activity),
		StartsAt: strings.TrimSpace(p.StartsAt), EndsAt: strings.TrimSpace(p.EndsAt), ReviewBucket: strings.TrimSpace(p.ReviewBucket),
		ReviewReason: strings.TrimSpace(p.ReviewReason), SourceQuotes: []string{strings.TrimSpace(p.SourceText)},
	}
	if candidate.ReviewBucket == "" {
		candidate.ReviewBucket = "discuss"
	}
	if err := validateLiveAgentPlanBenefitCandidate(candidate); err != nil {
		return liveStrategyFailed("invalid_benefit", err.Error(), nil)
	}
	result, err := s.store.AdoptLiveAgentPlanBenefit(r.Context(), tenantID, p.PlanID, actor.UserID, candidate, "system-agent:intent-benefit")
	if err != nil {
		return liveStrategyFailed("benefit_add_failed", "写入活动福利失败，请稍后重试。", nil)
	}
	if (result.Status != "adopted" && result.Status != "drafted") || result.Saved == nil {
		msg := result.Message
		if msg == "" {
			msg = "活动福利没有写入，请重新核对。"
		}
		return liveStrategyFailed("benefit_conflict", msg, nil)
	}
	statusText := "当前为草稿"
	if result.Saved.Status == "active" {
		statusText = "当前已生效"
	} else if result.Saved.Status == "expired" {
		statusText = "当前已过期"
	}
	return liveStrategySucceeded("benefit_added", "已确认添加活动福利，正式版本为 V"+strconv.FormatInt(result.Saved.VersionNo, 10)+"，"+statusText+"。", map[string]any{"module": "benefits", "plan_id": p.PlanID, "version_no": result.Saved.VersionNo, "status": result.Saved.Status})
}

func (s *Server) executeLiveStrategyUpdateBenefit(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if _, err := s.requireBoundLiveStrategyPlan(r, tenantID, p.RoomID, p.PlanID); err != nil {
		return liveStrategyFailed("plan_scope_changed", "当前方案已经不再绑定这个直播间。", nil)
	}
	items, err := s.store.ListLiveAgentPlanBenefits(r.Context(), tenantID, p.PlanID)
	if err != nil {
		return liveStrategyFailed("benefit_lookup_failed", "读取当前活动福利失败。", nil)
	}
	var current *model.LiveAgentPlanBenefit
	for i := range items {
		if items[i].ID == p.BenefitID && items[i].Key == strings.TrimSpace(p.BenefitKey) {
			current = &items[i]
			break
		}
	}
	if current == nil || (p.CurrentVersionNo > 0 && current.VersionNo != p.CurrentVersionNo) {
		return liveStrategyFailed("stale_confirmation", "这条活动福利已经变化，请重新发起修改。", nil)
	}
	input := model.UpdateLiveAgentPlanBenefitInput{
		ExpectedVersionNo: current.VersionNo, Key: current.Key, LinkKey: current.LinkKey,
		ProductName: strings.TrimSpace(p.ProductName), ActivityPrice: strings.TrimSpace(p.ActivityPrice),
		Gift: strings.TrimSpace(p.Gift), Activity: strings.TrimSpace(p.Activity),
		StartsAt: strings.TrimSpace(p.StartsAt), EndsAt: strings.TrimSpace(p.EndsAt),
	}
	candidate := model.LiveAgentPlanBenefitCandidate{Key: input.Key, LinkKey: input.LinkKey, ProductName: input.ProductName, ActivityPrice: input.ActivityPrice, Gift: input.Gift, Activity: input.Activity, StartsAt: input.StartsAt, EndsAt: input.EndsAt, ReviewBucket: "adoptable"}
	if err := validateLiveAgentPlanBenefitCandidate(candidate); err != nil {
		return liveStrategyFailed("invalid_benefit", err.Error(), nil)
	}
	updated, err := s.store.UpdateLiveAgentPlanBenefit(r.Context(), tenantID, p.PlanID, current.ID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanBenefitVersionConflict) {
		return liveStrategyFailed("stale_confirmation", "这条活动福利已经产生新版本，请重新发起修改。", nil)
	}
	if err != nil {
		return liveStrategyFailed("benefit_update_failed", "修改活动福利失败，请刷新后重试。", nil)
	}
	if updated.VersionNo == current.VersionNo {
		return liveStrategyFailed("no_change", "新值和当前活动福利一致，没有生成新版本。", nil)
	}
	statusText := "当前为草稿"
	if updated.Status == "active" {
		statusText = "当前已生效"
	} else if updated.Status == "expired" {
		statusText = "当前已过期"
	}
	return liveStrategySucceeded("benefit_updated", "已确认修改活动福利，正式版本更新为 V"+strconv.FormatInt(updated.VersionNo, 10)+"，"+statusText+"。", map[string]any{"module": "benefits", "plan_id": p.PlanID, "version_no": updated.VersionNo, "status": updated.Status})
}

func (s *Server) executeLiveStrategyDisableBenefit(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if _, err := s.requireBoundLiveStrategyPlan(r, tenantID, p.RoomID, p.PlanID); err != nil {
		return liveStrategyFailed("plan_scope_changed", "当前方案已经不再绑定这个直播间。", nil)
	}
	items, err := s.store.ListLiveAgentPlanBenefits(r.Context(), tenantID, p.PlanID)
	if err != nil {
		return liveStrategyFailed("benefit_lookup_failed", "读取当前活动福利失败。", nil)
	}
	var current *model.LiveAgentPlanBenefit
	for i := range items {
		if items[i].ID == p.BenefitID {
			current = &items[i]
			break
		}
	}
	if current == nil || (p.CurrentVersionNo > 0 && current.VersionNo != p.CurrentVersionNo) {
		return liveStrategyFailed("stale_confirmation", "这条活动福利已经变化或被停用，请重新发起。", nil)
	}
	if err := s.store.DeleteLiveAgentPlanBenefitWithExpectedVersion(r.Context(), tenantID, p.PlanID, current.ID, actor.UserID, current.VersionNo); errors.Is(err, appdb.ErrLiveAgentPlanBenefitVersionConflict) {
		return liveStrategyFailed("stale_confirmation", "这条活动福利已经产生新版本，请重新发起停用。", nil)
	} else if err != nil {
		return liveStrategyFailed("benefit_disable_failed", "停用活动福利失败，请稍后重试。", nil)
	}
	return liveStrategySucceeded("benefit_disabled", "已确认停用这条活动福利。它不会再进入直播生成，历史版本和审计记录仍保留。", map[string]any{"module": "benefits", "plan_id": p.PlanID})
}

func (s *Server) executeLiveStrategyAddFact(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if _, err := s.requireBoundLiveStrategyPlan(r, tenantID, p.RoomID, p.PlanID); err != nil {
		return liveStrategyFailed("plan_scope_changed", "当前方案已经不再绑定这个直播间。", nil)
	}
	candidate := model.LiveAgentPlanFactCandidate{
		Category: strings.TrimSpace(p.FactCategory), Key: strings.TrimSpace(p.FactKey), Value: strings.TrimSpace(p.FactValue),
		Status: "confirmed", ReviewBucket: "adoptable", SourceQuote: strings.TrimSpace(p.SourceText),
	}
	if err := validateLiveAgentPlanFactCandidate(candidate); err != nil {
		return liveStrategyFailed("invalid_fact", err.Error(), nil)
	}
	items, err := s.store.ListLiveAgentPlanFacts(r.Context(), tenantID, p.PlanID)
	if err != nil {
		return liveStrategyFailed("fact_lookup_failed", "读取当前事实依据失败。", nil)
	}
	for _, item := range items {
		if item.Category == candidate.Category && item.Key == candidate.Key {
			return liveStrategyFailed("stale_confirmation", "确认期间这条事实已经存在。为了避免把新增悄悄变成修改，请重新发起。", nil)
		}
	}
	result, err := s.store.AdoptLiveAgentPlanFact(r.Context(), tenantID, p.PlanID, actor.UserID, candidate, "system-agent:intent-fact")
	if err != nil || result.Status != "adopted" || result.Saved == nil {
		msg := result.Message
		if msg == "" {
			msg = "事实依据没有写入，请重新核对。"
		}
		return liveStrategyFailed("fact_add_failed", msg, nil)
	}
	if err := s.hotReloadLiveAgentPlanRooms(r.Context(), tenantID, p.PlanID, "facts"); err != nil {
		return liveStrategyFailed("runtime_sync_failed", "事实已保存，但热同步直播间失败。", map[string]any{"error": err.Error()})
	}
	return liveStrategySucceeded("fact_added", "已确认添加事实“"+candidate.Key+"："+candidate.Value+"”。", map[string]any{"module": "knowledge", "plan_id": p.PlanID, "version_no": result.Saved.VersionNo})
}

func (s *Server) executeLiveStrategyUpdateFact(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if _, err := s.requireBoundLiveStrategyPlan(r, tenantID, p.RoomID, p.PlanID); err != nil {
		return liveStrategyFailed("plan_scope_changed", "当前方案已经不再绑定这个直播间。", nil)
	}
	items, err := s.store.ListLiveAgentPlanFacts(r.Context(), tenantID, p.PlanID)
	if err != nil {
		return liveStrategyFailed("fact_lookup_failed", "读取当前事实依据失败。", nil)
	}
	var current *model.LiveAgentPlanFact
	for i := range items {
		if items[i].ID == p.FactID && items[i].Category == strings.TrimSpace(p.FactCategory) && items[i].Key == strings.TrimSpace(p.FactKey) {
			current = &items[i]
			break
		}
	}
	if current == nil || (p.CurrentVersionNo > 0 && current.VersionNo != p.CurrentVersionNo) {
		return liveStrategyFailed("stale_confirmation", "这条事实已经变化，请重新发起修改。", nil)
	}
	value := strings.TrimSpace(p.FactValue)
	if current.Value == value {
		return liveStrategyFailed("no_change", "新值和当前事实一致，没有生成新版本。", nil)
	}
	candidate := model.LiveAgentPlanFactCandidate{Category: current.Category, Key: current.Key, Value: value}
	if err := validateLiveAgentPlanFactCandidate(candidate); err != nil {
		return liveStrategyFailed("invalid_fact", err.Error(), nil)
	}
	updated, err := s.store.UpdateLiveAgentPlanFactWithExpectedVersion(r.Context(), tenantID, p.PlanID, current.ID, actor.UserID, current.VersionNo, current.Category, current.Key, value)
	if errors.Is(err, appdb.ErrLiveAgentPlanFactVersionConflict) {
		return liveStrategyFailed("stale_confirmation", "这条事实已经产生新版本，请重新发起修改。", nil)
	}
	if err != nil {
		return liveStrategyFailed("fact_update_failed", "修改事实依据失败，请刷新后重试。", nil)
	}
	if err := s.hotReloadLiveAgentPlanRooms(r.Context(), tenantID, p.PlanID, "facts"); err != nil {
		return liveStrategyFailed("runtime_sync_failed", "事实已修改，但热同步直播间失败。", map[string]any{"error": err.Error()})
	}
	return liveStrategySucceeded("fact_updated", "已确认修改事实“"+current.Key+"”，正式版本更新为 V"+strconv.FormatInt(updated.VersionNo, 10)+"。", map[string]any{"module": "knowledge", "plan_id": p.PlanID, "version_no": updated.VersionNo})
}

func (s *Server) executeLiveStrategyDisableFact(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if _, err := s.requireBoundLiveStrategyPlan(r, tenantID, p.RoomID, p.PlanID); err != nil {
		return liveStrategyFailed("plan_scope_changed", "当前方案已经不再绑定这个直播间。", nil)
	}
	items, err := s.store.ListLiveAgentPlanFacts(r.Context(), tenantID, p.PlanID)
	if err != nil {
		return liveStrategyFailed("fact_lookup_failed", "读取当前事实依据失败。", nil)
	}
	var current *model.LiveAgentPlanFact
	for i := range items {
		if items[i].ID == p.FactID {
			current = &items[i]
			break
		}
	}
	if current == nil || (p.CurrentVersionNo > 0 && current.VersionNo != p.CurrentVersionNo) {
		return liveStrategyFailed("stale_confirmation", "这条事实已经变化或被停用，请重新发起。", nil)
	}
	if err := s.store.DeleteLiveAgentPlanFactWithExpectedVersion(r.Context(), tenantID, p.PlanID, current.ID, actor.UserID, current.VersionNo); errors.Is(err, appdb.ErrLiveAgentPlanFactVersionConflict) {
		return liveStrategyFailed("stale_confirmation", "这条事实已经产生新版本，请重新发起停用。", nil)
	} else if err != nil {
		return liveStrategyFailed("fact_disable_failed", "停用事实依据失败，请稍后重试。", nil)
	}
	if err := s.hotReloadLiveAgentPlanRooms(r.Context(), tenantID, p.PlanID, "facts"); err != nil {
		return liveStrategyFailed("runtime_sync_failed", "事实已停用，但热同步直播间失败。", map[string]any{"error": err.Error()})
	}
	return liveStrategySucceeded("fact_disabled", "已确认停用事实“"+current.Key+"”。历史版本和审计记录仍保留。", map[string]any{"module": "knowledge", "plan_id": p.PlanID})
}

func (s *Server) executeLiveStrategyAddScriptReference(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if _, err := s.requireBoundLiveStrategyPlan(r, tenantID, p.RoomID, p.PlanID); err != nil {
		return liveStrategyFailed("plan_scope_changed", "当前方案已经不再绑定这个直播间。", nil)
	}
	input := model.CreateLiveAgentPlanScriptReferenceInput{
		ReferenceKey:  strings.TrimSpace(p.ScriptReferenceKey),
		Title:         strings.TrimSpace(p.ScriptTitle),
		ContentText:   strings.TrimSpace(p.ScriptText),
		Goal:          strings.TrimSpace(p.ScriptGoal),
		Transition:    strings.TrimSpace(p.ScriptTransition),
		ExecutionMode: strings.TrimSpace(p.ExecutionMode),
		SourceQuote:   strings.TrimSpace(p.SourceText),
		SourceType:    "system_agent",
		SourceRef:     "system-agent:intent-script",
	}
	if err := validateLiveAgentPlanScriptReferenceInput(input.ReferenceKey, input.Title, input.ContentText, input.Goal, input.Transition, input.ExecutionMode); err != nil {
		return liveStrategyFailed("invalid_script_reference", err.Error(), nil)
	}
	item, err := s.store.CreateLiveAgentPlanScriptReference(r.Context(), tenantID, p.PlanID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanScriptReferenceAlreadyExists) {
		return liveStrategyFailed("stale_confirmation", "确认期间同标识话术参考已经存在。为避免把新增悄悄变成修改，请重新发起。", nil)
	}
	if err != nil {
		return liveStrategyFailed("script_reference_add_failed", "新增正式话术参考失败，请稍后重试。", nil)
	}
	return liveStrategySucceeded("script_reference_added", "已确认添加话术参考“"+item.Title+"”，正式版本为 V"+strconv.FormatInt(item.VersionNo, 10)+"。", map[string]any{"module": "rhythm", "plan_id": p.PlanID, "version_no": item.VersionNo})
}

func (s *Server) executeLiveStrategyUpdateScriptReference(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if _, err := s.requireBoundLiveStrategyPlan(r, tenantID, p.RoomID, p.PlanID); err != nil {
		return liveStrategyFailed("plan_scope_changed", "当前方案已经不再绑定这个直播间。", nil)
	}
	items, err := s.store.ListLiveAgentPlanScriptReferences(r.Context(), tenantID, p.PlanID)
	if err != nil {
		return liveStrategyFailed("script_reference_lookup_failed", "读取当前话术参考失败。", nil)
	}
	var current *model.LiveAgentPlanScriptReference
	for i := range items {
		if items[i].ID == p.ScriptReferenceID && items[i].ReferenceKey == strings.TrimSpace(p.ScriptReferenceKey) {
			current = &items[i]
			break
		}
	}
	if current == nil || (p.CurrentVersionNo > 0 && current.VersionNo != p.CurrentVersionNo) {
		return liveStrategyFailed("stale_confirmation", "这条话术参考已经变化，请重新发起修改。", nil)
	}
	input := model.UpdateLiveAgentPlanScriptReferenceInput{
		ExpectedVersionNo: current.VersionNo,
		ReferenceKey:      current.ReferenceKey,
		Title:             strings.TrimSpace(p.ScriptTitle),
		ContentText:       strings.TrimSpace(p.ScriptText),
		Goal:              strings.TrimSpace(p.ScriptGoal),
		Transition:        strings.TrimSpace(p.ScriptTransition),
		ExecutionMode:     strings.TrimSpace(p.ExecutionMode),
	}
	if err := validateLiveAgentPlanScriptReferenceInput(input.ReferenceKey, input.Title, input.ContentText, input.Goal, input.Transition, input.ExecutionMode); err != nil {
		return liveStrategyFailed("invalid_script_reference", err.Error(), nil)
	}
	updated, err := s.store.UpdateLiveAgentPlanScriptReference(r.Context(), tenantID, p.PlanID, current.ID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanScriptReferenceVersionConflict) {
		return liveStrategyFailed("stale_confirmation", "这条话术参考已经产生新版本，请重新发起修改。", nil)
	}
	if errors.Is(err, appdb.ErrLiveAgentPlanScriptReferenceAlreadyExists) {
		return liveStrategyFailed("script_reference_conflict", "当前方案已经存在同标识话术参考。", nil)
	}
	if err != nil {
		return liveStrategyFailed("script_reference_update_failed", "修改正式话术参考失败，请刷新后重试。", nil)
	}
	if updated.VersionNo == current.VersionNo {
		return liveStrategyFailed("no_change", "新值和当前正式话术参考一致，没有生成新版本。", nil)
	}
	return liveStrategySucceeded("script_reference_updated", "已确认修改话术参考“"+updated.Title+"”，正式版本更新为 V"+strconv.FormatInt(updated.VersionNo, 10)+"。", map[string]any{"module": "rhythm", "plan_id": p.PlanID, "version_no": updated.VersionNo})
}

func (s *Server) executeLiveStrategyDisableScriptReference(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if _, err := s.requireBoundLiveStrategyPlan(r, tenantID, p.RoomID, p.PlanID); err != nil {
		return liveStrategyFailed("plan_scope_changed", "当前方案已经不再绑定这个直播间。", nil)
	}
	items, err := s.store.ListLiveAgentPlanScriptReferences(r.Context(), tenantID, p.PlanID)
	if err != nil {
		return liveStrategyFailed("script_reference_lookup_failed", "读取当前话术参考失败。", nil)
	}
	var current *model.LiveAgentPlanScriptReference
	for i := range items {
		if items[i].ID == p.ScriptReferenceID {
			current = &items[i]
			break
		}
	}
	if current == nil || (p.CurrentVersionNo > 0 && current.VersionNo != p.CurrentVersionNo) {
		return liveStrategyFailed("stale_confirmation", "这条话术参考已经变化或被停用，请重新发起。", nil)
	}
	err = s.store.DeleteLiveAgentPlanScriptReferenceWithExpectedVersion(r.Context(), tenantID, p.PlanID, current.ID, actor.UserID, current.VersionNo)
	if errors.Is(err, appdb.ErrLiveAgentPlanScriptReferenceVersionConflict) {
		return liveStrategyFailed("stale_confirmation", "这条话术参考已经产生新版本，请重新发起停用。", nil)
	}
	if err != nil {
		return liveStrategyFailed("script_reference_disable_failed", "停用正式话术参考失败，请稍后重试。", nil)
	}
	return liveStrategySucceeded("script_reference_disabled", "已确认停用话术参考“"+current.Title+"”。历史版本和审计记录仍保留。", map[string]any{"module": "rhythm", "plan_id": p.PlanID})
}

func (s *Server) executeLiveStrategyBindPlan(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if p.TargetPlanID <= 0 {
		return liveStrategyFailed("target_plan_required", "目标方案无效，请重新发起。", nil)
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, p.TargetPlanID); err != nil {
		return liveStrategyFailed("plan_not_found", "目标方案不存在或已经归档。", nil)
	}
	bound, err := s.store.IsRoomBoundToLiveAgentPlan(r.Context(), tenantID, p.RoomID, p.TargetPlanID)
	if err != nil {
		return liveStrategyFailed("plan_lookup_failed", "读取方案绑定状态失败。", nil)
	}
	if bound {
		return liveStrategyFailed("no_change", "这个方案已经绑定到当前直播间。", nil)
	}
	plan, err := s.store.BindRoomToLiveAgentPlan(r.Context(), tenantID, p.TargetPlanID, p.RoomID, actor.UserID)
	if err != nil {
		return liveStrategyFailed("plan_bind_failed", "绑定方案失败，请刷新后重试。", nil)
	}
	return liveStrategySucceeded("plan_bound", "已把“"+plan.Name+"”绑定到当前直播间。当前运行方案没有自动切换。", map[string]any{"module": "plan", "plan_id": p.CurrentPlanID, "target_plan_id": plan.ID})
}

func (s *Server) executeLiveStrategyUnbindPlan(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if p.TargetPlanID <= 0 {
		return liveStrategyFailed("target_plan_required", "目标方案无效，请重新发起。", nil)
	}
	bound, err := s.store.IsRoomBoundToLiveAgentPlan(r.Context(), tenantID, p.RoomID, p.TargetPlanID)
	if err != nil || !bound {
		return liveStrategyFailed("stale_confirmation", "这个方案已经不在当前直播间的绑定列表里。", nil)
	}
	if selected, selectedErr := s.store.GetLiveAgentPlanForRoom(r.Context(), tenantID, p.RoomID); selectedErr == nil && selected.ID == p.TargetPlanID {
		runtimeState, runtimeErr := s.getCoreAgentState(r.Context(), tenantID, p.RoomID)
		if runtimeErr != nil {
			return liveStrategyFailed("runtime_unavailable", "无法确认当前直播方案运行状态。", nil)
		}
		if runtimeState.State == "working" || runtimeState.State == "paused" {
			return liveStrategyFailed("plan_in_use", "这个方案当前正在直播间使用，请先热切换到其它已绑定方案，或停止AI后再解绑。", nil)
		}
		if runtimeState.PlanID == p.TargetPlanID {
			if _, err := s.setCoreAgentPlan(r.Context(), tenantID, p.RoomID, 0, ""); err != nil {
				return liveStrategyFailed("runtime_sync_failed", "清理直播间当前方案失败，没有执行解绑。", nil)
			}
		}
	}
	if err := s.store.UnbindRoomFromLiveAgentPlan(r.Context(), tenantID, p.TargetPlanID, p.RoomID); err != nil {
		return liveStrategyFailed("plan_unbind_failed", "解绑方案失败，请刷新后重试。", nil)
	}
	name := strings.TrimSpace(p.TargetPlanName)
	if name == "" {
		name = "目标方案"
	}
	return liveStrategySucceeded("plan_unbound", "已从当前直播间解绑“"+name+"”。方案本身没有删除。", map[string]any{"module": "plan", "plan_id": p.CurrentPlanID, "target_plan_id": p.TargetPlanID})
}

func (s *Server) executeLiveStrategySwitchPlan(r *http.Request, actor model.Actor, tenantID int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if p.TargetPlanID <= 0 {
		return liveStrategyFailed("target_plan_required", "目标方案无效，请重新发起。", nil)
	}
	bound, err := s.store.IsRoomBoundToLiveAgentPlan(r.Context(), tenantID, p.RoomID, p.TargetPlanID)
	if err != nil || !bound {
		return liveStrategyFailed("plan_not_bound", "目标方案已经不再绑定当前直播间，请先重新绑定。", nil)
	}
	plan, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, p.TargetPlanID)
	if err != nil || plan.Status != "active" {
		return liveStrategyFailed("plan_not_found", "目标方案不存在或不可用。", nil)
	}
	oldPlan, oldErr := s.store.GetLiveAgentPlanForRoom(r.Context(), tenantID, p.RoomID)
	if oldErr == nil && oldPlan.ID == plan.ID {
		return liveStrategyFailed("no_change", "当前直播间已经在使用“"+plan.Name+"”。", nil)
	}
	selected, err := s.store.SelectLiveAgentPlanForRoom(r.Context(), tenantID, plan.ID, p.RoomID, actor.UserID)
	if err != nil {
		return liveStrategyFailed("plan_switch_failed", "切换运行方案失败，请刷新后重试。", nil)
	}
	if _, err := s.setCoreAgentPlan(r.Context(), tenantID, p.RoomID, selected.ID, selected.Name); err != nil {
		if oldErr == nil && oldPlan.ID > 0 {
			_, _ = s.store.SelectLiveAgentPlanForRoom(r.Context(), tenantID, oldPlan.ID, p.RoomID, actor.UserID)
		}
		return liveStrategyFailed("runtime_sync_failed", "同步运行方案到 Core 失败，数据库选择已尝试回滚。", nil)
	}
	return liveStrategySucceeded("plan_switched", "已把当前直播间运行方案热切换到“"+selected.Name+"”。其它已绑定方案继续保留。", map[string]any{"module": "plan", "plan_id": selected.ID, "selected_plan_id": selected.ID})
}

func (s *Server) auditLiveStrategyAgentCommand(r *http.Request, actor model.Actor, action, result string, p liveStrategyExecutePayload, code string) {
	detail, _ := json.Marshal(map[string]any{
		"room_id": p.RoomID, "plan_id": p.PlanID, "target_plan_id": p.TargetPlanID,
		"product_link_id": p.ProductLinkID, "benefit_id": p.BenefitID, "fact_id": p.FactID,
		"code": code,
	})
	_, _ = s.store.BeginAdminAudit(r.Context(), model.AdminAuditLog{
		ActorUserID: actor.UserID, ActorUsername: actor.Username, ActorRole: actor.Role,
		ActorType: "user", Source: "live_strategy_agent", Action: "agent.command." + strings.ToLower(strings.TrimSpace(action)),
		ObjectType: "live_room", ObjectID: strconv.FormatInt(p.RoomID, 10), ObjectName: strings.TrimSpace(p.TargetPlanName),
		Reason: "直播策略智能体统一命令执行器", RequestID: strings.TrimSpace(r.Header.Get("X-Request-ID")),
		DetailJSON: string(detail), HTTPMethod: r.Method, Path: r.URL.Path, ClientIP: requestClientIP(r), Result: result,
	})
}
