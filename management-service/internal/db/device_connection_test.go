package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHardwareConnectionRejectsMissingOrStaleProof(t *testing.T) {
	now := time.Now().UTC()
	valid := HardwareConnectionInput{HardwareMAC: "1c:29:04:31:0e:b8", TenantID: 14, DeviceID: 7, ConnectionID: "connection-123", Phase: "heartbeat", ObservedAt: now}
	if err := ValidateHardwareConnection(valid, now); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*HardwareConnectionInput){
		func(i *HardwareConnectionInput) { i.ObservedAt = time.Time{} },
		func(i *HardwareConnectionInput) { i.ObservedAt = now.Add(-31 * time.Second) },
		func(i *HardwareConnectionInput) { i.ObservedAt = now.Add(2 * time.Second) },
		func(i *HardwareConnectionInput) { i.ConnectionID = "" },
		func(i *HardwareConnectionInput) { i.ConnectionID = "connection/other" },
		func(i *HardwareConnectionInput) { i.TenantID = 0 },
		func(i *HardwareConnectionInput) { i.Phase = "timer_tick" },
	} {
		input := valid
		change(&input)
		if err := ValidateHardwareConnection(input, now); err == nil {
			t.Fatal("unproven connection event accepted", input)
		}
	}
}

func TestHardwareConnectionMySQL(t *testing.T) {
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
		if strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_device_") || strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_runtime_") {
			exec(q)
		}
	}
	if err := s.MigrateDeviceProvisioning(ctx); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO mgmt_tenants(id,code,name,status) VALUES (25,'shutdown-owner','Owner','active'),(26,'shutdown-other','Other','active')`)
	exec(`INSERT INTO inv_devices(id,sn,sku_code,quality_status,lifecycle_status,current_customer_id) VALUES (1,'OFF-001','XZ-CAM','qualified','CUSTOMER_BOUND',25),(2,'OFF-002','XZ-CAM','qualified','ACTIVE',25)`)
	exec(`INSERT INTO device_hardware_profiles(device_id,hardware_mac,claimed_tenant_id) VALUES (1,'1c:29:04:31:0e:b8',25),(2,'1c:29:04:31:0e:b9',25)`)
	exec(`INSERT INTO live_device_room_bindings(tenant_id,device_id,room_id,binding_role,status) VALUES (25,1,25,'primary','active'),(25,2,25,'listener','active')`)
	observed := time.Now().UTC().Add(-10 * time.Second).Truncate(time.Millisecond)
	input := HardwareConnectionInput{HardwareMAC: "1c:29:04:31:0e:b8", TenantID: 25, DeviceID: 1, ConnectionID: "socket-old-123", Phase: "connect", ObservedAt: observed}
	apply := func(i HardwareConnectionInput, want bool) HardwareConnectionResult {
		t.Helper()
		got, err := s.ObserveHardwareConnection(ctx, i)
		if err != nil || got.Applied != want {
			t.Fatalf("phase=%s connection=%s: %+v %v", i.Phase, i.ConnectionID, got, err)
		}
		return got
	}
	got := apply(input, true)
	if got.TenantID != 25 || got.DeviceID != 1 || got.RoomID != 25 || got.BindingRole != "primary" {
		t.Fatal("incorrect scope", got)
	}
	input.Phase = "heartbeat"
	input.ObservedAt = observed.Add(time.Second)
	apply(input, true)
	var heartbeat time.Time
	if err := s.db.QueryRowContext(ctx, `SELECT last_heartbeat_at FROM live_device_runtime_state WHERE device_id=1`).Scan(&heartbeat); err != nil || !heartbeat.Equal(input.ObservedAt) {
		t.Fatal("heartbeat replaced by server timer", heartbeat, err)
	}
	newSocket := input
	newSocket.Phase = "connect"
	newSocket.ConnectionID = "socket-new-456"
	newSocket.ObservedAt = observed.Add(2 * time.Second)
	apply(newSocket, true)
	for _, phase := range []string{"shutdown", "disconnect", "heartbeat"} {
		old := input
		old.Phase = phase
		apply(old, false)
	}
	foreign := newSocket
	foreign.TenantID = 26
	if _, err := s.ObserveHardwareConnection(ctx, foreign); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign tenant accepted", err)
	}
	newSocket.Phase = "shutdown"
	newSocket.ObservedAt = observed.Add(3 * time.Second)
	apply(newSocket, true)
	newSocket.Phase = "disconnect"
	apply(newSocket, false)
	newSocket.Phase = "heartbeat"
	apply(newSocket, false)
	var status, reason string
	if err := s.db.QueryRowContext(ctx, `SELECT connection_status,stop_reason FROM live_device_runtime_state WHERE device_id=1`).Scan(&status, &reason); err != nil || status != "offline" || reason != "device_shutdown" {
		t.Fatal("shutdown overwritten", status, reason, err)
	}
	listener := input
	listener.DeviceID = 2
	listener.HardwareMAC = "1c:29:04:31:0e:b9"
	listener.ConnectionID = "listener-123"
	listener.Phase = "connect"
	apply(listener, true)
	listener.Phase = "shutdown"
	if got := apply(listener, true); got.BindingRole != "listener" {
		t.Fatal("listener became primary", got)
	}
	newSocket.Phase = "connect"
	newSocket.ConnectionID = "socket-third-789"
	newSocket.ObservedAt = time.Now().UTC().Truncate(time.Millisecond)
	apply(newSocket, true)
	newSocket.Phase = "heartbeat"
	newSocket.ObservedAt = newSocket.ObservedAt.Add(-time.Second)
	apply(newSocket, true)
	if err := s.db.QueryRowContext(ctx, `SELECT last_heartbeat_at FROM live_device_runtime_state WHERE device_id=1`).Scan(&heartbeat); err != nil || !heartbeat.Equal(newSocket.ObservedAt.Add(time.Second)) {
		t.Fatal("out-of-order proof regressed heartbeat", heartbeat, err)
	}
}
