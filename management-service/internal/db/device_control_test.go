package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type deviceControlConnector struct{ c *deviceControlConn }

func (c deviceControlConnector) Connect(context.Context) (driver.Conn, error) { return c.c, nil }
func (deviceControlConnector) Driver() driver.Driver                          { return deviceControlDriver{} }

type deviceControlDriver struct{}

func (deviceControlDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("connector only")
}

type deviceControlRows struct {
	v [][]driver.Value
	n int
}

func (r *deviceControlRows) Columns() []string { return make([]string, r.n) }
func (*deviceControlRows) Close() error        { return nil }
func (r *deviceControlRows) Next(v []driver.Value) error {
	if len(r.v) == 0 {
		return io.EOF
	}
	copy(v, r.v[0])
	r.v = r.v[1:]
	return nil
}
func deviceControlRow(v ...driver.Value) driver.Rows {
	return &deviceControlRows{v: [][]driver.Value{v}, n: len(v)}
}
func deviceControlNoRows() driver.Rows { return &deviceControlRows{} }

var deviceControlQuotaReached = errors.New("quota reached after connection gate")

type deviceControlConn struct {
	status, work, reason, metadata string
	heartbeat                      time.Time
	writes, commits                int
	pausedDevice                   bool
}

func (*deviceControlConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*deviceControlConn) Close() error                { return nil }
func (c *deviceControlConn) Begin() (driver.Tx, error) { return deviceControlTx{c}, nil }
func (c *deviceControlConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}

type deviceControlTx struct{ c *deviceControlConn }

func (t deviceControlTx) Commit() error { t.c.commits++; return nil }
func (deviceControlTx) Rollback() error { return nil }
func (c *deviceControlConn) QueryContext(_ context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	q = strings.Join(strings.Fields(q), " ")
	switch {
	case strings.Contains(q, "SELECT b.device_id"):
		if len(a) != 4 || a[0].Value != int64(14) || a[1].Value != int64(26) || a[2].Value != int64(7) || a[3].Value != int64(14) {
			return deviceControlNoRows(), nil
		}
		if !strings.Contains(q, "FOR UPDATE") || !strings.Contains(q, "b.binding_role='primary'") {
			return nil, errors.New("control requires scoped primary binding lock")
		}
		if strings.Contains(q, "rs.metadata_json") {
			return deviceControlRow(int64(7), c.status, c.heartbeat, c.metadata), nil
		}
		return deviceControlRow(int64(7)), nil
	case strings.Contains(q, "SELECT connection_status, work_status, last_heartbeat_at, metadata_json"):
		if a[0].Value != int64(7) || a[1].Value != int64(14) || !strings.Contains(q, "FOR UPDATE") {
			return nil, errors.New("runtime lock scope")
		}
		return deviceControlRow(c.status, c.work, c.heartbeat, c.metadata), nil
	case strings.Contains(q, "FROM inv_devices d") && strings.Contains(q, "WHERE d.id=?"):
		if a[0].Value != int64(7) || a[1].Value != int64(14) {
			return deviceControlNoRows(), nil
		}
		if !strings.Contains(q, "$.gateway_connection_id") || !strings.Contains(q, "$.transport") {
			return nil, errors.New("read must discard historical unproven online state")
		}
		status := c.status
		meta := deviceConnectionMetadata(sql.NullString{String: c.metadata, Valid: true})
		if meta["transport"] != "xiaozhi_websocket_proven" {
			status = "offline"
		}
		return deviceControlRow(int64(7), "XL000007", "XZ-CAM", "ACTIVE", int64(14), int64(26), "primary", status, c.work, c.reason, c.heartbeat, "小蓝盒子", c.metadata), nil
	case strings.Contains(q, "SELECT id FROM mgmt_tenants"):
		return deviceControlRow(int64(14)), nil
	case strings.Contains(q, "SELECT p.device_id,p.claimed_tenant_id"):
		if a[1].Value != int64(7) || a[2].Value != int64(14) {
			return deviceControlNoRows(), nil
		}
		return deviceControlRow(int64(7), int64(14)), nil
	case strings.Contains(q, "SELECT room_id,binding_role"):
		return deviceControlRow(int64(26), "primary"), nil
	case strings.Contains(q, "SELECT metadata_json,connection_status,work_status,last_heartbeat_at"):
		return deviceControlRow(c.metadata, c.status, c.work, c.heartbeat), nil
	case strings.Contains(q, "SELECT id FROM core_rooms"):
		return deviceControlRow(int64(26)), nil
	case strings.Contains(q, "SELECT COUNT(*) FROM mgmt_room_deletions"):
		return deviceControlRow(int64(0)), nil
	case strings.Contains(q, "FROM live_runtime_sessions s") && strings.Contains(q, "s.status='paused'"):
		var id driver.Value
		if c.pausedDevice {
			id = int64(7)
		}
		now := time.Now().UTC()
		return deviceControlRow(int64(487), "same-run", int64(14), int64(26), id, "XL000007", "prod", "paused", "manual_pause", int64(28), int64(28), now, now, nil, int64(100), int64(1)), nil
	case strings.Contains(q, "FROM live_runtime_sessions"):
		return deviceControlNoRows(), nil
	default:
		return nil, deviceControlQuotaReached
	}
}
func (c *deviceControlConn) ExecContext(_ context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	c.writes++
	switch {
	case strings.Contains(q, "UPDATE live_device_runtime_state"):
		for _, forbidden := range []string{"connection_status", "last_heartbeat_at", "stop_reason", "current_room_id"} {
			if strings.Contains(q, forbidden) {
				return nil, errors.New("browser changed transport presence: " + forbidden)
			}
		}
		if a[3].Value != int64(7) || a[4].Value != int64(14) {
			return nil, errors.New("write scope")
		}
		c.work = a[0].Value.(string)
		c.metadata = string(a[1].Value.([]byte))
	case strings.Contains(q, "INSERT INTO live_device_runtime_state"):
		c.status, c.work, c.reason = a[3].Value.(string), a[4].Value.(string), a[5].Value.(string)
		c.heartbeat = a[6].Value.(time.Time)
		c.metadata = string(a[7].Value.([]byte))
	case strings.Contains(q, "INSERT INTO live_runtime_events"):
	default:
		return nil, errors.New("unexpected write")
	}
	return driver.RowsAffected(1), nil
}
func newDeviceControlStore(t *testing.T) (*Store, *deviceControlConn) {
	t.Helper()
	c := &deviceControlConn{status: "online", work: "idle", heartbeat: time.Now().UTC().Add(-3 * time.Second).Truncate(time.Millisecond), metadata: `{"transport":"xiaozhi_websocket_proven","gateway_connection_id":"current-socket-123","last_transport_phase":"heartbeat","firmware_version":"2.2.7"}`}
	database := sql.OpenDB(deviceControlConnector{c})
	t.Cleanup(func() { _ = database.Close() })
	return &Store{db: database}, c
}

