package db

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

//go:embed device_provisioning_schema.sql
var deviceProvisioningSchema string

var (
	ErrDeviceClaimCode        = errors.New("绑定码不正确或已过期，请查看设备屏幕上的新码")
	ErrDeviceClaimUnavailable = errors.New("设备暂时不能认领或不属于当前客户")
	ErrDeviceNameUsed         = errors.New("设备名称已使用，请更换名称")
)

func (s *Store) MigrateDeviceProvisioning(ctx context.Context) error {
	for _, raw := range strings.Split(strings.ReplaceAll(deviceProvisioningSchema, "\r\n", "\n"), "\n-- +statement\n") {
		if _, err := s.db.ExecContext(ctx, strings.TrimSpace(raw)); err != nil {
			return fmt.Errorf("migrate device provisioning: %w", err)
		}
	}
	return s.MigrateDeviceBusiness(ctx)
}

func registerDeviceHardwareTx(ctx context.Context, tx *sql.Tx, deviceID int64, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	} // Older and non-Xiaozhi inventory remains supported.
	mac, err := model.NormalizeHardwareMAC(raw)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO device_hardware_profiles(device_id,hardware_mac,claim_enabled) SELECT id,?,(quality_status='qualified' AND lifecycle_status='IN_STOCK') FROM inv_devices WHERE id=?`, mac, deviceID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return sql.ErrNoRows
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO device_provisioning_events(device_id,event_code,detail) SELECT device_id,'ACTIVATION_ALLOWED','合格设备入库，默认允许激活' FROM device_hardware_profiles WHERE device_id=? AND claim_enabled=TRUE`, deviceID)
	return err
}

func claimCodeHash(code, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte("device-claim:" + code))
	return hex.EncodeToString(h.Sum(nil))
}

func claimCipher(secret string) (cipher.AEAD, error) {
	key := sha256.Sum256([]byte("device-claim-encryption:" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealClaimCode(code, secret string, deviceID int64) ([]byte, error) {
	gcm, err := claimCipher(secret)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(code), []byte(fmt.Sprint(deviceID))), nil
}

func openClaimCode(data []byte, secret string, deviceID int64) (string, error) {
	gcm, err := claimCipher(secret)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted code")
	}
	raw, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte(fmt.Sprint(deviceID)))
	return string(raw), err
}

func (s *Store) ProvisionDevice(ctx context.Context, rawMAC, secret string) (model.DeviceProvisioning, error) {
	mac, err := model.NormalizeHardwareMAC(rawMAC)
	if err != nil {
		return model.DeviceProvisioning{}, err
	}
	var deviceID int64
	err = s.db.QueryRowContext(ctx, `SELECT device_id FROM device_hardware_profiles WHERE hardware_mac=?`, mac).Scan(&deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return model.DeviceProvisioning{State: "unregistered", Message: "设备未登记，请联系售后"}, nil
	}
	if err != nil {
		return model.DeviceProvisioning{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.DeviceProvisioning{}, err
	}
	defer tx.Rollback()
	var lifecycle, quality string
	var expected sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT lifecycle_status,quality_status,current_customer_id FROM inv_devices WHERE id=? FOR UPDATE`, deviceID).Scan(&lifecycle, &quality, &expected); err != nil {
		return model.DeviceProvisioning{}, err
	}
	var enabled bool
	var claimed sql.NullInt64
	var name string
	var nameSequence int64
	var renamed bool
	if err := tx.QueryRowContext(ctx, `SELECT p.claim_enabled,p.claimed_tenant_id,p.device_name,p.name_sequence,EXISTS(SELECT 1 FROM device_provisioning_events e WHERE e.device_id=p.device_id AND e.event_code='RENAMED') FROM device_hardware_profiles p WHERE p.device_id=? FOR UPDATE`, deviceID).Scan(&enabled, &claimed, &name, &nameSequence, &renamed); err != nil {
		return model.DeviceProvisioning{}, err
	}
	out := model.DeviceProvisioning{DeviceID: deviceID}
	if quality != "qualified" || !(lifecycle == "SOLD" || lifecycle == "CUSTOMER_BOUND" || lifecycle == "ACTIVE" || (enabled && lifecycle == "IN_STOCK")) {
		out.State, out.Message = "disabled", "设备暂未激活，请联系销售"
	} else if claimed.Valid && expected.Valid && claimed.Int64 == expected.Int64 {
		out.TenantID, out.DeviceName = claimed.Int64, name
		// Only a proven system-generated legacy name is aliased on the device.
		// Persisted names and customer-written names remain untouched.
		if strings.HasPrefix(name, "小蓝直播助手") && !renamed {
			if sequence, ok := model.DeviceDefaultNameSequence(name); ok && sequence == nameSequence {
				alias := model.DefaultDeviceNameForSequence(sequence)
				var collisions int
				if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_hardware_profiles WHERE claimed_tenant_id=? AND device_id<>? AND device_name=?`, claimed.Int64, deviceID, alias).Scan(&collisions); err != nil {
					return out, err
				}
				if collisions == 0 {
					out.DeviceName = alias
				}
			}
		}
		out.State, out.Message = "claimed", "设备已添加，请选择房间"
		err := tx.QueryRowContext(ctx, `SELECT b.room_id,b.binding_role,r.name,r.external_room_id FROM live_device_room_bindings b JOIN core_rooms r ON r.id=b.room_id AND r.tenant_id=b.tenant_id WHERE b.device_id=? AND b.tenant_id=? AND b.status='active' ORDER BY b.id DESC LIMIT 1`, deviceID, claimed.Int64).Scan(&out.RoomID, &out.BindingRole, &out.RoomName, &out.RoomNumber)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
		if out.RoomID > 0 {
			out.State, out.Message = "bound", "连续待机"
		}
	} else if claimed.Valid {
		out.State, out.Message = "disabled", "设备归属异常，请联系售后"
	} else {
		out.State, out.Message = "claimable", "请在小蓝搭子手机端或电脑端添加设备"
		var encrypted []byte
		var expires time.Time
		err := tx.QueryRowContext(ctx, `SELECT encrypted_code,expires_at FROM device_claim_codes WHERE device_id=?`, deviceID).Scan(&encrypted, &expires)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
		if err == nil && expires.After(time.Now().UTC()) {
			out.Code, err = openClaimCode(encrypted, secret, deviceID)
		}
		if out.Code == "" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM device_claim_codes WHERE device_id=?`, deviceID); err != nil {
				return out, err
			}
			for attempt := 0; attempt < 12; attempt++ {
				n, err := rand.Int(rand.Reader, big.NewInt(1000000))
				if err != nil {
					return out, err
				}
				code := fmt.Sprintf("%06d", n.Int64())
				sealed, err := sealClaimCode(code, secret, deviceID)
				if err != nil {
					return out, err
				}
				expires = time.Now().UTC().Add(10 * time.Minute)
				_, err = tx.ExecContext(ctx, `INSERT INTO device_claim_codes(device_id,code_hash,encrypted_code,expires_at) VALUES (?,?,?,?)`, deviceID, claimCodeHash(code, secret), sealed, expires)
				if err != nil {
					if isDuplicateInventoryError(err) {
						continue
					}
					return out, err
				}
				out.Code = code
				break
			}
			if out.Code == "" {
				return out, errors.New("binding code allocation failed")
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO device_provisioning_events(device_id,event_code) VALUES (?,'CODE_ISSUED')`, deviceID); err != nil {
				return out, err
			}
		}
		out.ExpiresAt = &expires
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

