package db

import (
	"context"
	"database/sql"
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
	reconnecting := now.Add(-30 * time.Second)
	stale := now.Add(-61 * time.Second)
	for _, tt := range []struct {
		connection, work, reason string
		heartbeat                *time.Time
		want                     string
	}{
		{"online", "working", "", &now, "正常工作"}, {"online", "paused", "", &now, "已暂停"}, {"online", "idle", "", &now, "连续待机"},
		{"error", "working", "", &now, "设备异常"}, {"connecting", "idle", "", &now, "连接中"},
		{"error", "working", "", &reconnecting, "设备异常"},
		{"online", "working", "", &reconnecting, "连接中"},
		{"offline", "idle", "device_offline", &now, "连接中"},
		{"online", "working", "", &stale, "离线"},
		{"offline", "idle", "device_shutdown", &now, "离线"},
	} {
		d := model.LiveDevice{ID: 25, SN: "1c:29:04:31:0e:b8", ConnectionStatus: tt.connection, WorkStatus: tt.work, StopReason: tt.reason, LastHeartbeatAt: tt.heartbeat}
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
	if err := s.MigrateRoomDeletions(ctx); err != nil {
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
	// Customer account release must remove current associations, retain the
	// inventory/audit record, issue a new claimable code and prevent old access.
	exec(`INSERT INTO inv_devices(id,sn,sku_code,quality_status,lifecycle_status,current_customer_id) VALUES (4,'SN-004','XZ-CAM','qualified','SOLD',25)`)
	exec(`INSERT INTO device_hardware_profiles(device_id,hardware_mac) VALUES (4,'1c:29:04:31:0e:bc')`)
	listenerCode, err := s.ProvisionDevice(ctx, "1c:29:04:31:0e:bc", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimDevice(ctx, 25, 101, model.ClaimDeviceInput{BindingCode: listenerCode.Code, DeviceName: "保留的监听盒子"}, "test-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BindLiveDevice(ctx, 25, 101, 4, 25, "listener"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDeviceAddressingMode(ctx, 25, 101, 1, "female"); err != nil {
		t.Fatal(err)
	}
	if err := s.UnclaimLiveDevice(ctx, 26, 102, 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("other customer released device", err)
	}
	for _, status := range []string{"running", "paused", "starting", "resuming"} {
		exec(`INSERT INTO live_runtime_sessions(external_id,tenant_id,room_id,device_id,status,execution_realm,started_at,last_billed_at) VALUES('unclaim-live-test',25,25,4,?,'test',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))`, status)
		if err := s.UnclaimLiveDevice(ctx, 25, 101, 1); !errors.Is(err, ErrLiveBindingBusy) {
			t.Fatal("live room account release accepted", status, err)
		}
		exec(`DELETE FROM live_runtime_sessions WHERE external_id='unclaim-live-test'`)
	}
	if err := s.UnclaimLiveDevice(ctx, 25, 101, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetLiveDevice(ctx, 25, 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("old account still reads released box", err)
	}
	if _, err := s.DeviceAddressingMode(ctx, 25, 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("old account still reads preference", err)
	}
	var preferenceCount, inventoryCount, auditCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_addressing_preferences WHERE device_id=1`).Scan(&preferenceCount); err != nil || preferenceCount != 0 {
		t.Fatal("account preference retained", preferenceCount, err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inv_devices WHERE id=1 AND current_customer_id IS NULL AND lifecycle_status='SOLD'`).Scan(&inventoryCount); err != nil || inventoryCount != 1 {
		t.Fatal("inventory record removed or still owned", inventoryCount, err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_provisioning_events WHERE device_id=1 AND tenant_id=25 AND actor_user_id=101 AND event_code='OWNERSHIP_RELEASED'`).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatal("release not audited", auditCount, err)
	}
	if listener, err := s.GetLiveDevice(ctx, 25, 4); err != nil || listener.BindingRole != "primary" {
		t.Fatal("remaining listener not promoted", listener, err)
	}
	if err := s.UnclaimLiveDevice(ctx, 25, 101, 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("replayed release accepted", err)
	}
	newCode, err := s.ProvisionDevice(ctx, "1c:29:04:31:0e:b8", "test-secret")
	if err != nil || newCode.State != "claimable" || len(newCode.Code) != 6 || newCode.TenantID != 0 || newCode.RoomID != 0 || newCode.DeviceName != "" {
		t.Fatal("released code or former account data", newCode, err)
	}
	if _, err := s.ClaimDevice(ctx, 26, 102, model.ClaimDeviceInput{BindingCode: newCode.Code, DeviceName: "重新认领盒子"}, "test-secret"); err != nil {
		t.Fatal("customer released box cannot be reclaimed", err)
	}
	if err := s.UnclaimLiveDevice(ctx, 25, 101, 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("old request released new owner", err)
	}
	if got, err := s.DeviceAddressingMode(ctx, 26, 1); err != nil || got != "auto" {
		t.Fatal("new account inherited old preference", got, err)
	}
}
