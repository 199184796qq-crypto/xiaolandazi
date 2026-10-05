package httpapi

import (
	storedb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
)

func (s *Server) financeListCommerceRules(w http.ResponseWriter, r *http.Request) {
	actor, access, ok := s.requireAnyStaffPermission(w, r, "finance.settlement_rules.view", "commercial.marketing.view")
	if !ok {
		return
	}
	items, err := s.store.ListCommerceRewardRules(r.Context())
	if err != nil {
		writeError(w, 500, "读取计提规则失败")
		return
	}
	// Marketing can inspect published participation, never edit finance drafts.
	if !actor.IsPlatformAdmin() && !staffHasPermission(access, "finance.settlement_rules.view") {
		out := []model.CommerceRewardRule{}
		for _, v := range items {
			if v.Status == "published" {
				out = append(out, v)
			}
		}
		items = out
	}
	targets, err := s.store.CommerceRuleTargets(r.Context())
	if err != nil {
		writeError(w, 500, "读取商品活动范围失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "targets": targets})
}
func (s *Server) financeSaveCommerceDraft(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.settlement_rules.manage")
	if !ok {
		return
	}
	var input model.CommerceRewardRule
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := storedb.ValidateCommerceRewardRule(input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	id, err := s.store.SaveCommerceRewardDraft(r.Context(), actor.UserID, input)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}
func (s *Server) financePublishCommerceRule(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.settlement_rules.manage")
	if !ok {
		return
	}
	id, ok := incentivePathID(w, r, "ruleID")
	if !ok {
		return
	}
	if err := s.store.PublishCommerceRewardRule(r.Context(), actor.UserID, id); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"published": true})
}
func (s *Server) customerClaimFreeMarketingOrder(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, 403, "仅客户可领取")
		return
	}
	id, ok := incentivePathID(w, r, "orderID")
	if !ok {
		return
	}
	item, err := s.store.ClaimFreeMarketingOrder(r.Context(), *actor.TenantID, actor.UserID, id)
	if err != nil {
		writeError(w, 409, "此订单不符合免费领取条件")
		return
	}
	writeJSON(w, 200, map[string]any{"order": item})
}
