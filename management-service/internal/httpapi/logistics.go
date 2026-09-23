package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"livecompanion/management/internal/model"
)

func (s *Server) logisticsListShipments(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "logistics.view"); !ok {
		return
	}
	items, err := s.store.ListShipments(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取物流单失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) logisticsCreateShipment(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "logistics.manage")
	if !ok {
		return
	}

	var input model.CreateShipmentInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	input.ShipmentType = strings.ToLower(strings.TrimSpace(input.ShipmentType))
	input.BusinessType = strings.ToLower(strings.TrimSpace(input.BusinessType))
	input.RecipientType = strings.ToLower(strings.TrimSpace(input.RecipientType))
	input.DeliveryMethod = strings.ToLower(strings.TrimSpace(input.DeliveryMethod))
	input.BusinessNo = strings.TrimSpace(input.BusinessNo)
	input.RecipientName = strings.TrimSpace(input.RecipientName)
	input.RecipientPhone = strings.TrimSpace(input.RecipientPhone)
	input.RecipientAddress = strings.TrimSpace(input.RecipientAddress)
	input.CarrierCode = strings.TrimSpace(input.CarrierCode)
	input.CarrierName = strings.TrimSpace(input.CarrierName)
	input.TrackingNo = strings.TrimSpace(input.TrackingNo)
	input.Note = strings.TrimSpace(input.Note)

	switch input.ShipmentType {
	case "outbound", "transfer", "return", "exchange", "resend", "rma_return", "repair_outbound", "repair_return":
	default:
		writeError(w, http.StatusBadRequest, "物流类型不支持")
		return
	}
	if input.BusinessType == "" {
		input.BusinessType = "manual"
	}
	if input.RecipientType == "" {
		input.RecipientType = "individual"
	}
	if input.DeliveryMethod == "" {
		input.DeliveryMethod = "courier"
	}
	switch input.RecipientType {
	case "individual", "agent", "customer":
	default:
		writeError(w, http.StatusBadRequest, "出库对象类型不支持")
		return
	}
	switch input.DeliveryMethod {
	case "courier", "pickup":
	default:
		writeError(w, http.StatusBadRequest, "交付方式仅支持快递或直接领取")
		return
	}
	switch input.BusinessType {
	case "manual", "order", "rma", "transfer":
	default:
		writeError(w, http.StatusBadRequest, "关联业务类型不支持")
		return
	}
	if len(input.DeviceIDs) == 0 {
		writeError(w, http.StatusBadRequest, "至少选择一个设备 SN")
		return
	}
	if input.FromWarehouseID == nil &&
		input.ShipmentType != "return" &&
		input.ShipmentType != "rma_return" {
		writeError(w, http.StatusBadRequest, "必须选择出库仓库")
		return
	}
	if input.ShipmentType == "outbound" ||
		input.ShipmentType == "exchange" ||
		input.ShipmentType == "resend" ||
		input.ShipmentType == "return" ||
		input.ShipmentType == "rma_return" ||
		input.ShipmentType == "repair_outbound" ||
		input.ShipmentType == "repair_return" {
		if (input.ShipmentType == "outbound" ||
			input.ShipmentType == "exchange" ||
			input.ShipmentType == "resend") &&
			input.RecipientType == "agent" && input.RecipientOrgID == nil {
			writeError(w, http.StatusBadRequest, "出库给代理时必须选择代理")
			return
		}
		if input.RecipientName == "" || input.RecipientPhone == "" {
			writeError(w, http.StatusBadRequest, "领取/收件人和联系电话不能为空")
			return
		}
		if input.DeliveryMethod == "courier" {
			if input.RecipientAddress == "" {
				writeError(w, http.StatusBadRequest, "快递发货必须填写收货地址")
				return
			}
			if input.LogisticsFeeCents == nil {
				writeError(w, http.StatusBadRequest, "快递发货必须填写物流费用，无费用请明确填 0")
				return
			}
			if input.CarrierName == "" || input.TrackingNo == "" {
				writeError(w, http.StatusBadRequest, "快递发货必须填写快递公司和运单号")
				return
			}
		}
	}

	item, err := s.store.CreateShipment(r.Context(), actor.UserID, input)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "设备或仓库不存在")
		case strings.Contains(err.Error(), "not in selected source warehouse"):
			writeError(w, http.StatusConflict, "所选设备不在指定出库仓库")
		case strings.Contains(err.Error(), "requires at least one device"):
			writeError(w, http.StatusBadRequest, "至少选择一个设备")
		default:
			writeError(w, http.StatusBadRequest, "创建物流单失败："+inventorySafeError(err))
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) logisticsUpdateShipmentStatus(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "logistics.manage")
	if !ok {
		return
	}
	shipmentID, ok := inventoryPathID(w, r, "shipmentID")
	if !ok {
		return
	}

	var input model.ShipmentStatusInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	input.Location = strings.TrimSpace(input.Location)
	input.Description = strings.TrimSpace(input.Description)

	switch input.Status {
	case "ready_to_ship", "shipped", "in_transit", "delivered", "exception", "returned", "cancelled":
	default:
		writeError(w, http.StatusBadRequest, "物流状态不支持")
		return
	}

	item, err := s.store.UpdateShipmentStatus(
		r.Context(),
		actor.UserID,
		shipmentID,
		input,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "物流单或设备不存在")
		case strings.Contains(err.Error(), "invalid shipment transition"):
			writeError(w, http.StatusConflict, "当前物流状态不能直接变更到目标状态")
		case strings.Contains(err.Error(), "invalid device transition"):
			writeError(w, http.StatusConflict, "物流关联设备当前状态不允许执行该动作")
		default:
			writeError(w, http.StatusBadRequest, "物流状态更新失败："+inventorySafeError(err))
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}
