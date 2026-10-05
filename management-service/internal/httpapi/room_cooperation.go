package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"livecompanion/management/internal/model"
)

func roomTenantID(room map[string]any) int64 {
	value, ok := room["tenant_id"]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case json.Number:
		id, _ := typed.Int64()
		return id
	case float64:
		return int64(typed)
	case string:
		id, _ := strconv.ParseInt(typed, 10, 64)
		return id
	default:
		return 0
	}
}

func roomID(room map[string]any) int64 {
	value, ok := room["id"]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case json.Number:
		id, _ := typed.Int64()
		return id
	case float64:
		return int64(typed)
	case string:
		id, _ := strconv.ParseInt(typed, 10, 64)
		return id
	default:
		return 0
	}
}

func applyRoomCooperation(
	room map[string]any,
	info model.CustomerCooperationInfo,
) {
	room["cooperation_status"] = info.Status
	room["cooperation_note"] = info.Note
	room["recharge_dormant_90_days"] = info.RechargeDormant90Days
	if info.MarkedAt != nil {
		room["cooperation_marked_at"] = info.MarkedAt
	}
	if info.MarkedByUserID > 0 {
		room["cooperation_marked_by_user_id"] = info.MarkedByUserID
	}
	if info.LastRechargeAt != nil {
		room["last_recharge_at"] = info.LastRechargeAt
	}
}

func (s *Server) enrichRoomMaps(
	ctx context.Context,
	rooms []map[string]any,
) error {
	tenantIDs := make([]int64, 0, len(rooms))
	for _, room := range rooms {
		if tenantID := roomTenantID(room); tenantID > 0 {
			tenantIDs = append(tenantIDs, tenantID)
		}
	}
	cooperation, err := s.store.GetCustomerCooperationByTenantIDs(ctx, tenantIDs)
	if err != nil {
		return err
	}
	primaryOnline, err := s.store.ListPrimaryRoomDeviceOnline(ctx, tenantIDs)
	if err != nil {
		return err
	}
	customers, err := s.store.GetRoomCustomerNames(ctx, tenantIDs)
	if err != nil {
		return err
	}
	for _, room := range rooms {
		tenantID := roomTenantID(room)
		room["customer_name"] = customers[tenantID]
		room["device_online"] = primaryOnline[tenantID][roomID(room)]
		if info, ok := cooperation[tenantID]; ok {
			applyRoomCooperation(room, info)
			continue
		}
		room["cooperation_status"] = model.CustomerCooperationStatusCooperating
		room["cooperation_note"] = ""
		room["recharge_dormant_90_days"] = false
	}
	return nil
}

func (s *Server) writeEnrichedRoomListResponse(
	w http.ResponseWriter,
	r *http.Request,
	resp *http.Response,
) {
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		copyResponse(w, resp)
		return
	}

	var payload struct {
		Items []map[string]any `json:"items"`
	}
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		writeError(w, http.StatusBadGateway, "直播间数据格式异常")
		return
	}
	roomIDs := make([]int64, 0, len(payload.Items))
	for _, item := range payload.Items {
		roomIDs = append(roomIDs, roomID(item))
	}
	deletions, err := s.store.RequestedRoomDeletionIDs(r.Context(), roomIDs)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取直播间生命周期失败")
		return
	}
	visible := make([]map[string]any, 0, len(payload.Items))
	for _, item := range payload.Items {
		if !deletions[roomID(item)] {
			visible = append(visible, item)
		}
	}
	payload.Items = visible
	if err := s.enrichRoomMaps(r.Context(), payload.Items); err != nil {
		writeError(w, http.StatusInternalServerError, "读取商户合作状态失败")
		return
	}
	writeJSON(w, resp.StatusCode, payload)
}

func (s *Server) writeEnrichedRoomResponse(
	w http.ResponseWriter,
	r *http.Request,
	resp *http.Response,
) {
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		copyResponse(w, resp)
		return
	}

	room := map[string]any{}
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&room); err != nil {
		writeError(w, http.StatusBadGateway, "直播间数据格式异常")
		return
	}
	if err := s.enrichRoomMaps(r.Context(), []map[string]any{room}); err != nil {
		writeError(w, http.StatusInternalServerError, "读取商户合作状态失败")
		return
	}
	writeJSON(w, resp.StatusCode, room)
}
