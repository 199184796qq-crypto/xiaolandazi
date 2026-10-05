package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"livecompanion/management/internal/model"
)

func decorateLiveDevice(d *model.LiveDevice) {
	if _, err := model.NormalizeHardwareMAC(d.SN); err == nil {
		d.SN = fmt.Sprintf("XL%06d", d.ID)
	}
	if d.DeviceName == "" {
		d.DeviceName = model.DefaultDeviceName
	}
	if d.LastHeartbeatAt == nil || time.Since(*d.LastHeartbeatAt) > liveDeviceHeartbeatTimeout {
		d.ConnectionStatus = "offline"
	}
	switch {
	case d.ConnectionStatus == "offline":
		d.DisplayStatus = "离线"
	case d.ConnectionStatus == "error" || d.WorkStatus == "error":
		d.DisplayStatus = "设备异常"
	case d.ConnectionStatus == "connecting":
		d.DisplayStatus = "连接中"
	case d.WorkStatus == "working" || d.WorkStatus == "running":
		d.DisplayStatus = "正常工作"
	case d.WorkStatus == "paused":
		d.DisplayStatus = "已暂停"
	default:
		d.DisplayStatus = "连续待机"
	}
}

// The gateway can report presence without impersonating a customer login.
func (s *Store) HardwareHeartbeat(ctx context.Context, mac string, roomID int64) (model.LiveDevice, error) {
	mac, err := model.NormalizeHardwareMAC(mac)
	if err != nil {
		return model.LiveDevice{}, err
	}
	var id, tenant int64
	if err := s.db.QueryRowContext(ctx, `SELECT p.device_id,p.claimed_tenant_id FROM device_hardware_profiles p JOIN inv_devices d ON d.id=p.device_id WHERE p.hardware_mac=? AND p.claimed_tenant_id=d.current_customer_id AND d.lifecycle_status IN ('CUSTOMER_BOUND','ACTIVE')`, mac).Scan(&id, &tenant); err != nil {
		return model.LiveDevice{}, err
	}
	var room *int64
	if roomID > 0 {
		room = &roomID
	}
	item, err := s.HeartbeatLiveDevice(ctx, tenant, id, room, nil)
	if err != nil {
		return item, err
	}
	if roomID > 0 {
		_, err = s.db.ExecContext(ctx, `UPDATE inv_devices SET lifecycle_status='ACTIVE' WHERE id=? AND lifecycle_status='CUSTOMER_BOUND'`, id)
	}
	return item, err
}

func (s *Store) UnbindLiveDevice(ctx context.Context, tenantID, userID, deviceID int64) (model.LiveDevice, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveDevice{}, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM mgmt_tenants WHERE id=? FOR UPDATE`, tenantID).Scan(&id); err != nil {
		return model.LiveDevice{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT id FROM inv_devices WHERE id=? AND current_customer_id=? FOR UPDATE`, deviceID, tenantID).Scan(&id); err != nil {
		return model.LiveDevice{}, err
	}
	var bindingID, roomID int64
	var bindingRole string
	if err := tx.QueryRowContext(ctx, `
		SELECT id, room_id, binding_role
		FROM live_device_room_bindings
		WHERE device_id=? AND tenant_id=? AND status='active'
		ORDER BY id DESC LIMIT 1
		FOR UPDATE
	`, deviceID, tenantID).Scan(&bindingID, &roomID, &bindingRole); err != nil {
		return model.LiveDevice{}, err
	}
	var busy int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_runtime_sessions s WHERE s.tenant_id=? AND s.status='running' AND (s.device_id=? OR s.room_id IN (SELECT room_id FROM live_device_room_bindings WHERE device_id=? AND status='active'))`, tenantID, deviceID, deviceID).Scan(&busy); err != nil {
		return model.LiveDevice{}, err
	}
	if busy > 0 {
		return model.LiveDevice{}, ErrLiveBindingBusy
	}
	if _, err := tx.ExecContext(ctx, `UPDATE live_device_room_bindings SET status='inactive',unbound_at=UTC_TIMESTAMP(3),ended_by_user_id=? WHERE id=? AND tenant_id=? AND status='active'`, userID, bindingID, tenantID); err != nil {
		return model.LiveDevice{}, err
	}
	if bindingRole == "primary" {
		var promotedBindingID, promotedDeviceID int64
		err := tx.QueryRowContext(ctx, `
			SELECT id, device_id
			FROM live_device_room_bindings
			WHERE tenant_id=? AND room_id=? AND binding_role='listener' AND status='active'
			ORDER BY id DESC LIMIT 1
			FOR UPDATE
		`, tenantID, roomID).Scan(&promotedBindingID, &promotedDeviceID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return model.LiveDevice{}, err
		}
		if err == nil {
			if _, err := tx.ExecContext(ctx, `UPDATE live_device_room_bindings SET binding_role='primary',updated_at=UTC_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND status='active'`, promotedBindingID, tenantID); err != nil {
				return model.LiveDevice{}, err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO live_runtime_events (
					tenant_id, room_id, device_id, actor_type, actor_user_id,
					event_code, title, detail_json, occurred_at
				) VALUES (?, ?, ?, 'user', ?, 'DEVICE_ROLE_CHANGED', '监听设备已自动设为主设备', JSON_OBJECT('room_id', ?, 'binding_role', 'primary'), UTC_TIMESTAMP(3))
			`, tenantID, roomID, promotedDeviceID, userID, roomID); err != nil {
				return model.LiveDevice{}, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE live_device_runtime_state SET current_room_id=NULL,work_status='idle',stop_reason='unbound' WHERE device_id=? AND tenant_id=?`, deviceID, tenantID); err != nil {
		return model.LiveDevice{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_provisioning_events(device_id,tenant_id,actor_user_id,event_code) VALUES (?,?,?,'ROOM_UNBOUND')`, deviceID, tenantID, userID); err != nil {
		return model.LiveDevice{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveDevice{}, err
	}
	return s.GetLiveDevice(ctx, tenantID, deviceID)
}

