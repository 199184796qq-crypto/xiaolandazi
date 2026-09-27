package agentwork

import (
	"testing"
	"time"
)

func TestRegistryDefaultsStoppedAndTracksState(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })

	if got := registry.Get(7); got.State != StateStopped {
		t.Fatalf("default state=%q want stopped", got.State)
	}
	if got := registry.Get(7); got.Mode != ModeControl {
		t.Fatalf("default mode=%q want control", got.Mode)
	}
	if registry.IsWorking(7) {
		t.Fatal("default room must not be working")
	}

	if _, err := registry.GrantLease(7, 60); err != nil {
		t.Fatal(err)
	}
	snapshot, err := registry.Set(7, StateWorking)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != StateWorking || !registry.IsWorking(7) {
		t.Fatalf("working state not stored: %#v", snapshot)
	}

	now = now.Add(1500 * time.Millisecond)
	snapshot = registry.Get(7)
	if snapshot.WorkingSeconds != 1 {
		t.Fatalf("working seconds=%d want 1", snapshot.WorkingSeconds)
	}

	now = now.Add(500 * time.Millisecond)
	snapshot, err = registry.Set(7, StatePaused)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != StatePaused || registry.IsWorking(7) {
		t.Fatalf("paused state mismatch: %#v", snapshot)
	}
	if snapshot.WorkingSeconds != 2 {
		t.Fatalf("paused working seconds=%d want 2", snapshot.WorkingSeconds)
	}

	now = now.Add(30 * time.Second)
	snapshot = registry.Get(7)
	if snapshot.WorkingSeconds != 2 {
		t.Fatalf("paused meter advanced: %d", snapshot.WorkingSeconds)
	}

	if _, err := registry.GrantLease(7, 60); err != nil {
		t.Fatal(err)
	}
	snapshot, err = registry.Set(7, StateWorking)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Second)
	snapshot = registry.Get(7)
	if snapshot.WorkingSeconds != 5 {
		t.Fatalf("resumed working seconds=%d want 5", snapshot.WorkingSeconds)
	}
}

func TestRegistrySetWithBaseResumesDurableMeterAfterRestart(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })

	if _, err := registry.GrantLease(9, 60); err != nil {
		t.Fatal(err)
	}
	snapshot, err := registry.SetWithBase(9, StateWorking, 123)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.WorkingSeconds != 123 {
		t.Fatalf("base working seconds=%d want 123", snapshot.WorkingSeconds)
	}

	now = now.Add(7 * time.Second)
	snapshot = registry.Get(9)
	if snapshot.WorkingSeconds != 130 {
		t.Fatalf("working seconds=%d want 130", snapshot.WorkingSeconds)
	}

	snapshot, err = registry.SetWithBase(9, StateWorking, 123)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.WorkingSeconds != 130 {
		t.Fatalf("repeated sync reset meter: %d", snapshot.WorkingSeconds)
	}

	snapshot, err = registry.SetWithBase(9, StateWorking, 140)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.WorkingSeconds != 140 {
		t.Fatalf("durable floor not applied: %d", snapshot.WorkingSeconds)
	}
}

func TestRegistryStoppedToWorkingStartsNewSessionFromBase(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })

	if _, err := registry.GrantLease(3, 60); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetWithBase(3, StateWorking, 50); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Second)
	if _, err := registry.Set(3, StateStopped); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.GrantLease(3, 60); err != nil {
		t.Fatal(err)
	}
	snapshot, err := registry.SetWithBase(3, StateWorking, 0)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.WorkingSeconds != 0 {
		t.Fatalf("new session should reset meter, got %d", snapshot.WorkingSeconds)
	}
}

func TestRegistryTracksRoomModeWithoutResettingWorkMeter(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })
	if _, err := registry.GrantLease(4, 60); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetWithBase(4, StateWorking, 10); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)
	snapshot, err := registry.SetMode(4, ModeAnchor)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Mode != ModeAnchor || snapshot.WorkingSeconds != 15 {
		t.Fatalf("mode change corrupted meter: %#v", snapshot)
	}
	now = now.Add(3 * time.Second)
	snapshot = registry.Get(4)
	if snapshot.WorkingSeconds != 18 {
		t.Fatalf("meter stopped after mode change: %#v", snapshot)
	}
	if _, err := registry.SetMode(4, Mode("bad")); err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func TestRegistryWorkingRequiresLeaseAndStopsWhenLeaseExpires(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })
	if _, err := registry.Set(11, StateWorking); err == nil {
		t.Fatal("working must require paid lease")
	}
	if _, err := registry.GrantLease(11, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Set(11, StateWorking); err != nil {
		t.Fatal(err)
	}
	now = now.Add(4 * time.Second)
	snapshot := registry.Get(11)
	if snapshot.State != StateStopped {
		t.Fatalf("expired lease state=%q want stopped", snapshot.State)
	}
	if snapshot.WorkingSeconds != 3 {
		t.Fatalf("expired lease working seconds=%d want 3", snapshot.WorkingSeconds)
	}
	if snapshot.LeaseRemainingSeconds != 0 {
		t.Fatalf("expired lease remaining=%d want 0", snapshot.LeaseRemainingSeconds)
	}
}

func TestRegistryStopsOrphanWorkingStateWithoutLease(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })
	registry.rooms[12] = &roomState{
		roomID:       12,
		state:        StateWorking,
		mode:         ModeControl,
		workingSince: now,
		updatedAt:    now,
	}

	now = now.Add(2 * time.Second)
	snapshot := registry.Get(12)
	if snapshot.State != StateStopped {
		t.Fatalf("orphan working state=%q want stopped", snapshot.State)
	}
	if snapshot.LeaseRemainingSeconds != 0 {
		t.Fatalf("orphan lease remaining=%d want 0", snapshot.LeaseRemainingSeconds)
	}
}

func TestRegistrySetPlanKeepsWorkingLeaseAlive(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })
	if _, err := registry.GrantLease(13, 60); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Set(13, StateWorking); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Second)
	snapshot, err := registry.SetPlan(13, 2, "方案1")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != StateWorking {
		t.Fatalf("after plan sync state=%q want working", snapshot.State)
	}
	if snapshot.WorkingSince == nil {
		t.Fatal("working plan sync must restore working_since")
	}
	if snapshot.LeaseRemainingSeconds == 0 {
		t.Fatal("working plan sync must preserve lease")
	}
	now = now.Add(2 * time.Second)
	snapshot = registry.Get(13)
	if snapshot.State != StateWorking {
		t.Fatalf("next get state=%q want working", snapshot.State)
	}
	if snapshot.WorkingSeconds < 5 {
		t.Fatalf("working seconds=%d want at least 5", snapshot.WorkingSeconds)
	}
}

func TestRegistryRejectsInvalidState(t *testing.T) {
	registry := New()
	if _, err := registry.Set(1, State("weird")); err == nil {
		t.Fatal("expected invalid state error")
	}
}
