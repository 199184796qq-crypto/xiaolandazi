package liveruntime

import (
	"context"
	"sync"
	"testing"
	"time"
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
