package httpapi

import (
	"net/http"
	"regexp"
	"strings"
	"time"
)

var salesPerformancePeriodPattern = regexp.MustCompile(`^\d{4}-\d{2}$`)

func (s *Server) adminSalesPerformance(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "sales.view_all"); !ok {
		return
	}

	period := strings.TrimSpace(r.URL.Query().Get("period"))
	if period == "" {
		period = time.Now().Format("2006-01")
	}
	if !salesPerformancePeriodPattern.MatchString(period) {
		writeError(w, http.StatusBadRequest, "业绩周期格式应为 YYYY-MM")
		return
	}
	periodStart, err := time.ParseInLocation("2006-01", period, time.Local)
	if err != nil {
		writeError(w, http.StatusBadRequest, "业绩周期无效")
		return
	}
	periodEnd := periodStart.AddDate(0, 1, 0)

	items, err := s.store.ListSalesPerformance(
		r.Context(),
		periodStart.UTC(),
		periodEnd.UTC(),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取销售业绩失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"period":       period,
		"period_start": periodStart,
		"period_end":   periodEnd,
		"items":        items,
	})
}
