package httpapi

import (
	"net/http"
	"strconv"
	"unicode/utf8"
)

func (s *Server) financeInvitations(w http.ResponseWriter, r *http.Request) {
	actor, access, ok := s.requireStaffPermission(w, r, "finance.dashboard.view")
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	page, size := normalizedPageQuery(r, 12, 100)
	q := r.URL.Query()
	if page > 1000000 || utf8.RuneCountInString(q.Get("search")) > 160 {
		writeError(w, 400, "查询参数超出范围")
		return
	}
	status := q.Get("status")
	if status != "" && status != "all" && status != "confirmed" && status != "unconfirmed" {
		writeError(w, 400, "客户认定筛选无效")
		return
	}
	items, total, err := s.store.ListFinanceInvitations(r.Context(), actor, access, q.Get("search"), status, page, size)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeBusinessPage(w, items, total, page, size)
}
func (s *Server) financeInvitationEarnings(w http.ResponseWriter, r *http.Request) {
	actor, access, ok := s.requireStaffPermission(w, r, "finance.dashboard.view")
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	id, err := strconv.ParseInt(r.PathValue("invitationID"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, 400, "邀请记录编号无效")
		return
	}
	page, size := normalizedPageQuery(r, 12, 100)
	if page > 1000000 {
		writeError(w, 400, "页码超出范围")
		return
	}
	items, total, err := s.store.FinanceInvitationEarnings(r.Context(), actor, access, id, page, size)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeBusinessPage(w, items, total, page, size)
}
