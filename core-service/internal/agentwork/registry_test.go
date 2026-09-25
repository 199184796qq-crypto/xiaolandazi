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

	if _, err := registry.SetWithBase(3, StateWorking, 50); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Second)
	if _, err := registry.Set(3, StateStopped); err != nil {
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

func TestRegistryRejectsInvalidState(t *testing.T) {
	registry := New()
	if _, err := registry.Set(1, State("weird")); err == nil {
		t.Fatal("expected invalid state error")
	}
}
