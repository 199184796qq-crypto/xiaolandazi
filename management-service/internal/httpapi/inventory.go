package httpapi

import (
	"database/sql"
	"errors"
	appdb "livecompanion/management/internal/db"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

func (s *Server) inventoryListWarehouses(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.view"); !ok {
		return
	}
	items, err := s.store.ListWarehouses(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取仓库失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) inventoryListDeviceProducts(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.view"); !ok {
		return
	}
	items, err := s.store.ListInventoryDeviceProducts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设备名称失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) inventoryListAgents(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.view"); !ok {
		return
	}
	items, err := s.store.ListAgents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取代理列表失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) inventoryListDevices(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.view"); !ok {
		return
	}
	items, err := s.store.ListDevices(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设备档案失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) inventoryCreateDevice(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "inventory.manage")
	if !ok {
		return
	}

	var input model.CreateDeviceInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.SN = strings.TrimSpace(input.SN)
	input.SKUCode = strings.TrimSpace(input.SKUCode)
	input.BatchNo = strings.TrimSpace(input.BatchNo)
	input.QualityStatus = strings.TrimSpace(input.QualityStatus)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.HardwareMAC != "" {
		mac, err := model.NormalizeHardwareMAC(input.HardwareMAC)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		input.HardwareMAC = mac
	}

	if len(input.SN) < 3 || len(input.SN) > 128 {
		writeError(w, http.StatusBadRequest, "设备 SN 需为 3-128 个字符")
		return
	}
	if len(input.SKUCode) < 2 || len(input.SKUCode) > 96 {
		writeError(w, http.StatusBadRequest, "SKU 需为 2-96 个字符")
		return
	}
	if input.WarehouseID <= 0 {
		writeError(w, http.StatusBadRequest, "必须选择入库仓库")
		return
	}
	if input.QualityStatus == "" {
		input.QualityStatus = "qualified"
	}
	switch input.QualityStatus {
	case "qualified", "defective":
	default:
		writeError(w, http.StatusBadRequest, "质检状态不支持")
		return
	}

	item, err := s.store.CreateDevice(r.Context(), actor.UserID, input)
	if err != nil {
		switch {
		case isDuplicateDBError(err):
			writeError(w, http.StatusConflict, "设备 SN 或 MAC 已存在")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusBadRequest, "仓库不存在")
		default:
			writeError(w, http.StatusBadRequest, "设备入库失败："+inventorySafeError(err))
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) inventoryBatchInbound(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "inventory.manage")
	if !ok {
		return
	}

	var input model.BatchInboundInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.BatchNo = strings.TrimSpace(input.BatchNo)
	input.PurchaseNo = strings.TrimSpace(input.PurchaseNo)
	input.SupplierName = strings.TrimSpace(input.SupplierName)
	input.PaymentMethod = strings.TrimSpace(input.PaymentMethod)
	input.QualityStatus = strings.TrimSpace(input.QualityStatus)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ProductID <= 0 {
		writeError(w, http.StatusBadRequest, "必须选择设备名称")
		return
	}
	if input.WarehouseID <= 0 {
		writeError(w, http.StatusBadRequest, "必须选择入库仓库")
		return
	}
	if input.PurchaseAmountCents == 0 {
		writeError(w, http.StatusBadRequest, "采购入库必须填写采购总金额")
		return
	}
	if input.SupplierName == "" {
		writeError(w, http.StatusBadRequest, "采购入库必须填写供应商")
		return
	}
	if input.PaymentMethod == "" {
		input.PaymentMethod = "manual"
	}
	if len(input.SNs) == 0 || len(input.SNs) > 5000 {
		writeError(w, http.StatusBadRequest, "一次入库需录入 1-5000 个 SN")
		return
	}
	switch input.QualityStatus {
	case "", "qualified", "defective":
	default:
		writeError(w, http.StatusBadRequest, "质检状态不支持")
		return
	}

	item, err := s.store.CreateBatchInbound(r.Context(), actor.UserID, input)
	if err != nil {
		switch {
		case errors.Is(err, appdb.ErrInventoryBatchExists):
			writeError(w, http.StatusConflict, err.Error())
		case isDuplicateDBError(err):
			writeError(w, http.StatusConflict, "入库失败：存在重复 SN 或 MAC")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusBadRequest, "设备名称或仓库不存在")
		default:
			writeError(w, http.StatusBadRequest, "批量采购入库失败："+inventorySafeError(err))
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) inventoryTransitionDevice(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "inventory.manage")
	if !ok {
		return
	}
	deviceID, ok := inventoryPathID(w, r, "deviceID")
	if !ok {
		return
	}

	var input model.DeviceTransitionInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.ToStatus = strings.ToUpper(strings.TrimSpace(input.ToStatus))
	input.Reason = strings.TrimSpace(input.Reason)
	input.ReferenceNo = strings.TrimSpace(input.ReferenceNo)
	if input.ToStatus == "" {
		writeError(w, http.StatusBadRequest, "目标状态不能为空")
		return
	}
	if input.ToStatus == "SCRAPPED" {
		writeError(w, http.StatusBadRequest, "已报废必须通过报废处置登记处置金额，不能直接修改状态")
		return
	}
	if utf8.RuneCountInString(input.Reason) > 1024 {
		writeError(w, http.StatusBadRequest, "变更原因最多 1024 个字符")
		return
	}

	item, err := s.store.TransitionDevice(r.Context(), actor.UserID, deviceID, input)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "设备或仓库不存在")
		case strings.Contains(err.Error(), "invalid device transition"):
			writeError(w, http.StatusConflict, "当前设备状态不允许直接变更到目标状态")
		default:
			writeError(w, http.StatusBadRequest, "设备状态变更失败："+inventorySafeError(err))
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) inventoryDisposeScrapDevice(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "inventory.after_sales.manage")
	if !ok {
		return
	}
	deviceID, ok := inventoryPathID(w, r, "deviceID")
	if !ok {
		return
	}

	var input model.ScrapDisposalInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.BuyerName = strings.TrimSpace(input.BuyerName)
	input.PaymentMethod = strings.TrimSpace(input.PaymentMethod)
	input.Note = strings.TrimSpace(input.Note)
	if input.AmountCents > 0 && input.BuyerName == "" {
		writeError(w, http.StatusBadRequest, "有处置收入时必须填写回收方/购买方")
		return
	}
	if input.PaymentMethod == "" {
		if input.AmountCents > 0 {
			input.PaymentMethod = "manual"
		} else {
			input.PaymentMethod = "none"
		}
	}

	item, err := s.store.DisposeScrapDevice(r.Context(), actor.UserID, deviceID, input)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "设备或报废待处置库不存在")
		case strings.Contains(err.Error(), "not pending scrap disposal"):
			writeError(w, http.StatusConflict, "只有报废待处置设备才能执行最终处置")
		default:
			writeError(w, http.StatusBadRequest, "报废处置失败："+inventorySafeError(err))
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) inventoryDeviceLedger(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.view"); !ok {
		return
	}
	deviceID, ok := inventoryPathID(w, r, "deviceID")
	if !ok {
		return
	}

	items, err := s.store.ListDeviceLedger(r.Context(), deviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设备流水失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) inventoryAllLedger(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.view"); !ok {
		return
	}
	items, err := s.store.ListDeviceLedger(r.Context(), 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设备流水失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) inventorySummary(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.view"); !ok {
		return
	}
	items, err := s.store.GetInventorySummary(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取库存汇总失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) inventoryDocuments(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.view"); !ok {
		return
	}
	items, err := s.store.ListStockDocuments(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取库存单据失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) inventoryListRMAs(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.after_sales.view"); !ok {
		return
	}

	page := 1
	pageSize := 20
	if value := strings.TrimSpace(r.URL.Query().Get("page")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("page_size")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}
	if pageSize > 100 {
		pageSize = 100
	}

	items, total, openTotal, err := s.store.ListRMAs(r.Context(), page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取售后单失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"total":      total,
		"open_total": openTotal,
		"page":       page,
		"page_size":  pageSize,
	})
}

