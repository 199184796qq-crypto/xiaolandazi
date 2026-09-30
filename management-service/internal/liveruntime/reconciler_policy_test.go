package liveruntime

import "testing"

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
