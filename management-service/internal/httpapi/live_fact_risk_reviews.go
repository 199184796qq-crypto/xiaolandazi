package httpapi

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"
)

// Used only by the explicit maintenance CLI. No model, auth bypass endpoint,
// original source write, or user confirmation is introduced.
func (s *Server) RebuildFactRiskReviews(ctx context.Context, tenant, plan int64) error {
	return s.refreshPlanFactRiskRegistry(ctx, tenant, plan)
}

// Explicit scoped maintenance uses the same configured model and audit path as
// the UI. It never performs a user confirmation or modifies original content.
func (s *Server) RecheckPendingFactRiskReviews(ctx context.Context, tenant, plan int64) (int, error) {
	// 语义复核已下线：重建风险登记即可，不再触发模型语境判断。
	if err := s.refreshPlanFactRiskRegistry(ctx, tenant, plan); err != nil {
		return 0, err
	}
	items, err := s.store.ListLiveRiskReviews(ctx, tenant, plan)
	if err != nil {
		return 0, err
	}
	pending := 0
	for _, item := range items {
		if item.Status != "confirmed" {
			pending++
		}
	}
	return pending, nil
}

// This rebuild is deterministic and makes no model request.
func (s *Server) refreshPlanFactRiskRegistry(ctx context.Context, tenant, plan int64) error {
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		return err
	}
	d, err := s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
	if err != nil {
		return err
	}
	if err = s.syncDerivedReviews(ctx, tenant, plan, d); err != nil {
		return err
	}
	return s.store.SaveLiveFactPackageSnapshot(ctx, tenant, plan, derivedHash(d.Facts), d.Facts)
}
func (s *Server) liveFactRiskReviews(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, r.Method != http.MethodGet)
	if !ok {
		return
	}
	if err := s.refreshPlanFactRiskRegistry(r.Context(), tenant, plan); err != nil {
		writeError(w, 503, "刷新事实风险登记失败："+err.Error())
		return
	}
	if r.Method == http.MethodPut {
		if actor.Role != "customer" || actor.TenantID == nil || *actor.TenantID != tenant {
			writeError(w, http.StatusForbidden, "风险项处理必须由当前方案所属用户本人操作")
			return
		}
		var input struct {
			ClaimSHA256 string `json:"claim_sha256"`
			Action      string `json:"action"`
			Evidence    string `json:"evidence"`
			Reason      string `json:"reason"`
			HasEvidence bool   `json:"has_evidence"`
			Confirmed   bool   `json:"confirmed"`
		}
		if readJSON(w, r, &input) != nil || len(input.ClaimSHA256) != 64 || utf8.RuneCountInString(input.Evidence) > 2000 || utf8.RuneCountInString(input.Reason) > 2000 {
			writeError(w, 400, "风险项处理参数无效")
			return
		}
		action := strings.TrimSpace(input.Action)
		if action == "" && (input.Confirmed || input.HasEvidence) {
			action = "confirm"
		}
		var err error
		switch action {
		case "confirm":
			err = s.store.ConfirmLiveRiskReview(r.Context(), tenant, plan, actor.UserID, input.ClaimSHA256, strings.TrimSpace(input.Evidence))
		case "appeal":
			err = s.store.SubmitLiveRiskAppeal(r.Context(), tenant, plan, actor.UserID, input.ClaimSHA256, strings.TrimSpace(input.Reason))
		default:
			writeError(w, 400, "请选择确认、修改或提交后台复核")
			return
		}
		if err != nil {
			writeError(w, 409, err.Error())
			return
		}
	}
	items, err := s.store.ListLiveRiskReviews(r.Context(), tenant, plan)
	if err != nil {
		writeError(w, 503, "读取风险登记失败")
		return
	}
	unresolved := 0
	for _, item := range items {
		if item.Status != "confirmed" {
			unresolved++
		}
	}
	writeJSON(w, 200, map[string]any{"items": items, "unresolved_count": unresolved})
}
