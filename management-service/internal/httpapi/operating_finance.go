package httpapi

import (
	"net/http"
	"strings"

	"livecompanion/management/internal/model"
)

func (s *Server) financeOperatingOverview(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "finance.operating.view"); !ok {
		return
	}
	item, err := s.store.ListOperatingFinance(r.Context(), 500)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取经营收支失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) financeCreateTokenPurchase(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.operating.manage")
	if !ok {
		return
	}
	var input model.TokenPurchaseInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.ProviderName = strings.TrimSpace(input.ProviderName)
	input.ModelScope = strings.TrimSpace(input.ModelScope)
	input.PaymentMethod = strings.TrimSpace(input.PaymentMethod)
	input.InvoiceNo = strings.TrimSpace(input.InvoiceNo)
	input.Note = strings.TrimSpace(input.Note)
	if input.ProviderName == "" {
		writeError(w, http.StatusBadRequest, "Token 供应商不能为空")
		return
	}
	if input.TokenQuantity == 0 {
		writeError(w, http.StatusBadRequest, "Token 数量必须大于 0")
		return
	}
	if input.AmountCents == 0 {
		writeError(w, http.StatusBadRequest, "Token 采购金额必须大于 0")
		return
	}
	if input.PaymentMethod == "" {
		input.PaymentMethod = "manual"
	}
	item, err := s.store.CreateTokenPurchase(r.Context(), actor.UserID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "登记 Token 采购失败："+inventorySafeError(err))
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