func TestDeviceControlsPreserveActualHeartbeatAndCurrentSocket(t *testing.T) {
	s, c := newDeviceControlStore(t)
	before := c.heartbeat
	for _, action := range []string{"connect", "pause", "resume", "disconnect", "connect"} {
		got, err := s.ControlLiveDevice(context.Background(), 14, 28, 7, 26, action)
		if err != nil {
			t.Fatal(action, err)
		}
		var meta map[string]any
		_ = json.Unmarshal([]byte(c.metadata), &meta)
		if !c.heartbeat.Equal(before) || meta["gateway_connection_id"] != "current-socket-123" || meta["transport"] != "xiaozhi_websocket_proven" || meta["last_transport_phase"] != "heartbeat" || meta["firmware_version"] != "2.2.7" || got.ConnectionStatus != "online" {
			t.Fatal(action, got, c)
		}
		want := "playing"
		if action == "pause" {
			want = "paused"
		}
		if action == "disconnect" {
			want = "disconnected"
		}
		if meta["audio_control"] != want {
			t.Fatal(action, meta)
		}
		if action == "disconnect" && got.DisplayStatus != "声音已停用" {
			t.Fatal(got)
		}
	}
}

func TestDeviceControlsRejectFakeStaleMissingOrWrongSocketProof(t *testing.T) {
	for _, mutate := range []func(*deviceControlConn){
		func(c *deviceControlConn) { c.metadata = `{"transport":"development_control","handshake":true}` },
		func(c *deviceControlConn) { c.heartbeat = time.Now().Add(-21 * time.Second) },
		func(c *deviceControlConn) {
			c.metadata = `{"transport":"xiaozhi_websocket_proven","gateway_connection_id":"","last_transport_phase":"heartbeat"}`
		},
		func(c *deviceControlConn) {
			c.metadata = `{"transport":"xiaozhi_websocket_proven","gateway_connection_id":"current-socket-123","last_transport_phase":"disconnect"}`
		},
		func(c *deviceControlConn) { c.status = "offline" },
	} {
		for _, action := range []string{"connect", "pause", "resume"} {
			s, c := newDeviceControlStore(t)
			mutate(c)
			if _, err := s.ControlLiveDevice(context.Background(), 14, 28, 7, 26, action); !errors.Is(err, ErrLiveDeviceOffline) || c.writes != 0 {
				t.Fatal(action, err, c)
			}
		}
	}
}

