package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

//go:embed device_business_schema.sql
var deviceBusinessSchema string

var (
	ErrDeviceBusinessConflict   = errors.New("请求编号已使用或业务状态已结束")
	ErrListenerCommandForbidden = errors.New("监听设备仅支持本机音量和字体设置")
)

func (s *Store) MigrateDeviceBusiness(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, deviceBusinessSchema)
	if err != nil {
		return err
	}
	return s.MigrateDeviceAddressing(ctx)
}

func deviceBusinessIdentityTx(ctx context.Context, tx *sql.Tx, mac string) (model.DeviceBusinessIdentity, error) {
	var out model.DeviceBusinessIdentity
	var err error
	mac, err = model.NormalizeHardwareMAC(mac)
	if err != nil {
		return out, err
	}
	var deviceID int64
	if err := tx.QueryRowContext(ctx, `SELECT device_id FROM device_hardware_profiles WHERE hardware_mac=?`, mac).Scan(&deviceID); err != nil {
		return out, err
	}
	var tenant sql.NullInt64
	var quality, lifecycle string
	if err := tx.QueryRowContext(ctx, `SELECT current_customer_id,quality_status,lifecycle_status FROM inv_devices WHERE id=? FOR UPDATE`, deviceID).Scan(&tenant, &quality, &lifecycle); err != nil {
		return out, err
	}
	var claimed sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT claimed_tenant_id FROM device_hardware_profiles WHERE device_id=? AND hardware_mac=? FOR UPDATE`, deviceID, mac).Scan(&claimed); err != nil {
		return out, err
	}
	if !tenant.Valid || !claimed.Valid || tenant.Int64 != claimed.Int64 || tenant.Int64 <= 0 || quality != "qualified" || (lifecycle != "CUSTOMER_BOUND" && lifecycle != "ACTIVE" && lifecycle != "SOLD") {
		return out, ErrDeviceClaimUnavailable
	}
	out.DeviceID, out.TenantID = deviceID, tenant.Int64
	var room int64
	err = tx.QueryRowContext(ctx, `SELECT b.room_id,b.binding_role FROM live_device_room_bindings b JOIN core_rooms r ON r.id=b.room_id AND r.tenant_id=b.tenant_id WHERE b.device_id=? AND b.tenant_id=? AND b.status='active' ORDER BY b.id DESC LIMIT 1`, deviceID, tenant.Int64).Scan(&room, &out.BindingRole)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if err == nil {
		out.RoomID = &room
	}
	return out, nil
}

func (s *Store) ResolveDeviceBusinessIdentity(ctx context.Context, rawMAC string) (model.DeviceBusinessIdentity, error) {
	mac, err := model.NormalizeHardwareMAC(rawMAC)
	if err != nil {
		return model.DeviceBusinessIdentity{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.DeviceBusinessIdentity{}, err
	}
	defer tx.Rollback()
	out, err := deviceBusinessIdentityTx(ctx, tx, mac)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

const deviceBusinessColumns = `id,device_id,tenant_id,room_id,request_id,business_type,payload_hash,status,action,operation,transcript,question,CAST(value_json AS CHAR),message,result_text,asset_id,billing_mode,charged_beans,expires_at,created_at,updated_at`

func scanDeviceBusinessEvent(row interface{ Scan(...any) error }) (model.DeviceBusinessEvent, error) {
	var out model.DeviceBusinessEvent
	var room, asset sql.NullInt64
	var value sql.NullString
	err := row.Scan(&out.ID, &out.DeviceID, &out.TenantID, &room, &out.RequestID, &out.BusinessType, &out.PayloadHash, &out.Status, &out.Action, &out.Operation, &out.Text, &out.Question, &value, &out.Message, &out.ResultText, &asset, &out.BillingMode, &out.ChargedBeans, &out.ExpiresAt, &out.CreatedAt, &out.UpdatedAt)
	if room.Valid {
		out.RoomID = &room.Int64
	}
	if asset.Valid {
		out.AssetID = &asset.Int64
	}
	if value.Valid {
		out.Value = json.RawMessage(value.String)
	}
	return out, err
}

// Begin locks current inventory ownership, so neither a stale gateway tenant nor
// a retry can submit a new billable business operation for another customer.
func (s *Store) BeginDeviceBusiness(ctx context.Context, rawMAC, requestID, kind, payloadHash, question string) (model.DeviceBusinessEvent, bool, error) {
	mac, err := model.NormalizeHardwareMAC(rawMAC)
	if err != nil {
		return model.DeviceBusinessEvent{}, false, err
	}
	if kind != "voice_control" && kind != "environment_snapshot" {
		return model.DeviceBusinessEvent{}, false, ErrDeviceBusinessConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.DeviceBusinessEvent{}, false, err
	}
	defer tx.Rollback()
	identity, err := deviceBusinessIdentityTx(ctx, tx, mac)
	if err != nil {
		return model.DeviceBusinessEvent{}, false, err
	}
	out, err := scanDeviceBusinessEvent(tx.QueryRowContext(ctx, `SELECT `+deviceBusinessColumns+` FROM device_business_events WHERE device_id=? AND tenant_id=? AND request_id=? AND business_type=? FOR UPDATE`, identity.DeviceID, identity.TenantID, requestID, kind))
	if err == nil {
		if out.PayloadHash != payloadHash {
			return out, false, ErrDeviceBusinessConflict
		}
		if (out.Status == "processing" || out.Status == "recognized" || out.Status == "executing") && out.ExpiresAt.Before(time.Now().UTC()) {
			if _, err := tx.ExecContext(ctx, `UPDATE device_business_events SET status='timeout',message='设备业务请求超时' WHERE id=?`, out.ID); err != nil {
				return out, false, err
			}
			out.Status = "timeout"
			out.Message = "设备业务请求超时"
		}
		return out, false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, false, err
	}
	// Bound the ASR lane; expired work never blocks new commands permanently.
	if _, err := tx.ExecContext(ctx, `UPDATE device_business_events SET status='timeout',message='设备业务请求超时' WHERE device_id=? AND tenant_id=? AND status IN ('processing','recognized','executing') AND expires_at<UTC_TIMESTAMP(3)`, identity.DeviceID, identity.TenantID); err != nil {
		return out, false, err
	}
	if kind == "voice_control" {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_business_events WHERE device_id=? AND tenant_id=? AND business_type='voice_control' AND status='processing'`, identity.DeviceID, identity.TenantID).Scan(&n); err != nil {
			return out, false, err
		}
		if n > 0 {
			return out, false, ErrDeviceBusinessConflict
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO device_business_events(device_id,tenant_id,room_id,request_id,business_type,payload_hash,transcript,question,result_text,expires_at) VALUES(?,?,?,?,?,?,'',?,'',DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 120 SECOND))`, identity.DeviceID, identity.TenantID, identity.RoomID, requestID, kind, payloadHash, question)
	if err != nil {
		return out, false, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return out, false, err
	}
	out, err = scanDeviceBusinessEvent(tx.QueryRowContext(ctx, `SELECT `+deviceBusinessColumns+` FROM device_business_events WHERE id=?`, id))
	if err != nil {
		return out, false, err
	}
	return out, true, tx.Commit()
}

func (s *Store) FinishDeviceVoice(ctx context.Context, rawMAC string, event model.DeviceBusinessEvent, text, status, message string) (model.DeviceBusinessEvent, error) {
	return s.finishDeviceVoice(ctx, rawMAC, event, text, status, message, nil, false)
}

func (s *Store) FinishDeviceVoiceAddressing(ctx context.Context, rawMAC string, event model.DeviceBusinessEvent, text string, addressing model.DeviceVoiceAddressing, wakeOnly bool) (model.DeviceBusinessEvent, error) {
	if !model.ValidSpeakerAddressing(addressing.Addressing) {
		addressing = model.DeviceVoiceAddressing{Addressing: "neutral", Source: "neutral"}
	}
	return s.finishDeviceVoice(ctx, rawMAC, event, text, "recognized", "", &addressing, wakeOnly)
}

func (s *Store) finishDeviceVoice(ctx context.Context, rawMAC string, event model.DeviceBusinessEvent, text, status, message string, addressing *model.DeviceVoiceAddressing, wakeOnly bool) (model.DeviceBusinessEvent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return event, err
	}
	defer tx.Rollback()
	identity, err := deviceBusinessIdentityTx(ctx, tx, rawMAC)
	if err != nil {
		return event, err
	}
	if identity.DeviceID != event.DeviceID || identity.TenantID != event.TenantID {
		return event, ErrDeviceClaimUnavailable
	}
	current, err := scanDeviceBusinessEvent(tx.QueryRowContext(ctx, `SELECT `+deviceBusinessColumns+` FROM device_business_events WHERE id=? AND device_id=? AND tenant_id=? FOR UPDATE`, event.ID, identity.DeviceID, identity.TenantID))
	if err != nil {
		return event, err
	}
	if current.Status != "processing" {
		return current, ErrDeviceBusinessConflict
	}
	if current.ExpiresAt.Before(time.Now().UTC()) {
		status, message = "timeout", "设备业务请求超时"
	}
	if status != "recognized" && status != "failed" && status != "timeout" {
		return current, ErrDeviceBusinessConflict
	}
	action := ""
	if wakeOnly && status == "recognized" {
		status, action = "succeeded", "wake_greeting"
	}
	_, err = tx.ExecContext(ctx, `UPDATE device_business_events SET transcript=?,status=?,message=?,action=? WHERE id=?`, text, status, message, action, current.ID)
	if err != nil {
		return current, err
	}
	if addressing != nil {
		if _, err := tx.ExecContext(ctx, `INSERT INTO device_voice_addressing(event_id,device_id,tenant_id,addressing,source) VALUES(?,?,?,?,?)`, current.ID, current.DeviceID, current.TenantID, addressing.Addressing, addressing.Source); err != nil {
			return current, err
		}
	}
	current.Text, current.Status, current.Message, current.Action = text, status, message, action
	return current, tx.Commit()
}

// Both the private asset row and queued snapshot event are committed together.
func (s *Store) QueueDeviceSnapshot(ctx context.Context, rawMAC string, event model.DeviceBusinessEvent, input model.CreateMediaAssetInput) (model.DeviceBusinessEvent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return event, err
	}
	defer tx.Rollback()
	identity, err := deviceBusinessIdentityTx(ctx, tx, rawMAC)
	if err != nil {
		return event, err
	}
	if identity.DeviceID != event.DeviceID || identity.TenantID != event.TenantID || input.TenantID != identity.TenantID {
		return event, ErrDeviceClaimUnavailable
	}
	current, err := scanDeviceBusinessEvent(tx.QueryRowContext(ctx, `SELECT `+deviceBusinessColumns+` FROM device_business_events WHERE id=? AND tenant_id=? FOR UPDATE`, event.ID, identity.TenantID))
	if err != nil {
		return event, err
	}
	if current.Status != "processing" || current.BusinessType != "environment_snapshot" || current.ExpiresAt.Before(time.Now().UTC()) {
		return current, ErrDeviceBusinessConflict
	}
	metadata, err := json.Marshal(input.Metadata)
	if err != nil {
		return current, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO media_assets(tenant_id,asset_type,original_name,storage_driver,storage_bucket,object_key,mime_type,size_bytes,checksum_sha256,status,metadata_json,created_by_user_id) VALUES(?,'image',?,?,?,?,?,?,?,'active',CAST(? AS JSON),NULL)`, identity.TenantID, input.OriginalName, input.StorageDriver, input.StorageBucket, input.ObjectKey, input.MIMEType, input.SizeBytes, input.ChecksumSHA256, string(metadata))
	if err != nil {
		return current, err
	}
	assetID, err := res.LastInsertId()
	if err != nil {
		return current, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE device_business_events SET asset_id=?,status='queued',message='照片已接收，等待业务处理' WHERE id=?`, assetID, current.ID); err != nil {
		return current, err
	}
	current.AssetID = &assetID
	current.Status = "queued"
	current.Message = "照片已接收，等待业务处理"
	return current, tx.Commit()
}

// Persist the dispatch decision before any hardware write. A lost reply or
// gateway restart deliberately cannot retry a relative command ambiguously.
func (s *Store) ClaimDeviceCommand(ctx context.Context, input model.DeviceCommandDispatchInput) (model.DeviceBusinessEvent, error) {
	mac, err := model.NormalizeHardwareMAC(input.HardwareMAC)
	if err != nil {
		return model.DeviceBusinessEvent{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.DeviceBusinessEvent{}, err
	}
	defer tx.Rollback()
	identity, err := deviceBusinessIdentityTx(ctx, tx, mac)
	if err != nil {
		return model.DeviceBusinessEvent{}, err
	}
	if identity.BindingRole == "listener" && input.Action != "volume" && input.Action != "font_size" {
		return model.DeviceBusinessEvent{}, ErrListenerCommandForbidden
	}
	out, err := scanDeviceBusinessEvent(tx.QueryRowContext(ctx, `SELECT `+deviceBusinessColumns+` FROM device_business_events WHERE device_id=? AND tenant_id=? AND request_id=? AND business_type='voice_control' FOR UPDATE`, identity.DeviceID, identity.TenantID, input.RequestID))
	if err != nil {
		return out, err
	}
	if out.Status != "recognized" {
		return out, ErrDeviceBusinessConflict
	}
	if out.ExpiresAt.Before(time.Now().UTC()) {
		if _, err := tx.ExecContext(ctx, `UPDATE device_business_events SET status='timeout',message='指令下发已超时' WHERE id=?`, out.ID); err != nil {
			return out, err
		}
		if err := tx.Commit(); err != nil {
			return out, err
		}
		out.Status = "timeout"
		return out, ErrDeviceBusinessConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE device_business_events SET status='executing',action=?,operation=? WHERE id=?`, input.Action, input.Operation, out.ID)
	if err != nil {
		return out, err
	}
	out.Status, out.Action, out.Operation = "executing", input.Action, input.Operation
	return out, tx.Commit()
}