func validateDeviceName(name string) error {
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 32 {
		return errors.New("设备名称需为 1-32 个字")
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return errors.New("设备名称不能包含控制字符")
		}
	}
	return nil
}

func (s *Store) NextDeviceName(ctx context.Context, tenantID int64) (string, error) {
	var seq int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT last_sequence FROM device_name_sequences WHERE tenant_id=?),0)`, tenantID).Scan(&seq)
	if err != nil {
		return "", err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT name_sequence,device_name FROM device_hardware_profiles WHERE claimed_tenant_id=?`, tenantID)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var stored int64
		var name string
		if err := rows.Scan(&stored, &name); err != nil {
			rows.Close()
			return "", err
		}
		if n, ok := model.DeviceDefaultNameSequence(name); ok && n > stored {
			stored = n
		}
		if stored > seq {
			seq = stored
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	for {
		seq++
		name := model.DefaultDeviceNameForSequence(seq)
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_hardware_profiles WHERE claimed_tenant_id=? AND device_name=?`, tenantID, name).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return name, nil
		}
	}
}

func (s *Store) ClaimDevice(ctx context.Context, tenantID, userID int64, input model.ClaimDeviceInput, secret string) (model.LiveDevice, error) {
	name := strings.TrimSpace(input.DeviceName)
	if err := validateDeviceName(name); err != nil {
		return model.LiveDevice{}, err
	}
	if len(input.BindingCode) != 6 || strings.IndexFunc(input.BindingCode, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return model.LiveDevice{}, ErrDeviceClaimCode
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveDevice{}, err
	}
	defer tx.Rollback()
	var tenantLock int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM mgmt_tenants WHERE id=? FOR UPDATE`, tenantID).Scan(&tenantLock); err != nil {
		return model.LiveDevice{}, err
	}
	var deviceID int64
	if err := tx.QueryRowContext(ctx, `SELECT device_id FROM device_claim_codes WHERE code_hash=? AND expires_at>UTC_TIMESTAMP(3)`, claimCodeHash(input.BindingCode, secret)).Scan(&deviceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.LiveDevice{}, ErrDeviceClaimCode
		}
		return model.LiveDevice{}, err
	}
	var lifecycle, quality string
	var expected sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT lifecycle_status,quality_status,current_customer_id FROM inv_devices WHERE id=? FOR UPDATE`, deviceID).Scan(&lifecycle, &quality, &expected); err != nil {
		return model.LiveDevice{}, err
	}
	var enabled bool
	var claimed sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT claim_enabled,claimed_tenant_id FROM device_hardware_profiles WHERE device_id=? FOR UPDATE`, deviceID).Scan(&enabled, &claimed); err != nil {
		return model.LiveDevice{}, err
	}
	if claimed.Valid || quality != "qualified" || (expected.Valid && expected.Int64 != tenantID) || !(lifecycle == "SOLD" || lifecycle == "CUSTOMER_BOUND" || lifecycle == "ACTIVE" || (enabled && lifecycle == "IN_STOCK")) {
		return model.LiveDevice{}, ErrDeviceClaimUnavailable
	}
	var codeHash string
	if err := tx.QueryRowContext(ctx, `SELECT code_hash FROM device_claim_codes WHERE device_id=? AND code_hash=? AND expires_at>UTC_TIMESTAMP(3) FOR UPDATE`, deviceID, claimCodeHash(input.BindingCode, secret)).Scan(&codeHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.LiveDevice{}, ErrDeviceClaimCode
		}
		return model.LiveDevice{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO device_name_sequences(tenant_id) VALUES (?)`, tenantID); err != nil {
		return model.LiveDevice{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE device_name_sequences SET last_sequence=last_sequence+1 WHERE tenant_id=?`, tenantID); err != nil {
		return model.LiveDevice{}, err
	}
	var sequence int64
	if err := tx.QueryRowContext(ctx, `SELECT last_sequence FROM device_name_sequences WHERE tenant_id=?`, tenantID).Scan(&sequence); err != nil {
		return model.LiveDevice{}, err
	}
	if explicit, ok := model.DeviceDefaultNameSequence(name); ok && explicit > sequence {
		sequence = explicit
		if _, err := tx.ExecContext(ctx, `UPDATE device_name_sequences SET last_sequence=? WHERE tenant_id=?`, sequence, tenantID); err != nil {
			return model.LiveDevice{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE device_hardware_profiles SET claimed_tenant_id=?,device_name=?,name_sequence=?,claimed_at=UTC_TIMESTAMP(3) WHERE device_id=?`, tenantID, name, sequence, deviceID); err != nil {
		if isDuplicateInventoryError(err) {
			return model.LiveDevice{}, ErrDeviceNameUsed
		}
		return model.LiveDevice{}, err
	}
	original, err := lockDeviceTx(ctx, tx, deviceID)
	if err != nil {
		return model.LiveDevice{}, err
	}
	docID, err := createStockDocumentTx(ctx, tx, "customer_bind", nil, nil, original.OwnerOrgID, "用户输入绑定码认领设备", userID)
	if err != nil {
		return model.LiveDevice{}, err
	}
	if err := insertStockItemAndLedgerTx(ctx, tx, deviceID, docID, original.SN, original.SKUCode, "customer_bind", lifecycle, "CUSTOMER_BOUND", original.CustodyWarehouseID, original.CustodyWarehouseID, original.OwnerOrgID, original.OwnerOrgID, original.CurrentCustomerID, &tenantID, userID, "用户输入绑定码认领设备"); err != nil {
		return model.LiveDevice{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE inv_devices SET current_customer_id=?,lifecycle_status='CUSTOMER_BOUND',updated_by_user_id=? WHERE id=?`, tenantID, userID, deviceID); err != nil {
		return model.LiveDevice{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM device_claim_codes WHERE device_id=?`, deviceID); err != nil {
		return model.LiveDevice{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_provisioning_events(device_id,tenant_id,actor_user_id,event_code,detail) VALUES (?,?,?,'CLAIMED',?)`, deviceID, tenantID, userID, name); err != nil {
		return model.LiveDevice{}, err
	}
	if err := s.commitInboxTx(ctx, tx, "inventory"); err != nil {
		return model.LiveDevice{}, err
	}
	return s.GetLiveDevice(ctx, tenantID, deviceID)
}

func (s *Store) RenameDevice(ctx context.Context, tenantID, userID, deviceID int64, name string) (model.LiveDevice, error) {
	name = strings.TrimSpace(name)
	if err := validateDeviceName(name); err != nil {
		return model.LiveDevice{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveDevice{}, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT d.id FROM inv_devices d JOIN device_hardware_profiles p ON p.device_id=d.id WHERE d.id=? AND d.current_customer_id=? AND p.claimed_tenant_id=? FOR UPDATE`, deviceID, tenantID, tenantID).Scan(&id); err != nil {
		return model.LiveDevice{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE device_hardware_profiles SET device_name=? WHERE device_id=?`, name, deviceID); err != nil {
		if isDuplicateInventoryError(err) {
			return model.LiveDevice{}, ErrDeviceNameUsed
		}
		return model.LiveDevice{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_provisioning_events(device_id,tenant_id,actor_user_id,event_code,detail) VALUES (?,?,?,'RENAMED',?)`, deviceID, tenantID, userID, name); err != nil {
		return model.LiveDevice{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveDevice{}, err
	}
	return s.GetLiveDevice(ctx, tenantID, deviceID)
}