func TestBrowserHeartbeatCannotRefreshOrReplacePresence(t *testing.T) {
	s, c := newDeviceControlStore(t)
	before, metadata := c.heartbeat, c.metadata
	room := int64(26)
	if _, err := s.HeartbeatLiveDevice(context.Background(), 14, 7, &room, map[string]any{"transport": "development_control", "gateway_connection_id": "forged-socket"}); err != nil {
		t.Fatal(err)
	}
	if !before.Equal(c.heartbeat) || metadata != c.metadata || c.writes != 0 || c.commits != 0 {
		t.Fatal("browser heartbeat wrote presence", c)
	}
	room = 29
	if _, err := s.HeartbeatLiveDevice(context.Background(), 14, 7, &room, nil); !errors.Is(err, ErrLiveDeviceNotBound) {
		t.Fatal(err)
	}
	if _, err := s.HeartbeatLiveDevice(context.Background(), 15, 7, nil, nil); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
}

func TestDeviceControlTenantAndRoomCannotCrossScope(t *testing.T) {
	s, c := newDeviceControlStore(t)
	for _, scope := range [][3]int64{{15, 26, 7}, {14, 29, 7}, {14, 26, 8}} {
		if _, err := s.ControlLiveDevice(context.Background(), scope[0], 28, scope[2], scope[1], "connect"); !errors.Is(err, ErrLiveDeviceNotBound) || c.writes != 0 {
			t.Fatal(scope, err, c)
		}
	}
}

func TestAudioPreferenceSurvivesDisconnectAndNewSocket(t *testing.T) {
	for _, action := range []string{"pause", "disconnect"} {
		s, c := newDeviceControlStore(t)
		if _, err := s.ControlLiveDevice(context.Background(), 14, 28, 7, 26, action); err != nil {
			t.Fatal(err)
		}
		input := HardwareConnectionInput{HardwareMAC: "1c:29:04:31:0e:b8", TenantID: 14, DeviceID: 7, ConnectionID: "current-socket-123", ObservedAt: time.Now().UTC(), Phase: "disconnect"}
		if got, err := s.ObserveHardwareConnection(context.Background(), input); err != nil || !got.Applied {
			t.Fatal(got, err)
		}
		input.ConnectionID = "replacement-socket-456"
		input.Phase = "connect"
		if got, err := s.ObserveHardwareConnection(context.Background(), input); err != nil || !got.Applied {
			t.Fatal(got, err)
		}
		want := "paused"
		if action == "disconnect" {
			want = "disconnected"
		}
		if c.work != want || c.status != "online" {
			t.Fatal(action, c)
		}
		input.Phase = "heartbeat"
		input.ConnectionID = "current-socket-123"
		if got, err := s.ObserveHardwareConnection(context.Background(), input); err != nil || got.Applied {
			t.Fatal("old socket regained authority", got, err)
		}
	}
}

func TestStartAndResumeRejectHistoricalFakeOnlineBeforeBilling(t *testing.T) {
	for _, resume := range []bool{false, true} {
		s, c := newDeviceControlStore(t)
		c.metadata = `{"transport":"development_control","handshake":true}`
		c.pausedDevice = true
		var err error
		if resume {
			_, err = s.ResumeLiveRuntimeSessionIfCurrent(context.Background(), 14, 26, 28, time.Now().UTC(), 487, 1)
		} else {
			id := int64(7)
			_, err = s.StartLiveRuntimeSession(context.Background(), 14, 26, 28, &id, "prod", time.Now().UTC())
		}
		if !errors.Is(err, ErrLiveDeviceOffline) || c.writes != 0 {
			t.Fatal(resume, err, c)
		}
	}
}

