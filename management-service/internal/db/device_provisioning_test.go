package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestClaimCodeEncryptionAndIdentity(t *testing.T) {
	sealed, err := sealClaimCode("032518", "secret", 25)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "032518") {
		t.Fatal("raw code stored")
	}
	code, err := openClaimCode(sealed, "secret", 25)
	if err != nil || code != "032518" {
		t.Fatal(code, err)
	}
	if _, err := openClaimCode(sealed, "other", 25); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err := openClaimCode(sealed, "secret", 26); err == nil {
		t.Fatal("code can be moved to another device")
	}
}
func TestLiveDevicePrivacyAndStatuses(t *testing.T) {
	now := time.Now()
	stale := now.Add(-time.Minute)
	for _, tt := range []struct {
		connection, work string
		heartbeat        *time.Time
		want             string
	}{
		{"online", "working", &now, "正常工作"}, {"online", "paused", &now, "已暂停"}, {"online", "idle", &now, "连续待机"},
		{"error", "working", &now, "设备异常"}, {"connecting", "idle", &now, "连接中"}, {"online", "working", &stale, "离线"},
	} {
		d := model.LiveDevice{ID: 25, SN: "1c:29:04:31:0e:b8", ConnectionStatus: tt.connection, WorkStatus: tt.work, LastHeartbeatAt: tt.heartbeat}
		decorateLiveDevice(&d)
		if d.DisplayStatus != tt.want {
			t.Fatal(tt, d.DisplayStatus)
		}
		raw, _ := json.Marshal(d)
		if strings.Contains(string(raw), "1c:29") {
			t.Fatal("MAC leaked")
		}
	}
}

