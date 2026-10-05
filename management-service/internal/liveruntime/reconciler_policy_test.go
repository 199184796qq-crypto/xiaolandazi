package liveruntime

import (
	"context"
	"sync"
	"testing"
	"time"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func TestAuthoritativeCoreStopReasonRejectsEmptyReason(t *testing.T) {
	for _, state := range []string{"stopped", "stopping"} {
		reason, ok := authoritativeCoreStopReason(coreAgentRuntimeState{State: state})
		if ok || reason != "" {
			t.Fatalf("state=%q reason=%q ok=%v; empty reason is not authoritative", state, reason, ok)
		}
	}
}

func TestAuthoritativeCoreStopReasonAcceptsExplicitReason(t *testing.T) {
	reason, ok := authoritativeCoreStopReason(coreAgentRuntimeState{
		State:      "stopped",
		StopReason: " quota_exhausted ",
	})
	if !ok || reason != "quota_exhausted" {
		t.Fatalf("reason=%q ok=%v", reason, ok)
	}
}

func TestAuthoritativeCoreStopReasonRejectsWorkingState(t *testing.T) {
	reason, ok := authoritativeCoreStopReason(coreAgentRuntimeState{
		State:      "working",
		StopReason: "manual",
	})
	if ok || reason != "" {
		t.Fatalf("reason=%q ok=%v; working state must not finalize a session", reason, ok)
	}
}

func TestCoreRestartReasonClassification(t *testing.T) {
	for _, value := range []string{"core_restart", " core_runtime_reset ", "CORE_RESTART"} {
		if !isCoreRestartReason(value) {
			t.Fatalf("%q should be classified as a Core restart reason", value)
		}
	}
	for _, value := range []string{"manual", "quota_exhausted", "live_finished", ""} {
		if isCoreRestartReason(value) {
			t.Fatalf("%q must not be classified as a Core restart reason", value)
		}
	}
}

func TestRecoverableCoreRestartSessionRequiresStoppedRestartState(t *testing.T) {
	if !isRecoverableCoreRestartSession(model.LiveRuntimeSession{Status: "stopped", StopReason: "core_restart"}) {
		t.Fatal("stopped core_restart session should be recoverable")
	}
	if !isRecoverableCoreRestartSession(model.LiveRuntimeSession{Status: "stopped", StopReason: "core_runtime_reset"}) {
		t.Fatal("stopped core_runtime_reset session should be recoverable")
	}
	for _, session := range []model.LiveRuntimeSession{
		{Status: "running", StopReason: "core_restart"},
		{Status: "stopped", StopReason: "manual"},
		{Status: "paused", StopReason: "core_restart"},
	} {
		if isRecoverableCoreRestartSession(session) {
			t.Fatalf("unexpected recoverable session: %#v", session)
		}
	}
}

func TestWaitReconcileWorkersReturnsOnContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	started := time.Now()
	if waitReconcileWorkers(ctx, &wg) {
		t.Fatal("blocked worker unexpectedly reported completion")
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("deadline did not release scheduler promptly: %s", elapsed)
	}
	wg.Done()
}

func TestWaitReconcileWorkersReturnsWhenWorkersFinish(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		time.Sleep(10 * time.Millisecond)
		wg.Done()
	}()
	if !waitReconcileWorkers(ctx, &wg) {
		t.Fatal("completed worker group was treated as timed out")
	}
}

func TestDeviceOfflineStopDeadlineRequiresSixtyContinuousSeconds(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 30, 0, 0, time.UTC)
	startedAt := now.Add(-10 * time.Minute)

	for _, test := range []struct {
		name     string
		presence appdb.LiveDevicePresence
		wantDue  bool
	}{
		{
			name: "recent online heartbeat",
			presence: appdb.LiveDevicePresence{
				Exists: true, ConnectionStatus: "online", LastHeartbeatAt: timePtr(now.Add(-59 * time.Second)),
			},
			wantDue: false,
		},
		{
			name: "online heartbeat expired",
			presence: appdb.LiveDevicePresence{
				Exists: true, ConnectionStatus: "online", LastHeartbeatAt: timePtr(now.Add(-60 * time.Second)),
			},
			wantDue: true,
		},
		{
			name: "explicit offline still in grace",
			presence: appdb.LiveDevicePresence{
				Exists: true, ConnectionStatus: "offline", ConnectionUpdatedAt: now.Add(-30 * time.Second),
			},
			wantDue: false,
		},
		{
			name: "explicit offline grace expired",
			presence: appdb.LiveDevicePresence{
				Exists: true, ConnectionStatus: "offline", ConnectionUpdatedAt: now.Add(-61 * time.Second),
			},
			wantDue: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, due := deviceOfflineStopDeadline(startedAt, test.presence, now)
			if due != test.wantDue {
				t.Fatalf("due=%v want %v", due, test.wantDue)
			}
		})
	}
}

func timePtr(value time.Time) *time.Time {
	return &value
}
