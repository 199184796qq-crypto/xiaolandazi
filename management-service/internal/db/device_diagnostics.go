package db

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"livecompanion/management/internal/deviceops"
	"livecompanion/management/internal/model"
)

const deviceDiagnosticsSchema = `CREATE TABLE IF NOT EXISTS device_diagnostic_batches (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,device_id BIGINT UNSIGNED NOT NULL,
 owner_epoch CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 batch_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 boot_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 firmware_version VARCHAR(48) NOT NULL,firmware_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 storage_kind VARCHAR(16) NOT NULL,seq_start BIGINT UNSIGNED NOT NULL,seq_end BIGINT UNSIGNED NOT NULL,
 event_count INT UNSIGNED NOT NULL,size_bytes INT UNSIGNED NOT NULL,
 checksum_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 storage_driver VARCHAR(16) NOT NULL DEFAULT 'local',object_key VARCHAR(768) NOT NULL,received_at DATETIME(3) NOT NULL,
 UNIQUE KEY uk_device_diagnostic_batch(tenant_id,device_id,owner_epoch,batch_id),
 KEY idx_device_diagnostic_owner(tenant_id,device_id,id),
 KEY idx_device_diagnostic_quota(device_id,received_at)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`

func (s *Store) MigrateDeviceDiagnostics(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, deviceDiagnosticsSchema)
	return err
}