// This exercises transactions against a newly-created, automatically-cleaned
// schema. The fixture requires explicit -sales-test-env-file, never production rows.
func TestDeviceProvisioningMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
		if strings.HasPrefix(strings.TrimSpace(raw), "CREATE TABLE IF NOT EXISTS live_device_") || strings.HasPrefix(strings.TrimSpace(raw), "CREATE TABLE IF NOT EXISTS live_runtime_") {
			exec(strings.TrimSpace(raw))
		}
	}
	if err := s.MigrateDeviceProvisioning(ctx); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO mgmt_tenants(id,code,name,status) VALUES (25,'device-owner','Owner','active'),(26,'device-other','Other','active')`)
	exec(`CREATE TABLE core_rooms(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,name VARCHAR(128) NOT NULL,external_room_id VARCHAR(128) NOT NULL)`)
	exec(`INSERT INTO core_rooms(id,tenant_id,name,external_room_id) VALUES(25,25,'测试房间','room-2026-123')`)
	exec(`INSERT INTO inv_devices(id,sn,sku_code,quality_status,lifecycle_status,current_customer_id) VALUES (1,'SN-001','XZ-CAM','qualified','SOLD',25),(2,'SN-002','XZ-CAM','qualified','SOLD',25),(3,'SN-003','XZ-CAM','qualified','IN_STOCK',NULL)`)
	exec(`INSERT INTO device_hardware_profiles(device_id,hardware_mac) VALUES (1,'1c:29:04:31:0e:b8'),(2,'1c:29:04:31:0e:b9'),(3,'1c:29:04:31:0e:ba')`)
	unregistered, err := s.ProvisionDevice(ctx, "1c:29:04:31:0e:bb", "test-secret")
	if err != nil || unregistered.State != "unregistered" {
		t.Fatal(unregistered, err)
	}
	disabled, err := s.ProvisionDevice(ctx, "1c:29:04:31:0e:ba", "test-secret")
	if err != nil || disabled.Code != "" || disabled.State != "disabled" {
		t.Fatal(disabled, err)
	}
	list, err := s.ListLiveDevices(ctx, 25)
	if err != nil || len(list) != 0 {
		t.Fatal("unclaimed shipment visible", list, err)
	}
	p, err := s.ProvisionDevice(ctx, "1c:29:04:31:0e:b8", "test-secret")
	if err != nil || len(p.Code) != 6 {
		t.Fatal(p, err)
	}
	repeat, err := s.ProvisionDevice(ctx, "1C2904310EB8", "test-secret")
	if err != nil || repeat.Code != p.Code {
		t.Fatal("code changed during validity", repeat, err)
	}
	if _, err := s.ClaimDevice(ctx, 26, 102, model.ClaimDeviceInput{BindingCode: p.Code, DeviceName: model.DefaultDeviceName}, "test-secret"); !errors.Is(err, ErrDeviceClaimUnavailable) {
		t.Fatal("wrong customer can claim", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ClaimDevice(ctx, 25, 101, model.ClaimDeviceInput{BindingCode: p.Code, DeviceName: model.DefaultDeviceName}, "test-secret")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrDeviceClaimCode) && !errors.Is(err, ErrDeviceClaimUnavailable) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("concurrent claim success count", successes)
	}
	if _, err := s.ClaimDevice(ctx, 25, 101, model.ClaimDeviceInput{BindingCode: p.Code, DeviceName: "other"}, "test-secret"); !errors.Is(err, ErrDeviceClaimCode) {
		t.Fatal("code reused", err)
	}
	name, err := s.NextDeviceName(ctx, 25)
	if err != nil || name != "小蓝搭子02" {
		t.Fatal(name, err)
	}
	second, err := s.ProvisionDevice(ctx, "1c:29:04:31:0e:b9", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimDevice(ctx, 25, 101, model.ClaimDeviceInput{BindingCode: second.Code, DeviceName: model.DefaultDeviceName}, "test-secret"); !errors.Is(err, ErrDeviceNameUsed) {
		t.Fatal("duplicate name accepted", err)
	}
	if _, err := s.ClaimDevice(ctx, 25, 101, model.ClaimDeviceInput{BindingCode: second.Code, DeviceName: name}, "test-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameDevice(ctx, 26, 102, 1, "stolen"); err == nil {
		t.Fatal("cross tenant rename")
	}
	if _, err := s.BindLiveDevice(ctx, 26, 102, 1, 25, "primary"); err == nil {
		t.Fatal("cross tenant binding")
	}
	if _, err := s.BindLiveDevice(ctx, 25, 101, 1, 25, "primary"); err != nil {
		t.Fatal(err)
	}
	p, err = s.ProvisionDevice(ctx, "1c:29:04:31:0e:b8", "test-secret")
	if err != nil || p.State != "bound" || p.RoomID != 25 || p.RoomName != "测试房间" || p.RoomNumber != "room-2026-123" || p.Code != "" {
		t.Fatal(p, err)
	}
	if _, err := s.HardwareHeartbeat(ctx, "1c:29:04:31:0e:b8", 26); !errors.Is(err, ErrLiveDeviceNotBound) {
		t.Fatal("heartbeat for wrong room", err)
	}
	secondBinding, err := s.BindLiveDevice(ctx, 25, 101, 2, 25, "primary")
	if err != nil || secondBinding.BindingRole != "listener" {
		t.Fatal(err)
	}
	d, err := s.GetLiveDevice(ctx, 25, 1)
	if err != nil || d.RoomID == nil || *d.RoomID != 25 || d.BindingRole != "primary" {
		t.Fatal("existing primary was replaced by a newly bound device", d, err)
	}
	secondBinding, err = s.BindLiveDevice(ctx, 25, 101, 2, 25, "primary")
	if err != nil || secondBinding.BindingRole != "primary" {
		t.Fatal("listener could not be promoted", secondBinding, err)
	}
	d, err = s.GetLiveDevice(ctx, 25, 1)
	if err != nil || d.RoomID == nil || *d.RoomID != 25 || d.BindingRole != "listener" {
		t.Fatal("previous primary was not demoted to listener", d, err)
	}
	if _, err := s.UnbindLiveDevice(ctx, 25, 101, 2); err != nil {
		t.Fatal(err)
	}
	d, err = s.GetLiveDevice(ctx, 25, 1)
	if err != nil || d.BindingRole != "primary" {
		t.Fatal("listener was not promoted after primary unbind", d, err)
	}
	list, err = s.ListLiveDevices(ctx, 25)
	if err != nil || len(list) != 2 {
		t.Fatal("unbinding released ownership", list, err)
	}
	if err := s.ReleaseDeviceOwnership(ctx, 101, 2, "售后转移测试"); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListLiveDevices(ctx, 25)
	if err != nil || len(list) != 1 {
		t.Fatal("released device still visible", list, err)
	}
	released, err := s.ProvisionDevice(ctx, "1c:29:04:31:0e:b9", "test-secret")
	if err != nil || len(released.Code) != 6 {
		t.Fatal("released code invalid", released, err)
	}
	if _, err := s.ClaimDevice(ctx, 26, 102, model.ClaimDeviceInput{BindingCode: released.Code, DeviceName: model.DefaultDeviceName}, "test-secret"); err != nil {
		t.Fatal("new owner cannot claim", err)
	}
	name, err = s.NextDeviceName(ctx, 25)
	if err != nil || name != "小蓝搭子03" {
		t.Fatal("sequence reused after release", name, err)
	}
}