func (s *Store) ConfigureDeviceHardware(ctx context.Context, userID, deviceID int64, rawMAC string, enabled bool, reason string) error {
	mac, err := model.NormalizeHardwareMAC(rawMAC)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	var lifecycle, quality string
	if err := tx.QueryRowContext(ctx, `SELECT id,lifecycle_status,quality_status FROM inv_devices WHERE id=? FOR UPDATE`, deviceID).Scan(&id, &lifecycle, &quality); err != nil {
		return err
	}
	if enabled && (quality != "qualified" || !(lifecycle == "IN_STOCK" || lifecycle == "SOLD" || lifecycle == "CUSTOMER_BOUND" || lifecycle == "ACTIVE")) {
		return errors.New("只有品质合格且在库或已交付的设备才能允许激活")
	}
	var previous string
	var claimed sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT hardware_mac,claimed_tenant_id FROM device_hardware_profiles WHERE device_id=? FOR UPDATE`, deviceID).Scan(&previous, &claimed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if previous != "" && previous != mac {
		return errors.New("已登记的 MAC 不允许修改，请走售后流程")
	}
	if previous == "" {
		if err := registerDeviceHardwareTx(ctx, tx, deviceID, mac); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE device_hardware_profiles SET claim_enabled=? WHERE device_id=?`, enabled, deviceID); err != nil {
		return err
	}
	if !enabled {
		if _, err := tx.ExecContext(ctx, `DELETE FROM device_claim_codes WHERE device_id=?`, deviceID); err != nil {
			return err
		}
	}
	event := "HARDWARE_REGISTERED"
	if enabled {
		event = "ACTIVATION_ALLOWED"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_provisioning_events(device_id,actor_user_id,event_code,detail) VALUES (?,?,?,?)`, deviceID, userID, event, reason); err != nil {
		return err
	}
	return s.commitInboxTx(ctx, tx, "inventory")
}

// Release ownership is a staff-only, audited after-sales operation. Inventory
// custody stays unchanged; this does not pretend the unit physically returned.
func (s *Store) ReleaseDeviceOwnership(ctx context.Context, userID, deviceID int64, reason string) error {
	var tenant int64
	if err := s.db.QueryRowContext(ctx, `SELECT claimed_tenant_id FROM device_hardware_profiles WHERE device_id=? AND claimed_tenant_id IS NOT NULL`, deviceID).Scan(&tenant); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM mgmt_tenants WHERE id=? FOR UPDATE`, tenant).Scan(&id); err != nil {
		return err
	}
	d, err := lockDeviceTx(ctx, tx, deviceID)
	if err != nil {
		return err
	}
	var claimed sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT claimed_tenant_id FROM device_hardware_profiles WHERE device_id=? FOR UPDATE`, deviceID).Scan(&claimed); err != nil {
		return err
	}
	if !claimed.Valid || claimed.Int64 != tenant || d.CurrentCustomerID == nil || *d.CurrentCustomerID != tenant || (d.LifecycleStatus != "SOLD" && d.LifecycleStatus != "CUSTOMER_BOUND" && d.LifecycleStatus != "ACTIVE") {
		return ErrDeviceClaimUnavailable
	}
	var busy int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_runtime_sessions WHERE tenant_id=? AND status='running' AND (device_id=? OR room_id IN (SELECT room_id FROM live_device_room_bindings WHERE device_id=? AND status='active'))`, tenant, deviceID, deviceID).Scan(&busy); err != nil {
		return err
	}
	if busy > 0 {
		return ErrLiveBindingBusy
	}
	if _, err := tx.ExecContext(ctx, `UPDATE live_device_room_bindings SET status='inactive',unbound_at=UTC_TIMESTAMP(3),ended_by_user_id=? WHERE device_id=? AND status='active'`, userID, deviceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM live_device_runtime_state WHERE device_id=?`, deviceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM device_claim_codes WHERE device_id=?`, deviceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE device_hardware_profiles SET claimed_tenant_id=NULL,device_name='',claimed_at=NULL,claim_enabled=FALSE WHERE device_id=?`, deviceID); err != nil {
		return err
	}
	docID, err := createStockDocumentTx(ctx, tx, "customer_release", nil, nil, d.OwnerOrgID, reason, userID)
	if err != nil {
		return err
	}
	if err := insertStockItemAndLedgerTx(ctx, tx, deviceID, docID, d.SN, d.SKUCode, "customer_release", d.LifecycleStatus, "SOLD", d.CustodyWarehouseID, d.CustodyWarehouseID, d.OwnerOrgID, d.OwnerOrgID, d.CurrentCustomerID, nil, userID, reason); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE inv_devices SET current_customer_id=NULL,lifecycle_status='SOLD',updated_by_user_id=? WHERE id=?`, userID, deviceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_provisioning_events(device_id,tenant_id,actor_user_id,event_code,detail) VALUES (?,?,?,'OWNERSHIP_RELEASED',?)`, deviceID, tenant, userID, reason); err != nil {
		return err
	}
	return s.commitInboxTx(ctx, tx, "inventory")
}