func maintenanceIdentityTx(ctx context.Context, tx *sql.Tx, mac, cid, secret string) (deviceops.Identity, error) {
	var out deviceops.Identity
	identity, err := deviceBusinessIdentityTx(ctx, tx, mac)
	if err != nil {
		if errors.Is(err, ErrDeviceClaimUnavailable) || errors.Is(err, sql.ErrNoRows) {
			return out, deviceops.ErrForbidden
		}
		return out, err
	}
	out.TenantID, out.DeviceID, out.MAC = identity.TenantID, identity.DeviceID, mac
	var claimed sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT claimed_at FROM device_hardware_profiles WHERE device_id=? FOR UPDATE`, out.DeviceID).Scan(&claimed); err != nil {
		return out, err
	}
	out.OwnerEpoch = deviceops.OwnerEpoch(secret, out.TenantID, out.DeviceID, claimed.Time)
	if cid != "" {
		if !deviceops.ValidConnectionID(cid) {
			return out, deviceops.ErrForbidden
		}
		var connection, metadata string
		var heartbeat sql.NullTime
		if err := tx.QueryRowContext(ctx, `SELECT connection_status,COALESCE(metadata_json,'{}'),last_heartbeat_at FROM live_device_runtime_state WHERE tenant_id=? AND device_id=? FOR UPDATE`, out.TenantID, out.DeviceID).Scan(&connection, &metadata, &heartbeat); err != nil {
			return out, err
		}
		var fields map[string]any
		if json.Unmarshal([]byte(metadata), &fields) != nil || fields["gateway_connection_id"] != cid || fields["transport"] != "xiaozhi_websocket_proven" || connection != "online" || !heartbeat.Valid || time.Since(heartbeat.Time) > 60*time.Second {
			return out, deviceops.ErrForbidden
		}
		out.ConnectionID = cid
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_runtime_sessions WHERE tenant_id=? AND device_id=? AND status IN ('running','paused')`, out.TenantID, out.DeviceID).Scan(&active); err != nil {
		return out, err
	}
	out.Busy = active > 0
	return out, nil
}
func (s *Store) ResolveDeviceMaintenanceIdentity(ctx context.Context, rawMAC, cid, secret string) (deviceops.Identity, error) {
	mac, err := model.NormalizeHardwareMAC(rawMAC)
	if err != nil {
		return deviceops.Identity{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return deviceops.Identity{}, err
	}
	defer tx.Rollback()
	out, err := maintenanceIdentityTx(ctx, tx, mac, cid, secret)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
func maintenanceOwnerTx(ctx context.Context, tx *sql.Tx, want deviceops.Identity, secret string) (deviceops.Identity, error) {
	var mac string
	if err := tx.QueryRowContext(ctx, `SELECT hardware_mac FROM device_hardware_profiles WHERE device_id=?`, want.DeviceID).Scan(&mac); err != nil {
		return deviceops.Identity{}, err
	}
	out, err := maintenanceIdentityTx(ctx, tx, mac, "", secret)
	if err != nil {
		return out, err
	}
	if out.DeviceID != want.DeviceID || out.TenantID != want.TenantID || subtle.ConstantTimeCompare([]byte(out.OwnerEpoch), []byte(want.OwnerEpoch)) != 1 {
		return out, deviceops.ErrForbidden
	}
	return out, nil
}
func (s *Store) CheckDeviceMaintenanceOwner(ctx context.Context, want deviceops.Identity, secret string) (deviceops.Identity, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return deviceops.Identity{}, err
	}
	defer tx.Rollback()
	out, err := maintenanceOwnerTx(ctx, tx, want, secret)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

const diagnosticColumns = `id,tenant_id,device_id,owner_epoch,batch_id,boot_id,firmware_version,firmware_sha256,storage_kind,seq_start,seq_end,event_count,size_bytes,checksum_sha256,storage_driver,object_key,received_at`

func scanDiagnostic(row interface{ Scan(...any) error }) (deviceops.StoredBatch, error) {
	var b deviceops.StoredBatch
	err := row.Scan(&b.ID, &b.TenantID, &b.DeviceID, &b.OwnerEpoch, &b.BatchID, &b.BootID, &b.FirmwareVersion, &b.FirmwareSHA256, &b.Storage, &b.SeqStart, &b.SeqEnd, &b.EventCount, &b.SizeBytes, &b.ChecksumSHA256, &b.StorageDriver, &b.ObjectKey, &b.ReceivedAt)
	if err == nil && b.StorageDriver != "local" {
		return b, errors.New("日志存储驱动无效")
	}
	return b, err
}

// The inventory ownership lock remains held through the bounded private write
// and index commit. A customer transfer cannot move a batch into its new owner.
// Retrying an uncertain commit is safe: the same ID and checksum is ACKed only
// after its private object is present; a different checksum never overwrites it.
func (s *Store) RecordDeviceDiagnosticBatch(ctx context.Context, b deviceops.StoredBatch, secret string, writePrivate func() error) (deviceops.StoredBatch, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return b, false, err
	}
	defer tx.Rollback()
	if b.StorageDriver != "" && b.StorageDriver != "local" {
		return b, false, errors.New("日志存储驱动无效")
	}
	b.StorageDriver = "local"
	if b.TenantID <= 0 || b.DeviceID <= 0 || !deviceops.HexID(b.OwnerEpoch, 64, 64) || !deviceops.HexID(b.BatchID, 32, 64) || !deviceops.HexID(b.ChecksumSHA256, 64, 64) || b.SizeBytes <= 0 || b.SizeBytes > deviceops.MaxBatchBytes || b.EventCount <= 0 || b.EventCount > 128 || writePrivate == nil {
		return b, false, errors.New("日志批次索引无效")
	}
	_, err = maintenanceOwnerTx(ctx, tx, deviceops.Identity{TenantID: b.TenantID, DeviceID: b.DeviceID, OwnerEpoch: b.OwnerEpoch}, secret)
	if err != nil {
		return b, false, err
	}
	old, err := scanDiagnostic(tx.QueryRowContext(ctx, `SELECT `+diagnosticColumns+` FROM device_diagnostic_batches WHERE tenant_id=? AND device_id=? AND owner_epoch=? AND batch_id=? FOR UPDATE`, b.TenantID, b.DeviceID, b.OwnerEpoch, b.BatchID))
	if err == nil {
		if old.ChecksumSHA256 != b.ChecksumSHA256 || old.SizeBytes != b.SizeBytes || old.ObjectKey != b.ObjectKey {
			return b, false, deviceops.ErrConflict
		}
		if err := writePrivate(); err != nil {
			return b, false, err
		}
		return old, true, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return b, false, err
	}
	now := time.Now().UTC()
	var minute, count, bytes int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(received_at>=?),0),COUNT(*),COALESCE(SUM(size_bytes),0) FROM device_diagnostic_batches WHERE device_id=? AND received_at>=?`, now.Add(-time.Minute), b.DeviceID, now.Add(-24*time.Hour)).Scan(&minute, &count, &bytes); err != nil {
		return b, false, err
	}
	if minute >= deviceops.MaxBatchesPerMinute || count >= deviceops.MaxDailyBatches || bytes+b.SizeBytes > deviceops.MaxDailyBytes {
		return b, false, deviceops.ErrQuota
	}
	if err := writePrivate(); err != nil {
		return b, false, err
	}
	b.ReceivedAt = now
	result, err := tx.ExecContext(ctx, `INSERT INTO device_diagnostic_batches(tenant_id,device_id,owner_epoch,batch_id,boot_id,firmware_version,firmware_sha256,storage_kind,seq_start,seq_end,event_count,size_bytes,checksum_sha256,object_key,received_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, b.TenantID, b.DeviceID, b.OwnerEpoch, b.BatchID, b.BootID, b.FirmwareVersion, b.FirmwareSHA256, b.Storage, b.SeqStart, b.SeqEnd, b.EventCount, b.SizeBytes, b.ChecksumSHA256, b.ObjectKey, b.ReceivedAt)
	if err != nil {
		return b, false, err
	}
	b.ID, err = result.LastInsertId()
	if err != nil {
		return b, false, err
	}
	return b, false, tx.Commit()
}
func (s *Store) ListDeviceDiagnosticBatches(ctx context.Context, tenant, device, before int64, limit int) ([]deviceops.StoredBatch, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	query := `SELECT ` + diagnosticColumns + ` FROM device_diagnostic_batches WHERE tenant_id=? AND device_id=?`
	args := []any{tenant, device}
	if before > 0 {
		query += ` AND id<?`
		args = append(args, before)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []deviceops.StoredBatch{}
	for rows.Next() {
		b, err := scanDiagnostic(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (s *Store) GetDeviceDiagnosticBatch(ctx context.Context, tenant, device int64, epoch, batch string) (deviceops.StoredBatch, error) {
	if !deviceops.HexID(epoch, 64, 64) || !deviceops.HexID(batch, 32, 64) {
		return deviceops.StoredBatch{}, sql.ErrNoRows
	}
	return scanDiagnostic(s.db.QueryRowContext(ctx, `SELECT `+diagnosticColumns+` FROM device_diagnostic_batches WHERE tenant_id=? AND device_id=? AND owner_epoch=? AND batch_id=? LIMIT 1`, tenant, device, epoch, strings.TrimSpace(batch)))
}
