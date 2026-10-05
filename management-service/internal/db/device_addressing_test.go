package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestDeviceAddressingMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range strings.Split(strings.ReplaceAll(inventorySchema, "\r\n", "\n"), "\n-- +statement\n") {
		exec(strings.TrimSpace(raw))
	}
	for _, raw := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		q := strings.TrimSpace(raw)
		if strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_device_") {
			exec(q)
		}
	}
	if err := s.MigrateDeviceProvisioning(ctx); err != nil {
		t.Fatal(err)
	}
	exec(`CREATE TABLE core_rooms(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,name VARCHAR(128) NOT NULL,external_room_id VARCHAR(128) NOT NULL)`)
	exec(`INSERT INTO inv_devices(id,sn,sku_code,quality_status,lifecycle_status,current_customer_id) VALUES(1,'ADDRESS1','CAM','qualified','ACTIVE',25),(2,'ADDRESS2','CAM','qualified','ACTIVE',26)`)
	exec(`INSERT INTO device_hardware_profiles(device_id,hardware_mac,claimed_tenant_id,device_name) VALUES(1,'01:02:03:04:05:01',25,'小蓝搭子'),(2,'01:02:03:04:05:02',26,'小蓝搭子')`)
	mode, err := s.DeviceAddressingMode(ctx, 25, 1)
	if err != nil || mode != "auto" {
		t.Fatal("default", mode, err)
	}
	for _, mode := range []string{"female", "male", "child", "neutral", "auto"} {
		out, err := s.SetDeviceAddressingMode(ctx, 25, 250, 1, mode)
		if err != nil || out.Mode != mode {
			t.Fatal(out, err)
		}
		got, err := s.DeviceAddressingMode(ctx, 25, 1)
		if err != nil || got != mode {
			t.Fatal("preference not persisted", got, err)
		}
	}
	if _, err := s.SetDeviceAddressingMode(ctx, 25, 250, 1, "identify-me"); err == nil {
		t.Fatal("biometric/unknown mode accepted")
	}
	if _, err := s.SetDeviceAddressingMode(ctx, 26, 260, 1, "female"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("other tenant changed preference", err)
	}
	if _, err := s.DeviceAddressingMode(ctx, 26, 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("other tenant read preference", err)
	}
	if got, err := s.DeviceAddressingMode(ctx, 26, 2); err != nil || got != "auto" {
		t.Fatal("another device inherited preference", got, err)
	}
	mac, hash := "01:02:03:04:05:01", strings.Repeat("a", 64)
	event, _, err := s.BeginDeviceBusiness(ctx, mac, "address001", "voice_control", hash, "")
	if err != nil {
		t.Fatal(err)
	}
	event, err = s.FinishDeviceVoiceAddressing(ctx, mac, event, "小蓝小蓝", model.DeviceVoiceAddressing{Addressing: "female", Source: "preference"}, true)
	if err != nil || event.Status != "succeeded" || event.Action != "wake_greeting" || event.BillingMode != "disabled" || event.ChargedBeans != 0 {
		t.Fatal("wake not terminal/no-charge", event, err)
	}
	stored, err := s.VoiceSpeakerAddressing(ctx, 25, 1, event.ID)
	if err != nil || stored.Addressing != "female" || stored.Source != "preference" {
		t.Fatal("cue not persisted atomically", stored, err)
	}
	if _, err := s.VoiceSpeakerAddressing(ctx, 26, 1, event.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("voice cue tenant leak", err)
	}
	if _, err := s.ClaimDeviceCommand(ctx, model.DeviceCommandDispatchInput{HardwareMAC: mac, RequestID: event.RequestID, Action: "volume", Operation: "set"}); !errors.Is(err, ErrDeviceBusinessConflict) {
		t.Fatal("wake greeting dispatch permitted", err)
	}
	if _, err := s.FinishDeviceVoiceAddressing(ctx, mac, event, "替换唤醒", model.DeviceVoiceAddressing{Addressing: "male", Source: "acoustic"}, false); !errors.Is(err, ErrDeviceBusinessConflict) {
		t.Fatal("terminal wake rewritten", err)
	}
	repeat, created, err := (&Store{db: s.db}).BeginDeviceBusiness(ctx, mac, event.RequestID, "voice_control", hash, "")
	if err != nil || created || repeat.ID != event.ID || repeat.Status != "succeeded" || repeat.Action != "wake_greeting" {
		t.Fatal("restarted gateway wake idempotency", repeat, created, err)
	}
	command, _, err := s.BeginDeviceBusiness(ctx, mac, "address002", "voice_control", hash, "")
	if err != nil {
		t.Fatal(err)
	}
	command, err = s.FinishDeviceVoiceAddressing(ctx, mac, command, "小蓝小蓝音量调到30%", model.DeviceVoiceAddressing{Addressing: "not-identity", Source: "untrusted"}, false)
	if err != nil || command.Status != "recognized" || command.Action != "" {
		t.Fatal("wake plus command incorrectly greeted", command, err)
	}
	stored, err = s.VoiceSpeakerAddressing(ctx, 25, 1, command.ID)
	if err != nil || stored.Addressing != "neutral" || stored.Source != "neutral" {
		t.Fatal("invalid cue not neutral", stored, err)
	}
	if _, err := s.SetDeviceAddressingMode(ctx, 25, 250, 1, "child"); err != nil {
		t.Fatal(err)
	}
	// Inventory ownership alone cannot inherit the former customer's preference.
	exec(`UPDATE inv_devices SET current_customer_id=26 WHERE id=1`)
	if _, err := s.DeviceAddressingMode(ctx, 25, 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("old inventory owner accepted", err)
	}
	if _, err := s.SetDeviceAddressingMode(ctx, 26, 260, 1, "male"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("claim/inventory mismatch accepted", err)
	}
	exec(`UPDATE device_hardware_profiles SET claimed_tenant_id=26,device_name='转移设备' WHERE device_id=1`)
	if got, err := s.DeviceAddressingMode(ctx, 26, 1); err != nil || got != "auto" {
		t.Fatal("former customer preference leaked", got, err)
	}
	if _, err := s.VoiceSpeakerAddressing(ctx, 25, 1, event.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("former customer read voice cue", err)
	}
	if _, err := s.VoiceSpeakerAddressing(ctx, 26, 1, event.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("new customer read old voice cue", err)
	}
	if _, err := s.SetDeviceAddressingMode(ctx, 26, 260, 1, "male"); err != nil {
		t.Fatal(err)
	}
	var charged int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(charged_beans),0) FROM device_business_events`).Scan(&charged); err != nil || charged != 0 {
		t.Fatal("addressing charged beans", charged, err)
	}
}