func (s *Store) RecordDeviceCommand(ctx context.Context, input model.DeviceCommandEventInput) (model.DeviceBusinessEvent, error) {
	mac, err := model.NormalizeHardwareMAC(input.HardwareMAC)
	if err != nil {
		return model.DeviceBusinessEvent{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.DeviceBusinessEvent{}, err
	}
	defer tx.Rollback()
	identity, err := deviceBusinessIdentityTx(ctx, tx, mac)
	if err != nil {
		return model.DeviceBusinessEvent{}, err
	}
	out, err := scanDeviceBusinessEvent(tx.QueryRowContext(ctx, `SELECT `+deviceBusinessColumns+` FROM device_business_events WHERE device_id=? AND tenant_id=? AND request_id=? AND business_type='voice_control' FOR UPDATE`, identity.DeviceID, identity.TenantID, input.RequestID))
	if errors.Is(err, sql.ErrNoRows) && (input.Status == "rejected" || input.Status == "timeout" || input.Status == "failed") {
		payload, _ := json.Marshal(input)
		hash := sha256.Sum256(payload)
		var value any
		if len(input.Value) > 0 {
			value = string(input.Value)
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO device_business_events(device_id,tenant_id,room_id,request_id,business_type,payload_hash,status,action,operation,transcript,result_text,value_json,message,expires_at) VALUES(?,?,?,?,'voice_control',?,?,?,?,'','',CAST(? AS JSON),?,UTC_TIMESTAMP(3))`, identity.DeviceID, identity.TenantID, identity.RoomID, input.RequestID, hex.EncodeToString(hash[:]), input.Status, input.Action, input.Operation, value, input.Message)
		if e != nil {
			return out, e
		}
		id, e := res.LastInsertId()
		if e != nil {
			return out, e
		}
		out, e = scanDeviceBusinessEvent(tx.QueryRowContext(ctx, `SELECT `+deviceBusinessColumns+` FROM device_business_events WHERE id=?`, id))
		if e != nil {
			return out, e
		}
		return out, tx.Commit()
	}
	if err != nil {
		return out, err
	}
	status := input.Status
	if out.Status == "executing" && (out.Action != input.Action || out.Operation != input.Operation) {
		return out, ErrDeviceBusinessConflict
	}
	if (out.Status == "recognized" || out.Status == "executing") && out.ExpiresAt.Before(time.Now().UTC()) {
		status = "timeout"
		input.Message = "执行回执超时"
	}
	if out.Status != "recognized" && out.Status != "executing" && !(out.Status == "processing" && status != "succeeded") {
		// Retransmission of the identical terminal receipt is harmless. Late success
		// cannot resurrect failed/timeout/rejected events.
		if out.Status == status && out.Action == input.Action && out.Operation == input.Operation && jsonValueEqual(out.Value, input.Value) && out.Message == input.Message {
			return out, nil
		}
		return out, ErrDeviceBusinessConflict
	}
	var value any
	if len(input.Value) > 0 {
		value = string(input.Value)
	}
	_, err = tx.ExecContext(ctx, `UPDATE device_business_events SET action=?,operation=?,status=?,value_json=CAST(? AS JSON),message=? WHERE id=?`, input.Action, input.Operation, status, value, input.Message, out.ID)
	if err != nil {
		return out, err
	}
	out.Action, out.Operation, out.Status, out.Value, out.Message = input.Action, input.Operation, status, input.Value, input.Message
	return out, tx.Commit()
}

func jsonValueEqual(a, b json.RawMessage) bool {
	var x, y any
	if len(a) == 0 {
		a = json.RawMessage("null")
	}
	if len(b) == 0 {
		b = json.RawMessage("null")
	}
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	aa, _ := json.Marshal(x)
	bb, _ := json.Marshal(y)
	return string(aa) == string(bb)
}

func (s *Store) DeviceBusinessEvents(ctx context.Context, tenantID, deviceID, beforeID int64, limit int) ([]model.DeviceBusinessEvent, error) {
	var found int64
	if err := s.db.QueryRowContext(ctx, `SELECT d.id FROM inv_devices d JOIN device_hardware_profiles p ON p.device_id=d.id WHERE d.id=? AND d.current_customer_id=? AND p.claimed_tenant_id=?`, deviceID, tenantID, tenantID).Scan(&found); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	query := `SELECT ` + deviceBusinessColumns + ` FROM device_business_events WHERE tenant_id=? AND device_id=?`
	args := []any{tenantID, deviceID}
	if beforeID > 0 {
		query += ` AND id<?`
		args = append(args, beforeID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	return s.queryDeviceBusinessEvents(ctx, query, args...)
}

func (s *Store) queryDeviceBusinessEvents(ctx context.Context, query string, args ...any) ([]model.DeviceBusinessEvent, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.DeviceBusinessEvent, 0)
	for rows.Next() {
		e, err := scanDeviceBusinessEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) QueuedDeviceSnapshots(ctx context.Context, tenantID, beforeID int64, limit int) ([]model.DeviceBusinessEvent, error) {
	if tenantID <= 0 {
		return nil, ErrDeviceClaimUnavailable
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	query := `SELECT ` + deviceBusinessColumns + ` FROM device_business_events WHERE tenant_id=? AND business_type='environment_snapshot' AND status='queued' AND device_id IN (SELECT d.id FROM inv_devices d JOIN device_hardware_profiles p ON p.device_id=d.id WHERE d.current_customer_id=? AND p.claimed_tenant_id=?)`
	args := []any{tenantID, tenantID, tenantID}
	if beforeID > 0 {
		query += ` AND id<?`
		args = append(args, beforeID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	return s.queryDeviceBusinessEvents(ctx, query, args...)
}

func (s *Store) InternalDeviceBusinessMAC(ctx context.Context, tenantID, deviceID int64) (string, error) {
	var mac string
	err := s.db.QueryRowContext(ctx, `SELECT p.hardware_mac FROM inv_devices d JOIN device_hardware_profiles p ON p.device_id=d.id WHERE d.id=? AND d.current_customer_id=? AND p.claimed_tenant_id=? AND d.quality_status='qualified' AND d.lifecycle_status IN ('CUSTOMER_BOUND','ACTIVE','SOLD')`, deviceID, tenantID, tenantID).Scan(&mac)
	return mac, err
}

func (s *Store) CompleteDeviceSnapshot(ctx context.Context, rawMAC, requestID, status, resultText string) (model.DeviceBusinessEvent, error) {
	mac, err := model.NormalizeHardwareMAC(rawMAC)
	if err != nil {
		return model.DeviceBusinessEvent{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.DeviceBusinessEvent{}, err
	}
	defer tx.Rollback()
	identity, err := deviceBusinessIdentityTx(ctx, tx, mac)
	if err != nil {
		return model.DeviceBusinessEvent{}, err
	}
	out, err := scanDeviceBusinessEvent(tx.QueryRowContext(ctx, `SELECT `+deviceBusinessColumns+` FROM device_business_events WHERE device_id=? AND tenant_id=? AND request_id=? AND business_type='environment_snapshot' FOR UPDATE`, identity.DeviceID, identity.TenantID, requestID))
	if err != nil {
		return out, err
	}
	if out.Status != "queued" {
		if out.Status == status && out.ResultText == resultText {
			return out, nil
		}
		return out, ErrDeviceBusinessConflict
	}
	if status != "succeeded" && status != "failed" {
		return out, ErrDeviceBusinessConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE device_business_events SET status=?,result_text=? WHERE id=?`, status, resultText, out.ID)
	if err != nil {
		return out, err
	}
	out.Status, out.ResultText = status, resultText
	return out, tx.Commit()
}

func (s *Store) DeviceBusinessAsset(ctx context.Context, tenantID, deviceID, eventID int64) (model.MediaAsset, error) {
	var asset int64
	err := s.db.QueryRowContext(ctx, `SELECT e.asset_id FROM device_business_events e JOIN inv_devices d ON d.id=e.device_id JOIN device_hardware_profiles p ON p.device_id=d.id WHERE e.id=? AND e.device_id=? AND e.tenant_id=? AND d.current_customer_id=e.tenant_id AND p.claimed_tenant_id=e.tenant_id AND e.asset_id IS NOT NULL`, eventID, deviceID, tenantID).Scan(&asset)
	if err != nil {
		return model.MediaAsset{}, err
	}
	return s.GetMediaAsset(ctx, tenantID, asset)
}

func (s *Store) FailDeviceBusiness(ctx context.Context, eventID, tenantID int64, status, message string) error {
	if status != "failed" && status != "timeout" {
		return fmt.Errorf("invalid terminal status")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE device_business_events SET status=?,message=? WHERE id=? AND tenant_id=? AND status='processing'`, status, strings.TrimSpace(message), eventID, tenantID)
	return err
}
