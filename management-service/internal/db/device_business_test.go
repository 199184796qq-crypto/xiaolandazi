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

func TestDeviceBusinessMySQL(t *testing.T) {
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
		q := strings.TrimSpace(raw)
		if strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_device_") || strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS media_assets ") {
			exec(q)
		}
	}
	if err := s.MigrateDeviceProvisioning(ctx); err != nil {
		t.Fatal(err)
	}
	exec(`CREATE TABLE core_rooms(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,name VARCHAR(128) NOT NULL,external_room_id VARCHAR(128) NOT NULL)`)
	exec(`INSERT INTO core_rooms VALUES(15,25,'美妆专场','external-915'),(16,26,'另一用户','external-916')`)
	exec(`INSERT INTO inv_devices(id,sn,sku_code,quality_status,lifecycle_status,current_customer_id) VALUES(1,'SN1','CAM','qualified','ACTIVE',25),(2,'SN2','CAM','qualified','ACTIVE',26),(3,'SN3','CAM','qualified','IN_STOCK',NULL),(4,'SN4','CAM','defective','ACTIVE',25)`)
	exec(`INSERT INTO device_hardware_profiles(device_id,hardware_mac,claimed_tenant_id,device_name,name_sequence) VALUES(1,'01:02:03:04:05:01',25,'小蓝直播助手02',2),(2,'01:02:03:04:05:02',26,'小蓝搭子',1),(3,'01:02:03:04:05:03',NULL,'',0),(4,'01:02:03:04:05:04',25,'不良设备',0)`)
	exec(`INSERT INTO live_device_room_bindings(tenant_id,device_id,room_id,binding_role,status) VALUES(25,1,15,'primary','active'),(26,2,16,'primary','active')`)
	mac := "01:02:03:04:05:01"
	hash := strings.Repeat("a", 64)
	identity, err := s.ResolveDeviceBusinessIdentity(ctx, mac)
	if err != nil || identity.TenantID != 25 || identity.RoomID == nil || *identity.RoomID != 15 {
		t.Fatal(identity, err)
	}
	for _, bad := range []string{"01:02:03:04:05:03", "01:02:03:04:05:04", "01:02:03:04:05:ff"} {
		if _, err := s.ResolveDeviceBusinessIdentity(ctx, bad); err == nil {
			t.Fatal("unusable device accepted", bad)
		}
	}
	event, newEvent, err := s.BeginDeviceBusiness(ctx, mac, "request001", "voice_control", hash, "")
	if err != nil || !newEvent || event.Status != "processing" || event.ChargedBeans != 0 {
		t.Fatal(event, newEvent, err)
	}
	repeat, created, err := s.BeginDeviceBusiness(ctx, mac, "request001", "voice_control", hash, "")
	if err != nil || created || repeat.ID != event.ID {
		t.Fatal("duplicate event", repeat, created, err)
	}
	if _, _, err := s.BeginDeviceBusiness(ctx, mac, "request001", "voice_control", strings.Repeat("b", 64), ""); !errors.Is(err, ErrDeviceBusinessConflict) {
		t.Fatal("payload replacement", err)
	}
	if _, _, err := s.BeginDeviceBusiness(ctx, mac, "request002", "voice_control", hash, ""); !errors.Is(err, ErrDeviceBusinessConflict) {
		t.Fatal("parallel ASR accepted", err)
	}
	event, err = s.FinishDeviceVoice(ctx, mac, event, "音量调到30%", "recognized", "")
	if err != nil {
		t.Fatal(err)
	}
	ack := model.DeviceCommandEventInput{HardwareMAC: mac, RequestID: event.RequestID, Action: "volume", Operation: "set", Status: "succeeded", Value: json.RawMessage("30"), Message: "已执行"}
	event, err = s.RecordDeviceCommand(ctx, ack)
	if err != nil || event.Status != "succeeded" {
		t.Fatal(event, err)
	}
	duplicate, err := s.RecordDeviceCommand(ctx, ack)
	if err != nil || duplicate.ID != event.ID {
		t.Fatal("receipt retry", duplicate, err)
	}
	ack.Value = json.RawMessage("31")
	if _, err := s.RecordDeviceCommand(ctx, ack); !errors.Is(err, ErrDeviceBusinessConflict) {
		t.Fatal("terminal receipt mutated", err)
	}
	ack.HardwareMAC = "01:02:03:04:05:02"
	if _, err := s.RecordDeviceCommand(ctx, ack); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("another tenant receipt accepted", err)
	}
	failed, _, err := s.BeginDeviceBusiness(ctx, mac, "request002", "voice_control", hash, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FailDeviceBusiness(ctx, failed.ID, 25, "failed", "ASR失败"); err != nil {
		t.Fatal(err)
	}
	ack.HardwareMAC = mac
	ack.RequestID = failed.RequestID
	ack.Value = json.RawMessage("30")
	if _, err := s.RecordDeviceCommand(ctx, ack); !errors.Is(err, ErrDeviceBusinessConflict) {
		t.Fatal("late success resurrected failure", err)
	}
	// A photo reuses the voice request ID without overwriting its terminal state.
	snapshot, _, err := s.BeginDeviceBusiness(ctx, mac, "request001", "environment_snapshot", hash, "看下环境")
	if err != nil {
		t.Fatal(err)
	}
	input := model.CreateMediaAssetInput{TenantID: 25, OriginalName: "环境.jpg", StorageDriver: "oss", StorageBucket: "private", ObjectKey: "25/snapshot.jpg", MIMEType: "image/jpeg", SizeBytes: 10, ChecksumSHA256: hash, Metadata: map[string]any{"private": true}}
	snapshot, err = s.QueueDeviceSnapshot(ctx, mac, snapshot, input)
	if err != nil || snapshot.Status != "queued" || snapshot.AssetID == nil {
		t.Fatal(snapshot, err)
	}
	if _, err = s.QueueDeviceSnapshot(ctx, mac, snapshot, input); !errors.Is(err, ErrDeviceBusinessConflict) {
		t.Fatal("duplicate asset accepted", err)
	}
	queue, err := s.QueuedDeviceSnapshots(ctx, 25, 0, 20)
	if err != nil || len(queue) != 1 || queue[0].ID != snapshot.ID {
		t.Fatal(queue, err)
	}
	other, err := s.QueuedDeviceSnapshots(ctx, 26, 0, 20)
	if err != nil || len(other) != 0 {
		t.Fatal("queue tenant leak", other, err)
	}
	if _, err = s.DeviceBusinessAsset(ctx, 26, 1, snapshot.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("asset tenant leak", err)
	}
	asset, err := s.DeviceBusinessAsset(ctx, 25, 1, snapshot.ID)
	if err != nil || asset.TenantID != 25 {
		t.Fatal(asset, err)
	}
	if _, err = s.CompleteDeviceSnapshot(ctx, "01:02:03:04:05:02", snapshot.RequestID, "succeeded", "偷看"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross tenant result", err)
	}
	snapshot, err = s.CompleteDeviceSnapshot(ctx, mac, snapshot.RequestID, "succeeded", "接口结果文本")
	if err != nil || snapshot.ResultText != "接口结果文本" {
		t.Fatal(snapshot, err)
	}
	if _, err = s.CompleteDeviceSnapshot(ctx, mac, snapshot.RequestID, "succeeded", "接口结果文本"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CompleteDeviceSnapshot(ctx, mac, snapshot.RequestID, "failed", "变更"); !errors.Is(err, ErrDeviceBusinessConflict) {
		t.Fatal("result terminal modified", err)
	}
	if _, err = s.DeviceBusinessEvents(ctx, 26, 1, 0, 20); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("customer event leak", err)
	}
	list, err := s.DeviceBusinessEvents(ctx, 25, 1, 0, 1)
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	older, err := s.DeviceBusinessEvents(ctx, 25, 1, list[0].ID, 20)
	if err != nil || len(older) != 2 {
		t.Fatal("paging", older, err)
	}
	// A new gateway process must not re-dispatch a relative command whose
	// hardware execution may already have happened before its ACK was stored.
	dispatched, _, err := s.BeginDeviceBusiness(ctx, mac, "dispatch001", "voice_control", hash, "")
	if err != nil {
		t.Fatal(err)
	}
	dispatched, err = s.FinishDeviceVoice(ctx, mac, dispatched, "声音大一点", "recognized", "")
	if err != nil {
		t.Fatal(err)
	}
	claim := model.DeviceCommandDispatchInput{HardwareMAC: mac, RequestID: dispatched.RequestID, Action: "volume", Operation: "adjust"}
	dispatched, err = s.ClaimDeviceCommand(ctx, claim)
	if err != nil || dispatched.Status != "executing" || dispatched.Action != "volume" || dispatched.Operation != "adjust" {
		t.Fatal("dispatch not persisted", dispatched, err)
	}
	restarted := &Store{db: s.db}
	if _, err := restarted.ClaimDeviceCommand(ctx, claim); !errors.Is(err, ErrDeviceBusinessConflict) {
		t.Fatal("gateway restart repeated relative command", err)
	}
	if duplicate, created, err := restarted.BeginDeviceBusiness(ctx, mac, claim.RequestID, "voice_control", hash, ""); err != nil || created || duplicate.Status != "executing" {
		t.Fatal("voice retry lost executing state", duplicate, created, err)
	}
	dispatchACK := model.DeviceCommandEventInput{HardwareMAC: mac, RequestID: claim.RequestID, Action: "font_size", Operation: "adjust", Status: "succeeded", Value: json.RawMessage("1")}
	if _, err := s.RecordDeviceCommand(ctx, dispatchACK); !errors.Is(err, ErrDeviceBusinessConflict) {
		t.Fatal("different dispatch action accepted", err)
	}
	dispatchACK.Action = "volume"
	dispatchACK.Operation = "set"
	dispatchACK.Value = json.RawMessage("40")
	if _, err := s.RecordDeviceCommand(ctx, dispatchACK); !errors.Is(err, ErrDeviceBusinessConflict) {
		t.Fatal("different dispatch operation accepted", err)
	}
	dispatchACK.Operation = "adjust"
	dispatched, err = s.RecordDeviceCommand(ctx, dispatchACK)
	if err != nil || dispatched.Status != "succeeded" {
		t.Fatal("matching dispatch ACK rejected", dispatched, err)
	}
	if _, err := s.ClaimDeviceCommand(ctx, claim); !errors.Is(err, ErrDeviceBusinessConflict) {
		t.Fatal("completed command redispatched", err)
	}
	if _, err := s.RecordDeviceCommand(ctx, dispatchACK); err != nil {
		t.Fatal("matching receipt retry failed", err)
	}
	claim.HardwareMAC = "01:02:03:04:05:02"
	if _, err := s.ClaimDeviceCommand(ctx, claim); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross tenant dispatch", err)
	}
	claim.HardwareMAC = mac
	claim.RequestID = "dispatch002"
	expired, _, err := s.BeginDeviceBusiness(ctx, mac, claim.RequestID, "voice_control", hash, "")
	if err != nil {
		t.Fatal(err)
	}
	expired, err = s.FinishDeviceVoice(ctx, mac, expired, "声音大一点", "recognized", "")
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE device_business_events SET expires_at=DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 1 SECOND) WHERE id=?`, expired.ID)
	if _, err := s.ClaimDeviceCommand(ctx, claim); !errors.Is(err, ErrDeviceBusinessConflict) {
		t.Fatal("expired command dispatched", err)
	}
	var expiredState string
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM device_business_events WHERE id=?`, expired.ID).Scan(&expiredState); err != nil || expiredState != "timeout" {
		t.Fatal("expired dispatch not terminal", expiredState, err)
	}
	claim.RequestID = "dispatch003"
	pending, _, err := s.BeginDeviceBusiness(ctx, mac, claim.RequestID, "voice_control", hash, "")
	if err != nil {
		t.Fatal(err)
	}
	pending, err = s.FinishDeviceVoice(ctx, mac, pending, "声音大一点", "recognized", "")
	if err != nil {
		t.Fatal(err)
	}
	pending, err = s.ClaimDeviceCommand(ctx, claim)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE device_business_events SET expires_at=DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 1 SECOND) WHERE id=?`, pending.ID)
	if expiredRetry, created, err := s.BeginDeviceBusiness(ctx, mac, claim.RequestID, "voice_control", hash, ""); err != nil || created || expiredRetry.Status != "timeout" {
		t.Fatal("executing cleanup did not expire", expiredRetry, created, err)
	}
	dispatchACK.RequestID = claim.RequestID
	if _, err := s.RecordDeviceCommand(ctx, dispatchACK); !errors.Is(err, ErrDeviceBusinessConflict) {
		t.Fatal("late executed receipt resurrected timeout", err)
	}
	var charged, wallet int64
	exec(`INSERT INTO fin_wallet_accounts(tenant_id,balance_cents,status) VALUES(25,777,'active')`)
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(charged_beans),0) FROM device_business_events`).Scan(&charged); err != nil || charged != 0 {
		t.Fatal("beans charged", charged, err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=25`).Scan(&wallet); err != nil || wallet != 777 {
		t.Fatal("wallet changed", wallet, err)
	}
	name, err := s.NextDeviceName(ctx, 25)
	if err != nil || name != "小蓝搭子03" {
		t.Fatal("legacy sequence", name, err)
	}
	var stored string
	if err := s.db.QueryRowContext(ctx, `SELECT device_name FROM device_hardware_profiles WHERE device_id=1`).Scan(&stored); err != nil || stored != "小蓝直播助手02" {
		t.Fatal("legacy user name rewritten", stored, err)
	}
	provision, err := s.ProvisionDevice(ctx, mac, "test-secret")
	if err != nil || provision.DeviceName != "小蓝搭子02" || provision.RoomNumber != "external-915" {
		t.Fatal("safe device-only alias", provision, err)
	}
	exec(`INSERT INTO device_provisioning_events(device_id,tenant_id,event_code,detail) VALUES(1,25,'RENAMED','用户指定旧名称')`)
	provision, err = s.ProvisionDevice(ctx, mac, "test-secret")
	if err != nil || provision.DeviceName != "小蓝直播助手02" {
		t.Fatal("explicit rename overridden", provision, err)
	}
	// Current owner changes while recognition is in flight: old result must fail.
	transferred, _, err := s.BeginDeviceBusiness(ctx, mac, "request003", "voice_control", hash, "")
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE inv_devices SET current_customer_id=26 WHERE id=1`)
	exec(`UPDATE device_hardware_profiles SET claimed_tenant_id=26,device_name='转移设备' WHERE device_id=1`)
	if _, err = s.FinishDeviceVoice(ctx, mac, transferred, "秘密指令", "recognized", ""); !errors.Is(err, ErrDeviceClaimUnavailable) {
		t.Fatal("transferred voice returned", err)
	}
	if _, err = s.DeviceBusinessEvents(ctx, 25, 1, 0, 20); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("old owner accesses device", err)
	}
	// Concurrent duplicate request is created once only.
	var wg sync.WaitGroup
	createdCount := make(chan bool, 2)
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, created, err := s.BeginDeviceBusiness(ctx, "01:02:03:04:05:02", "concurrent1", "environment_snapshot", hash, "")
			createdCount <- created
			errCh <- err
		}()
	}
	wg.Wait()
	close(createdCount)
	close(errCh)
	n := 0
	for created := range createdCount {
		if created {
			n++
		}
	}
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n != 1 {
		t.Fatal("concurrent duplicate count", n)
	}
}
