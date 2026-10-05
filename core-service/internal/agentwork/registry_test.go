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
	snapshot, err = registry.StopAgent(7, StopReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != StateStopped || registry.IsWorking(7) {
		t.Fatalf("stopped state mismatch: %#v", snapshot)
	}
	if snapshot.WorkingSeconds != 2 {
		t.Fatalf("stopped working seconds=%d want 2", snapshot.WorkingSeconds)
	}

	now = now.Add(30 * time.Second)
	snapshot = registry.Get(7)
	if snapshot.WorkingSeconds != 2 {
		t.Fatalf("stopped meter advanced: %d", snapshot.WorkingSeconds)
	}

	if _, err := registry.GrantLease(7, 60); err != nil {
		t.Fatal(err)
	}
	snapshot, err = registry.StartAgent(7, snapshot.WorkingSeconds)
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

func TestRegistryWorkingDoesNotExpireWithoutLease(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })
	if _, err := registry.Set(11, StateWorking); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	snapshot := registry.Get(11)
	if snapshot.State != StateWorking {
		t.Fatalf("runtime state=%q want working", snapshot.State)
	}
	if snapshot.WorkingSeconds != 120 {
		t.Fatalf("working seconds=%d want 120", snapshot.WorkingSeconds)
	}
	if snapshot.LeaseRemainingSeconds != 0 || snapshot.LeaseRenewalDue || snapshot.LeaseUntil != nil {
		t.Fatalf("lease compatibility fields must stay empty: %#v", snapshot)
	}
}

func TestRegistryOrphanWorkingStateWithoutLeaseKeepsAccruing(t *testing.T) {
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
	if snapshot.State != StateWorking {
		t.Fatalf("working state=%q want working", snapshot.State)
	}
	if snapshot.WorkingSeconds != 2 {
		t.Fatalf("working seconds=%d want 2", snapshot.WorkingSeconds)
	}
}

