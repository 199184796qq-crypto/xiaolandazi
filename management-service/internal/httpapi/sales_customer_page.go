package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

func (s *Server) salesCustomerPage(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsSalesStaff() {
		writeError(w, http.StatusForbidden, "仅销售人员可查看本人负责客户")
		return
	}
	q := r.URL.Query()
	search, status, source, sort := strings.TrimSpace(q.Get("search")), strings.TrimSpace(q.Get("status")), strings.TrimSpace(q.Get("source")), strings.TrimSpace(q.Get("sort"))
	if utf8.RuneCountInString(search) > 160 || len(source) > 32 || len(status) > 32 {
		writeError(w, http.StatusBadRequest, "筛选内容过长")
		return
	}
	page, size := normalizedPageQuery(r, 12, 100)
	if page > 1000000 {
		writeError(w, http.StatusBadRequest, "页码超出支持范围")
		return
	}
	items, total, summary, err := s.store.ListSalesCustomersPageByUser(r.Context(), actor.UserID, search, status, source, sort, page, size, q.Get("qualification"), q.Get("collection"))
	if errors.Is(err, sql.ErrNoRows) {
		items = make([]model.SalesCustomerListItem, 0)
		total = 0
		summary = model.SalesCustomerListSummary{}
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "读取我的客户失败，请稍后重试")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "page_size": size, "total_pages": totalPages(total, size), "summary": summary})
}
