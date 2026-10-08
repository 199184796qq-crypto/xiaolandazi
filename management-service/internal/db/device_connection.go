package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

type HardwareConnectionInput struct {
	HardwareMAC  string    `json:"hardware_mac"`
	ConnectionID string    `json:"connection_id"`
	TenantID     int64     `json:"tenant_id"`
	DeviceID     int64     `json:"device_id"`
	Phase        string    `json:"phase"`
	ObservedAt   time.Time `json:"observed_at"`
}

type HardwareConnectionResult struct {
	TenantID    int64  `json:"tenant_id"`
	DeviceID    int64  `json:"device_id"`
	RoomID      int64  `json:"room_id"`
	BindingRole string `json:"binding_role"`
	Applied     bool   `json:"applied"`
}

func ValidateHardwareConnection(input HardwareConnectionInput, now time.Time) error {
	if _, err := model.NormalizeHardwareMAC(input.HardwareMAC); err != nil {
		return err
	}
	if input.DeviceID <= 0 || input.TenantID <= 0 || len(input.ConnectionID) < 8 || len(input.ConnectionID) > 96 {
		return errors.New("设备连接标识无效")
	}
	for _, c := range input.ConnectionID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return errors.New("设备连接标识无效")
		}
	}
	switch input.Phase {
	case "connect", "heartbeat", "disconnect", "shutdown":
	default:
		return errors.New("设备连接事件无效")
	}
	if input.ObservedAt.IsZero() || input.ObservedAt.After(now.Add(time.Second)) || input.ObservedAt.Before(now.Add(-30*time.Second)) {
		return errors.New("设备真实应答时间已过期")
	}
	return nil
}

// A connection lease lives in existing runtime metadata. Updates from an old
// socket cannot overwrite a replacement socket or stop its room.
func (s *Store) ObserveHardwareConnection(ctx context.Context, input HardwareConnectionInput) (HardwareConnectionResult, error) {
	var out HardwareConnectionResult
	now := time.Now().UTC()
	if err := ValidateHardwareConnection(input, now); err != nil {
		return out, err
	}
	mac, _ := model.NormalizeHardwareMAC(input.HardwareMAC)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var tenant int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM mgmt_tenants WHERE id=? FOR UPDATE`, input.TenantID).Scan(&tenant); err != nil {
		return out, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT p.device_id,p.claimed_tenant_id FROM device_hardware_profiles p JOIN inv_devices d ON d.id=p.device_id WHERE p.hardware_mac=? AND p.device_id=? AND p.claimed_tenant_id=? AND p.claimed_tenant_id=d.current_customer_id AND d.lifecycle_status IN ('CUSTOMER_BOUND','ACTIVE') FOR UPDATE`, mac, input.DeviceID, input.TenantID).Scan(&out.DeviceID, &out.TenantID); err != nil {
		return out, err
	}
	err = tx.QueryRowContext(ctx, `SELECT room_id,binding_role FROM live_device_room_bindings WHERE tenant_id=? AND device_id=? AND status='active' ORDER BY id DESC LIMIT 1 FOR UPDATE`, out.TenantID, out.DeviceID).Scan(&out.RoomID, &out.BindingRole)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	var metadataRaw sql.NullString
	var connectionStatus, workStatus string
	var heartbeat sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT metadata_json,connection_status,work_status,last_heartbeat_at FROM live_device_runtime_state WHERE tenant_id=? AND device_id=? FOR UPDATE`, out.TenantID, out.DeviceID).Scan(&metadataRaw, &connectionStatus, &workStatus, &heartbeat)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	metadata := map[string]any{}
	if metadataRaw.Valid {
		_ = json.Unmarshal([]byte(metadataRaw.String), &metadata)
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	if input.Phase != "connect" && strings.TrimSpace(fmt.Sprint(metadata["gateway_connection_id"])) != input.ConnectionID {
		return out, nil
	}
	if input.Phase == "heartbeat" && connectionStatus != "online" {
		return out, nil
	}
	if input.Phase == "disconnect" && metadata["last_transport_phase"] == "shutdown" {
		return out, nil
	}
	if heartbeat.Valid && heartbeat.Time.After(input.ObservedAt) && input.Phase != "connect" {
		input.ObservedAt = heartbeat.Time
	}
	metadata["gateway_connection_id"] = input.ConnectionID
	metadata["transport"] = "xiaozhi_websocket_proven"
	stopReason := ""
	if workStatus == "" {
		workStatus = "idle"
	}
	connectionStatus = "online"
	switch input.Phase {
	case "disconnect":
		connectionStatus = "offline"
		workStatus = "idle"
		stopReason = "device_offline"
	case "shutdown":
		connectionStatus = "offline"
		workStatus = "idle"
		stopReason = "device_shutdown"
	}
	metadata["last_transport_phase"] = input.Phase
	if input.Phase == "shutdown" {
		metadata["shutdown_at"] = now.Format(time.RFC3339Nano)
	}
	encoded, _ := json.Marshal(metadata)
	var room any
	if out.RoomID > 0 {
		room = out.RoomID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO live_device_runtime_state (device_id,tenant_id,current_room_id,connection_status,work_status,stop_reason,last_heartbeat_at,metadata_json,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE tenant_id=VALUES(tenant_id),current_room_id=VALUES(current_room_id),connection_status=VALUES(connection_status),work_status=VALUES(work_status),stop_reason=VALUES(stop_reason),last_heartbeat_at=VALUES(last_heartbeat_at),metadata_json=VALUES(metadata_json),updated_at=VALUES(updated_at)`, out.DeviceID, out.TenantID, room, connectionStatus, workStatus, stopReason, input.ObservedAt.UTC(), encoded, now, now)
	if err != nil {
		return out, err
	}
	if input.Phase == "connect" || input.Phase == "disconnect" || input.Phase == "shutdown" {
		_, err = tx.ExecContext(ctx, `INSERT INTO live_runtime_events (tenant_id,room_id,device_id,actor_type,event_code,title,detail_json,occurred_at) VALUES (?,?,?,'system',?,?,?,?)`, out.TenantID, room, out.DeviceID, "DEVICE_TRANSPORT_"+strings.ToUpper(input.Phase), "设备连接状态已更新", encoded, now)
		if err != nil {
			return out, err
		}
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	out.Applied = true
	return out, nil
}