func TestNoHardwareStartAndResumeKeepExistingBillingPath(t *testing.T) {
	for _, resume := range []bool{false, true} {
		s, c := newDeviceControlStore(t)
		var err error
		if resume {
			_, err = s.ResumeLiveRuntimeSessionIfCurrent(context.Background(), 14, 26, 28, time.Now().UTC(), 487, 1)
		} else {
			_, err = s.StartLiveRuntimeSession(context.Background(), 14, 26, 28, nil, "dev", time.Now().UTC())
		}
		if !errors.Is(err, deviceControlQuotaReached) || c.writes != 0 {
			t.Fatal(resume, err, c)
		}
	}
}

func TestRuntimeConnectionFreshnessUsesTimeAfterLockRatherThanRequestEntry(t *testing.T) {
	for _, resume := range []bool{false, true} {
		for _, fresh := range []bool{false, true} {
			s, c := newDeviceControlStore(t)
			c.pausedDevice = true
			entry := time.Now().UTC().Add(-2 * time.Minute)
			if !fresh {
				c.heartbeat = entry.Add(-3 * time.Second)
			}
			var err error
			if resume {
				_, err = s.ResumeLiveRuntimeSessionIfCurrent(context.Background(), 14, 26, 28, entry, 487, 1)
			} else {
				id := int64(7)
				_, err = s.StartLiveRuntimeSession(context.Background(), 14, 26, 28, &id, "prod", entry)
			}
			want := ErrLiveDeviceOffline
			if fresh {
				want = deviceControlQuotaReached
			}
			if !errors.Is(err, want) || c.writes != 0 {
				t.Fatal("request entry time corrupted lock-time liveness", resume, fresh, err, c)
			}
		}
	}
}

func TestLegacyAudioIntentSurvivesUpgradeAndUsesSameReadAndReconnectPolicy(t *testing.T) {
	for _, tc := range []struct{ action, explicit, previous, wantControl, wantWork string }{
		{"pause", "", "idle", "paused", "paused"},
		{"disconnect", "", "idle", "disconnected", "disconnected"},
		{"connect", "", "paused", "playing", "idle"},
		{"resume", "", "disconnected", "playing", "idle"},
		{"pause", "playing", "paused", "playing", "idle"},
		{"resume", "paused", "idle", "paused", "paused"},
		{"connect", "disconnected", "working", "disconnected", "disconnected"},
	} {
		s, c := newDeviceControlStore(t)
		meta := deviceConnectionMetadata(sql.NullString{String: c.metadata, Valid: true})
		meta["control_action"] = tc.action
		if tc.explicit != "" {
			meta["audio_control"] = tc.explicit
		}
		raw, _ := json.Marshal(meta)
		c.metadata = string(raw)
		c.work = tc.previous
		if got := deviceAudioControl(meta); got != tc.wantControl {
			t.Fatal(tc, got)
		}
		got, err := s.GetLiveDevice(context.Background(), 14, 7)
		if err != nil || got.WorkStatus != tc.wantWork || got.ConnectionStatus != "online" {
			t.Fatal("read lost intent", tc, got, err)
		}
		input := HardwareConnectionInput{HardwareMAC: "1c:29:04:31:0e:b8", TenantID: 14, DeviceID: 7, ConnectionID: "upgrade-socket-789", ObservedAt: time.Now().UTC(), Phase: "connect"}
		if got, err := s.ObserveHardwareConnection(context.Background(), input); err != nil || !got.Applied {
			t.Fatal(got, err)
		}
		if c.work != tc.wantWork {
			t.Fatal("reconnect differs from read", tc, c.work)
		}
	}
}