func (s *Server) inventoryCreateRMA(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "inventory.after_sales.manage")
	if !ok {
		return
	}

	var input model.CreateRMAInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.ServiceType = strings.ToLower(strings.TrimSpace(input.ServiceType))
	input.SourceType = "staff_manual"
	input.SourceUserID = &actor.UserID
	input.CustomerName = strings.TrimSpace(input.CustomerName)
	input.ContactPhone = strings.TrimSpace(input.ContactPhone)
	input.Issue = strings.TrimSpace(input.Issue)

	if input.DeviceID <= 0 {
		writeError(w, http.StatusBadRequest, "必须选择设备")
		return
	}
	switch input.ServiceType {
	case "return", "exchange", "repair", "refurbish", "scrap":
	default:
		writeError(w, http.StatusBadRequest, "售后类型不支持")
		return
	}
	if input.Issue == "" {
		writeError(w, http.StatusBadRequest, "售后问题说明不能为空")
		return
	}

	item, err := s.store.CreateRMA(r.Context(), actor.UserID, input)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "设备不存在")
		case strings.Contains(err.Error(), "cannot open RMA"):
			writeError(w, http.StatusConflict, "当前设备状态不能发起售后")
		case strings.Contains(err.Error(), "already has active RMA"):
			writeError(w, http.StatusConflict, "该设备已有未完成的售后维修单")
		default:
			writeError(w, http.StatusBadRequest, "创建售后单失败："+inventorySafeError(err))
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) inventoryAcceptRMA(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "inventory.after_sales.manage")
	if !ok {
		return
	}
	rmaID, ok := inventoryPathID(w, r, "rmaID")
	if !ok {
		return
	}
	item, err := s.store.AcceptRMA(r.Context(), actor.UserID, rmaID)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "售后维修单不存在")
		case strings.Contains(err.Error(), "not awaiting acceptance"):
			writeError(w, http.StatusConflict, "当前维修单已经受理或不在待受理状态")
		case strings.Contains(err.Error(), "cannot accept RMA"):
			writeError(w, http.StatusConflict, "当前设备状态不能受理该维修单")
		default:
			writeError(w, http.StatusBadRequest, "受理售后维修单失败："+inventorySafeError(err))
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) inventoryStartRMARepair(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "inventory.after_sales.manage")
	if !ok {
		return
	}
	rmaID, ok := inventoryPathID(w, r, "rmaID")
	if !ok {
		return
	}
	item, err := s.store.StartInternalRMARepair(r.Context(), actor.UserID, rmaID)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "售后维修单或维修库不存在")
		case strings.Contains(err.Error(), "not ready for internal repair"):
			writeError(w, http.StatusConflict, "当前维修单还不能转入内部维修")
		case strings.Contains(err.Error(), "not in after-sales warehouse"):
			writeError(w, http.StatusConflict, "设备尚未签收入售后库，不能开始维修")
		default:
			writeError(w, http.StatusBadRequest, "开始内部维修失败："+inventorySafeError(err))
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) inventoryRMAEvents(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.after_sales.view"); !ok {
		return
	}
	rmaID, ok := inventoryPathID(w, r, "rmaID")
	if !ok {
		return
	}
	items, err := s.store.ListRMAEvents(r.Context(), rmaID, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取维修进度失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) inventoryListRMACosts(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.after_sales.view"); !ok {
		return
	}
	rmaID, ok := inventoryPathID(w, r, "rmaID")
	if !ok {
		return
	}
	items, err := s.store.ListRMACosts(r.Context(), rmaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取维修费用失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) inventoryCreateRMACost(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "inventory.after_sales.manage")
	if !ok {
		return
	}
	rmaID, ok := inventoryPathID(w, r, "rmaID")
	if !ok {
		return
	}
	var input model.CreateRMACostInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.CostType = strings.ToLower(strings.TrimSpace(input.CostType))
	input.CounterpartyName = strings.TrimSpace(input.CounterpartyName)
	input.PaymentMethod = strings.TrimSpace(input.PaymentMethod)
	input.Note = strings.TrimSpace(input.Note)
	if input.AmountCents == 0 {
		writeError(w, http.StatusBadRequest, "维修费用金额必须大于 0")
		return
	}
	item, err := s.store.CreateRMACost(r.Context(), actor.UserID, rmaID, input)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "售后维修单不存在")
		case strings.Contains(err.Error(), "unsupported RMA cost type"):
			writeError(w, http.StatusBadRequest, "维修费用类型不支持")
		default:
			writeError(w, http.StatusBadRequest, "登记维修费用失败："+inventorySafeError(err))
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) inventoryCompleteRMA(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "inventory.after_sales.manage")
	if !ok {
		return
	}
	rmaID, ok := inventoryPathID(w, r, "rmaID")
	if !ok {
		return
	}

	var input model.CompleteRMAInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Resolution = strings.TrimSpace(input.Resolution)
	input.ToStatus = strings.ToUpper(strings.TrimSpace(input.ToStatus))
	if input.Resolution == "" {
		writeError(w, http.StatusBadRequest, "处理结果不能为空")
		return
	}

	item, err := s.store.CompleteRMA(r.Context(), actor.UserID, rmaID, input)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "售后单或设备不存在")
		case strings.Contains(err.Error(), "already closed"):
			writeError(w, http.StatusConflict, "售后单已经关闭")
		case strings.Contains(err.Error(), "invalid RMA result"):
			writeError(w, http.StatusBadRequest, "售后结果状态不支持")
		default:
			writeError(w, http.StatusBadRequest, "完成售后失败："+inventorySafeError(err))
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func inventoryPathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue(name)), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "ID 无效")
		return 0, false
	}
	return value, true
}

func inventorySafeError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "warehouse is not active"):
		return "仓库不可用"
	default:
		return "请检查输入和当前设备状态"
	}
}
