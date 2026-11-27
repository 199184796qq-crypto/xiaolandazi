package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	storedb "livecompanion/management/internal/db"
)

func (s *Server) workspaceTimeCards(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "commercial.time_card.view"); !ok {
		return
	}
	start, end, ok := workspaceTimeCardPeriod(time.Now(), r.URL.Query().Get("period"))
	if !ok {
		writeError(w, http.StatusBadRequest, "销售统计时段无效")
		return
	}
	items, err := s.store.ListWorkspaceTimeCards(r.Context(), start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取时长卡失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

var salesCalendar = time.FixedZone("Asia/Shanghai", 8*3600)

func workspaceTimeCardPeriod(now time.Time, period string) (start, end time.Time, ok bool) {
	year, month, _ := now.In(salesCalendar).Date()
	var months int
	switch period {
	case "", "all":
		return time.Time{}, time.Time{}, true
	case "month":
		months = 1
	case "quarter":
		month = ((month-1)/3)*3 + 1
		months = 3
	case "year":
		month, months = time.January, 12
	default:
		return time.Time{}, time.Time{}, false
	}
	start = time.Date(year, month, 1, 0, 0, 0, 0, salesCalendar)
	return start.UTC(), start.AddDate(0, months, 0).UTC(), true
}

func (s *Server) workspaceSaveTimeCard(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.time_card.manage")
	if !ok {
		return
	}
	var productID int64
	if r.Method == http.MethodPut {
		productID, ok = timeCardProductID(w, r)
		if !ok {
			return
		}
	}
	input, ok := readTimeCardInput(w, r, true)
	if !ok {
		return
	}
	if err := s.store.SaveWorkspaceTimeCard(r.Context(), actor.UserID, productID, input); err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "时长卡不存在")
		case errors.Is(err, storedb.ErrTimeCardArchived):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "保存时长卡失败")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) workspaceSortTimeCards(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.time_card.manage")
	if !ok {
		return
	}
	var input struct {
		IDs      []int64 `json:"ids"`
		Previous []int64 `json:"previous"`
	}
	if err := readJSON(w, r, &input); err != nil || len(input.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "时长卡排序格式有误")
		return
	}
	if err := s.store.ReorderWorkspaceTimeCards(r.Context(), actor.UserID, input.IDs, input.Previous); err != nil {
		if errors.Is(err, storedb.ErrTimeCardCatalogChanged) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "保存排序失败")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) workspaceTimeCardSales(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "commercial.time_card.view"); !ok {
		return
	}
	productID, ok := timeCardProductID(w, r)
	if !ok {
		return
	}
	params := r.URL.Query()
	filter := storedb.TimeCardSalesQuery{
		Username: strings.TrimSpace(params.Get("username")), Reference: strings.TrimSpace(params.Get("reference")),
		Membership: strings.TrimSpace(params.Get("membership")), Status: params.Get("status"), Page: 1, PageSize: 50,
	}
	if value := params.Get("page"); value != "" {
		page, err := strconv.Atoi(value)
		if err != nil || page < 1 || page > 1000000 {
			writeError(w, http.StatusBadRequest, "页码无效")
			return
		}
		filter.Page = page
	}
	if params.Get("size") == "100" {
		filter.PageSize = 100
	}
	switch filter.Status {
	case "", "refunded", "expired", "active", "unactivated", "exhausted":
	default:
		writeError(w, http.StatusBadRequest, "筛选状态无效")
		return
	}

	for _, boundary := range []struct {
		key    string
		target **time.Time
	}{{"start", &filter.Start}, {"end", &filter.End}} {
		if raw := params.Get(boundary.key); raw != "" {
			date, err := time.ParseInLocation("2006-01-02", raw, salesCalendar)
			if err != nil {
				writeError(w, http.StatusBadRequest, "日期格式无效")
				return
			}
			if boundary.key == "end" {
				date = date.AddDate(0, 0, 1)
			}
			utc := date.UTC()
			*boundary.target = &utc
		}
	}
	if filter.Start != nil && filter.End != nil && !filter.Start.Before(*filter.End) {
		writeError(w, http.StatusBadRequest, "开始日期不能晚于结束日期")
		return
	}
	sales, err := s.store.ListTimeCardSales(r.Context(), productID, filter)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "时长卡不存在")
		} else {
			writeError(w, http.StatusInternalServerError, "读取销售明细失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, sales)
}

func (s *Server) workspaceTimeCardRefund(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.time_card.manage")
	if !ok {
		return
	}
	productID, ok := timeCardProductID(w, r)
	if !ok {
		return
	}
	assetID, ok := staffPathID(w, r, "assetID")
	if !ok {
		return
	}
	var input struct {
		Amount     uint64 `json:"amount_cents"`
		Reason     string `json:"reason"`
		Supervisor string `json:"supervisor"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "退款申请格式错误")
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	input.Supervisor = strings.TrimSpace(input.Supervisor)
	if input.Amount == 0 || input.Reason == "" || input.Supervisor == "" ||
		utf8.RuneCountInString(input.Reason) > 240 || utf8.RuneCountInString(input.Supervisor) > 64 {
		writeError(w, http.StatusBadRequest, "请填写退款金额、退款说明（最多 240 字）和上级主管名称（最多 64 字）")
		return
	}
	if err := s.store.RequestTimeCardExpiryRefund(r.Context(), productID, assetID, actor.UserID, input.Amount, input.Reason, input.Supervisor); err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "销售记录不存在")
		case errors.Is(err, storedb.ErrTimeCardRefundUnavailable), errors.Is(err, storedb.ErrTimeCardRefundAmount):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "提交退款申请失败")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
