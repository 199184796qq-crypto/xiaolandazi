package db

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

//go:embed device_addressing_schema.sql
var deviceAddressingSchema string

func (s *Store) MigrateDeviceAddressing(ctx context.Context) error {
	for _, q := range strings.Split(strings.ReplaceAll(deviceAddressingSchema, "\r\n", "\n"), "\n-- +statement\n") {
		if _, err := s.db.ExecContext(ctx, strings.TrimSpace(q)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeviceAddressingMode(ctx context.Context, tenantID, deviceID int64) (string, error) {
	var mode string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(a.mode,'auto') FROM inv_devices d JOIN device_hardware_profiles p ON p.device_id=d.id LEFT JOIN device_addressing_preferences a ON a.device_id=d.id AND a.tenant_id=p.claimed_tenant_id WHERE d.id=? AND d.current_customer_id=? AND p.claimed_tenant_id=?`, deviceID, tenantID, tenantID).Scan(&mode)
	if err == nil && !model.ValidDeviceAddressingMode(mode) {
		mode = "auto"
	}
	return mode, err
}

func (s *Store) SetDeviceAddressingMode(ctx context.Context, tenantID, userID, deviceID int64, mode string) (model.DeviceAddressingPreference, error) {
	if !model.ValidDeviceAddressingMode(mode) {
		return model.DeviceAddressingPreference{}, errors.New("invalid addressing mode")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.DeviceAddressingPreference{}, err
	}
	defer tx.Rollback()
	var owner sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT current_customer_id FROM inv_devices WHERE id=? FOR UPDATE`, deviceID).Scan(&owner); err != nil {
		return model.DeviceAddressingPreference{}, err
	}
	var claimed sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT claimed_tenant_id FROM device_hardware_profiles WHERE device_id=? FOR UPDATE`, deviceID).Scan(&claimed); err != nil {
		return model.DeviceAddressingPreference{}, err
	}
	if !owner.Valid || !claimed.Valid || owner.Int64 != tenantID || claimed.Int64 != tenantID {
		return model.DeviceAddressingPreference{}, sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_addressing_preferences(device_id,tenant_id,mode,updated_by_user_id) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE mode=VALUES(mode),updated_by_user_id=VALUES(updated_by_user_id)`, deviceID, tenantID, mode, userID); err != nil {
		return model.DeviceAddressingPreference{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_provisioning_events(device_id,tenant_id,actor_user_id,event_code,detail) VALUES(?,?,?,'ADDRESSING_PREFERENCE',?)`, deviceID, tenantID, userID, mode); err != nil {
		return model.DeviceAddressingPreference{}, err
	}
	return model.DeviceAddressingPreference{Mode: mode}, tx.Commit()
}

func (s *Store) VoiceSpeakerAddressing(ctx context.Context, tenantID, deviceID, eventID int64) (model.DeviceVoiceAddressing, error) {
	out := model.DeviceVoiceAddressing{Addressing: "neutral", Source: "neutral"}
	var addressing, source sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT a.addressing,a.source FROM device_business_events e JOIN inv_devices d ON d.id=e.device_id JOIN device_hardware_profiles p ON p.device_id=d.id LEFT JOIN device_voice_addressing a ON a.event_id=e.id AND a.tenant_id=e.tenant_id AND a.device_id=e.device_id WHERE e.id=? AND e.device_id=? AND e.tenant_id=? AND d.current_customer_id=? AND p.claimed_tenant_id=?`, eventID, deviceID, tenantID, tenantID, tenantID).Scan(&addressing, &source)
	if err == nil && addressing.Valid && model.ValidSpeakerAddressing(addressing.String) {
		out.Addressing = addressing.String
		if source.Valid {
			out.Source = source.String
		}
	}
	return out, err
}
