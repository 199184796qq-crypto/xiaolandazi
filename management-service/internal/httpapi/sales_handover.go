package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

type salesPortfolioHandoverRequest struct {
	ExpectedCustomerCount *int64 `json:"expected_customer_count"`
	ExpectedLeadCount     *int64 `json:"expected_lead_count"`
	ToSalesStaffID        int64  `json:"to_sales_staff_id"`
	Reason                string `json:"reason"`
}

func (s *Server) adminSalesHandoverPreview(w http.ResponseWriter, r *http.Request) {
	actor, access, ok := s.requireStaffPermission(w, r, "sales.assignment.manage")
	if !ok {
		return
	}
	fromSalesStaffID, ok := parsePositivePathID(w, r.PathValue("salesStaffID"))
	if !ok {
		return
	}

	scope := staffBusinessScope(actor, access, "sales.assignment.manage")
	if err := s.store.CanManageSalesHandover(r.Context(), scope, fromSalesStaffID); err != nil {
		writeSalesBusinessError(w, err, "读取交接范围失败")
		return
	}
	item, err := s.store.SalesPortfolioHandoverPreview(r.Context(), fromSalesStaffID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "销售人员不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取销售交接数据失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminSalesHandover(w http.ResponseWriter, r *http.Request) {
	actor, access, ok := s.requireStaffPermission(w, r, "sales.assignment.manage")
	if !ok {
		return
	}
	fromSalesStaffID, ok := parsePositivePathID(w, r.PathValue("salesStaffID"))
	if !ok {
		return
	}

	var input salesPortfolioHandoverRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ToSalesStaffID <= 0 {
		writeError(w, http.StatusBadRequest, "请选择接手销售")
		return
	}
	if input.ToSalesStaffID == fromSalesStaffID {
		writeError(w, http.StatusBadRequest, "不能把客户交接给本人")
		return
	}
	if input.Reason == "" || len([]rune(input.Reason)) > 1000 {
		writeError(w, http.StatusBadRequest, "请填写 1-1000 个字符的交接原因")
		return
	}

	if input.ExpectedCustomerCount == nil || input.ExpectedLeadCount == nil || *input.ExpectedCustomerCount < 0 || *input.ExpectedLeadCount < 0 {
		writeError(w, http.StatusBadRequest, "请先预览并核实交接数量")
		return
	}
	item, err := s.store.TransferSalesPortfolio(
		r.Context(),
		staffBusinessScope(actor, access, "sales.assignment.manage"),
		fromSalesStaffID,
		input.ToSalesStaffID,
		actor.UserID,
		input.Reason,
		input.ExpectedCustomerCount, input.ExpectedLeadCount,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "交接对象不在管理范围，或接手销售已停用")
		case strings.Contains(err.Error(), "portfolio changed"):
			writeError(w, http.StatusConflict, "没有待交接客户或已被其他操作转交，请刷新")
		case strings.Contains(err.Error(), "invalid sales handover target"):
			writeError(w, http.StatusBadRequest, "销售交接对象无效")
		default:
			writeError(w, http.StatusInternalServerError, "销售交接失败")
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