// Optional real MySQL coverage uses the existing explicit opt-in fixture,
// creating and dropping a fresh schema instead of touching customer rows.
func TestDeviceControlPresenceMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	exec := func(q string, a ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, a...); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range strings.Split(strings.ReplaceAll(inventorySchema, "\r\n", "\n"), "\n-- +statement\n") {
		exec(strings.TrimSpace(raw))
	}
	for _, raw := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		q := strings.TrimSpace(raw)
		if strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_device_") || strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_runtime_") {
			exec(q)
		}
	}
	if err := s.MigrateDeviceProvisioning(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateRoomDeletions(ctx); err != nil {
		t.Fatal(err)
	}
	exec(`CREATE TABLE core_rooms(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,name VARCHAR(128) NOT NULL,external_room_id VARCHAR(128) NOT NULL)`)
	exec(`INSERT INTO mgmt_tenants(id,code,name,status) VALUES(14,'control-owner','Owner','active'),(15,'control-other','Other','active')`)
	exec(`INSERT INTO core_rooms(id,tenant_id,name,external_room_id) VALUES(26,14,'控制测试','room26')`)
	exec(`INSERT INTO inv_devices(id,sn,sku_code,quality_status,lifecycle_status,current_customer_id) VALUES(7,'CONTROL-007','XZ-CAM','qualified','ACTIVE',14)`)
	exec(`INSERT INTO device_hardware_profiles(device_id,hardware_mac,claimed_tenant_id,device_name) VALUES(7,'1c:29:04:31:0e:b8',14,'测试盒子')`)
	exec(`INSERT INTO live_device_room_bindings(tenant_id,device_id,room_id,binding_role,status) VALUES(14,7,26,'primary','active')`)
	now := time.Now().UTC().Truncate(time.Millisecond)
	exec(`INSERT INTO live_device_runtime_state(device_id,tenant_id,current_room_id,connection_status,work_status,last_heartbeat_at,metadata_json) VALUES(7,14,26,'online','working',?,JSON_OBJECT('transport','development_control','handshake',true))`, now)
	device, err := s.GetLiveDevice(ctx, 14, 7)
	if err != nil || device.ConnectionStatus != "offline" {
		t.Fatal("historical false online still visible", device, err)
	}
	list, err := s.ListLiveDevices(ctx, 14)
	if err != nil || len(list) != 1 || list[0].ConnectionStatus != "offline" {
		t.Fatal(list, err)
	}
	rooms, err := s.ListPrimaryRoomDeviceOnline(ctx, []int64{14})
	if err != nil || rooms[14][26] {
		t.Fatal(rooms, err)
	}
	id := int64(7)
	if _, err := s.StartLiveRuntimeSession(ctx, 14, 26, 101, &id, "prod", now); !errors.Is(err, ErrLiveDeviceOffline) {
		t.Fatal("fake heartbeat admitted a paid runtime", err)
	}
	input := HardwareConnectionInput{HardwareMAC: "1c:29:04:31:0e:b8", TenantID: 14, DeviceID: 7, ConnectionID: "real-current-socket", Phase: "connect", ObservedAt: now}
	if got, err := s.ObserveHardwareConnection(ctx, input); err != nil || !got.Applied {
		t.Fatal(got, err)
	}
	for _, action := range []string{"connect", "pause", "resume", "disconnect"} {
		if _, err := s.ControlLiveDevice(ctx, 14, 101, 7, 26, action); err != nil {
			t.Fatal(action, err)
		}
		var heartbeat time.Time
		var raw string
		if err := s.db.QueryRowContext(ctx, `SELECT last_heartbeat_at,metadata_json FROM live_device_runtime_state WHERE device_id=7`).Scan(&heartbeat, &raw); err != nil || !heartbeat.Equal(now) {
			t.Fatal("control refreshed real heartbeat", heartbeat, err)
		}
		meta := deviceConnectionMetadata(sql.NullString{String: raw, Valid: true})
		if meta["gateway_connection_id"] != input.ConnectionID || meta["transport"] != "xiaozhi_websocket_proven" {
			t.Fatal("control replaced socket lease", meta)
		}
		provision, err := s.ProvisionDevice(ctx, input.HardwareMAC, "test-secret")
		if err != nil || provision.AudioControl != deviceAudioControl(meta) || provision.State != "bound" {
			t.Fatal(provision, err)
		}
	}
	input.Phase = "disconnect"
	if _, err := s.ObserveHardwareConnection(ctx, input); err != nil {
		t.Fatal(err)
	}
	input.Phase = "connect"
	input.ConnectionID = "real-replacement-socket"
	if _, err := s.ObserveHardwareConnection(ctx, input); err != nil {
		t.Fatal(err)
	}
	device, err = s.GetBoundLiveDeviceByRoom(ctx, 14, 26)
	if err != nil || device.ConnectionStatus != "online" || device.WorkStatus != "disconnected" {
		t.Fatal("reconnect lost audio preference", device, err)
	}
	wrongRoom := int64(27)
	if _, err := s.HeartbeatLiveDevice(ctx, 14, 7, &wrongRoom, nil); !errors.Is(err, ErrLiveDeviceNotBound) {
		t.Fatal(err)
	}
	if _, err := s.ControlLiveDevice(ctx, 15, 101, 7, 26, "connect"); !errors.Is(err, ErrLiveDeviceNotBound) {
		t.Fatal(err)
	}
}