func TestRegistrySetPlanKeepsWorkingRuntimeAlive(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })
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
	if snapshot.LeaseRemainingSeconds != 0 || snapshot.LeaseRenewalDue {
		t.Fatalf("plan sync unexpectedly exposed lease state: %#v", snapshot)
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

func TestRegistryHotReloadKeepsWorkingPlanAndMeter(t *testing.T) {
	now := time.Date(2026, 9, 29, 11, 50, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })
	if _, err := registry.SetPlan(31, 9, "已生效方案"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Set(31, StateWorking); err != nil {
		t.Fatal(err)
	}
	now = now.Add(7 * time.Second)
	before := registry.Get(31)

	hot, err := registry.TouchHotReload(31, []string{"facts", "interaction", "facts", "style"})
	if err != nil {
		t.Fatal(err)
	}
	if hot.State != StateWorking || hot.PlanID != 9 || hot.PlanName != "已生效方案" {
		t.Fatalf("hot reload changed active runtime identity: %#v", hot)
	}
	if hot.WorkingSeconds != before.WorkingSeconds {
		t.Fatalf("hot reload reset/advanced meter unexpectedly: before=%d after=%d", before.WorkingSeconds, hot.WorkingSeconds)
	}
	if hot.LeaseRemainingSeconds != 0 || hot.LeaseRenewalDue {
		t.Fatalf("hot reload unexpectedly exposed lease state: %#v", hot)
	}
	if hot.HotRevision != 1 || hot.HotUpdatedAt == nil {
		t.Fatalf("hot revision not recorded: %#v", hot)
	}
	if len(hot.HotModules) != 3 || hot.HotModules[0] != "facts" || hot.HotModules[1] != "interaction" || hot.HotModules[2] != "style" {
		t.Fatalf("hot modules not normalized/deduplicated: %#v", hot.HotModules)
	}

	now = now.Add(2 * time.Second)
	hot2, err := registry.TouchHotReload(31, []string{"addressing"})
	if err != nil {
		t.Fatal(err)
	}
	if hot2.HotRevision != 2 || hot2.State != StateWorking || hot2.WorkingSeconds < before.WorkingSeconds+2 {
		t.Fatalf("second hot reload corrupted runtime: %#v", hot2)
	}
}

func TestRegistryRejectsInvalidState(t *testing.T) {
	registry := New()
	if _, err := registry.Set(1, State("weird")); err == nil {
		t.Fatal("expected invalid state error")
	}
}

func TestRegistryTracksLifecycleTransitionsAndStopReason(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })

	starting, err := registry.SetWithReason(21, StateStarting, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if starting.State != StateStarting || starting.StopReason != "" {
		t.Fatalf("starting snapshot=%#v", starting)
	}
	working, err := registry.SetWithReason(21, StateWorking, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if working.State != StateWorking {
		t.Fatalf("working snapshot=%#v", working)
	}

	now = now.Add(4 * time.Second)
	running := registry.Get(21)
	if running.State != StateWorking || running.StopReason != "" {
		t.Fatalf("running snapshot=%#v", running)
	}
	if running.WorkingSeconds != 4 {
		t.Fatalf("working seconds=%d want 4", running.WorkingSeconds)
	}

	stopping, err := registry.SetWithReason(21, StateStopping, StopReasonManual, running.WorkingSeconds)
	if err != nil {
		t.Fatal(err)
	}
	if stopping.State != StateStopping || stopping.WorkingSince != nil || stopping.LeaseRemainingSeconds != 0 {
		t.Fatalf("stopping snapshot=%#v", stopping)
	}
	stopped, err := registry.SetWithReason(21, StateStopped, StopReasonManual, stopping.WorkingSeconds)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != StateStopped || stopped.StopReason != StopReasonManual {
		t.Fatalf("stopped snapshot=%#v", stopped)
	}
}

func TestNormalizeStopReasonAcceptsHistoricalValues(t *testing.T) {
	cases := map[StopReason]StopReason{
		"manual_stop":        StopReasonManual,
		"manual_pause":       StopReasonManualPause,
		"quota_unavailable":  StopReasonQuotaExhausted,
		"room_offline":       StopReasonLiveFinished,
		"device_offline":     StopReasonDeviceOffline,
		"core_runtime_reset": StopReasonCoreRestart,
		"core_start_failed":  StopReasonSystemError,
	}
	for input, want := range cases {
		if got := NormalizeStopReason(input); got != want {
			t.Fatalf("NormalizeStopReason(%q)=%q want %q", input, got, want)
		}
	}
}

func TestManualPauseStopPreservesModeAndPlan(t *testing.T) {
	now := time.Date(2026, 9, 28, 6, 20, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })
	if _, err := registry.SetMode(28, ModeAnchor); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetPlan(28, 901, "菜籽油直播间智能体方案"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.GrantLease(28, 60); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.StartAgent(28, 11); err != nil {
		t.Fatal(err)
	}
	now = now.Add(4 * time.Second)
	paused, err := registry.StopAgent(28, StopReasonManualPause)
	if err != nil {
		t.Fatal(err)
	}
	if paused.State != StateStopped || paused.StopReason != StopReasonManualPause {
		t.Fatalf("paused snapshot=%#v", paused)
	}
	if paused.Mode != ModeAnchor || paused.PlanID != 901 || paused.PlanName != "菜籽油直播间智能体方案" {
		t.Fatalf("pause lost anchor configuration: %#v", paused)
	}
}

func TestRegistryStartStopCommandsOwnLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 27, 5, 0, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })

	working, err := registry.StartAgent(31, 7)
	if err != nil {
		t.Fatal(err)
	}
	if working.State != StateWorking || working.WorkingSeconds != 7 {
		t.Fatalf("working snapshot=%#v", working)
	}

	now = now.Add(5 * time.Second)
	stopped, err := registry.StopAgent(31, StopReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != StateStopped || stopped.StopReason != StopReasonManual {
		t.Fatalf("stopped snapshot=%#v", stopped)
	}
	if stopped.LeaseRemainingSeconds != 0 {
		t.Fatalf("stopped lease remaining=%d want 0", stopped.LeaseRemainingSeconds)
	}
	if stopped.WorkingSeconds != 12 {
		t.Fatalf("stopped working seconds=%d want 12", stopped.WorkingSeconds)
	}
}

func TestRegistryStopIsIsolatedPerRoom(t *testing.T) {
	now := time.Date(2026, 9, 27, 5, 30, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })
	for _, roomID := range []int64{41, 42} {
		if _, err := registry.StartAgent(roomID, 0); err != nil {
			t.Fatal(err)
		}
	}

	now = now.Add(2 * time.Second)
	if _, err := registry.StopAgent(41, StopReasonManual); err != nil {
		t.Fatal(err)
	}
	first := registry.Get(41)
	second := registry.Get(42)
	if first.State != StateStopped || first.StopReason != StopReasonManual {
		t.Fatalf("room 41 snapshot=%#v", first)
	}
	if second.State != StateWorking {
		t.Fatalf("room 42 state=%q want working", second.State)
	}
	if second.WorkingSeconds != 2 {
		t.Fatalf("room 42 working seconds=%d want 2", second.WorkingSeconds)
	}
}

func TestRegistryLegacyLeaseGrantIsCompatibilityNoOp(t *testing.T) {
	now := time.Date(2026, 9, 27, 6, 0, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })
	if _, err := registry.StartAgent(51, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.GrantLease(51, 60); err != nil {
		t.Fatal(err)
	}
	now = now.Add(90 * time.Second)
	snapshot := registry.Get(51)
	if snapshot.State != StateWorking || snapshot.WorkingSeconds != 90 {
		t.Fatalf("legacy lease grant affected runtime: %#v", snapshot)
	}
	if snapshot.LeaseRemainingSeconds != 0 || snapshot.LeaseRenewalDue || snapshot.LeaseUntil != nil {
		t.Fatalf("legacy lease compatibility fields must remain empty: %#v", snapshot)
	}
}
