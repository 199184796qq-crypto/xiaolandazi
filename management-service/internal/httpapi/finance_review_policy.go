package httpapi

import "net/http"

func (s *Server) financeReviewPolicy(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireAnyStaffPermission(w, r, "finance.dashboard.view", "finance.recharge.approve", "finance.refund.approve", "finance.reward.approve", "finance.ai_time.approve"); !ok {
		return
	}
	policy, err := s.store.FinanceReviewPolicy(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, policy)
}
