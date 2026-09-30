package speechmission

import (
	"testing"
	"time"
)

func TestSweepExpiresStaleActiveMission(t *testing.T) {
	now := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	r := New()
	mission := r.Ensure(Mission{
		ID: "d-1", DecisionID: "d-1", TenantID: 7, RoomID: 15, State: StateDispatched,
	})
	if _, ok := r.Update(mission.ID, func(m *Mission) {
		m.StateChangedAt = now.Add(-4 * time.Minute)
		m.UpdatedAt = now.Add(-4 * time.Minute)
	}); !ok {
		t.Fatal("mission update failed")
	}

	expired, removed := r.Sweep(now, 3*time.Minute, 5*time.Minute)
	if removed != 0 {
		t.Fatalf("removed=%d want 0", removed)
	}
	if len(expired) != 1 || expired[0].ID != mission.ID || expired[0].State != StateExpired {
		t.Fatalf("unexpected expired missions: %#v", expired)
	}
	snapshot, ok := r.Snapshot(mission.ID)
	if !ok || snapshot.State != StateExpired {
		t.Fatalf("mission must remain briefly as expired for diagnostics: %#v", snapshot)
	}
}

func TestSweepPrunesTerminalMissionAfterRetention(t *testing.T) {
	now := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	r := New()
	mission := r.Ensure(Mission{
		ID: "d-2", DecisionID: "d-2", TenantID: 7, RoomID: 15, State: StateCompleted,
	})
	if _, ok := r.Update(mission.ID, func(m *Mission) {
		m.StateChangedAt = now.Add(-6 * time.Minute)
		m.UpdatedAt = now.Add(-6 * time.Minute)
	}); !ok {
		t.Fatal("mission update failed")
	}

	expired, removed := r.Sweep(now, 3*time.Minute, 5*time.Minute)
	if len(expired) != 0 {
		t.Fatalf("terminal mission must not be re-expired: %#v", expired)
	}
	if removed != 1 {
		t.Fatalf("removed=%d want 1", removed)
	}
	if _, ok := r.Snapshot(mission.ID); ok {
		t.Fatal("terminal mission should be removed from hot memory")
	}
}

func TestEnsureRestartsTerminalMissionForSameDecision(t *testing.T) {
	r := New()
	first := r.Ensure(Mission{
		ID: "d-3", DecisionID: "d-3", TenantID: 7, RoomID: 15, RuntimeSessionID: 1, State: StateCreated,
	})
	if _, ok := r.Transition(first.ID, StateFailed, "failed", "test"); !ok {
		t.Fatal("transition failed")
	}

	retried := r.Ensure(Mission{
		ID: "d-3", DecisionID: "d-3", TenantID: 7, RoomID: 15, RuntimeSessionID: 2, State: StateCreated,
	})
	if retried.State != StateCreated {
		t.Fatalf("state=%s want %s", retried.State, StateCreated)
	}
	if retried.RuntimeSessionID != 2 {
		t.Fatalf("runtime_session_id=%d want 2", retried.RuntimeSessionID)
	}
	if len(retried.Trace) == 0 || retried.Trace[len(retried.Trace)-1].Action != "retry_created" {
		t.Fatalf("missing retry trace: %#v", retried.Trace)
	}
}

func TestTransitionSuppressesDuplicateTrace(t *testing.T) {
	r := New()
	mission := r.Ensure(Mission{ID: "d-4", DecisionID: "d-4", TenantID: 7, RoomID: 15})
	if _, ok := r.Transition(mission.ID, StateReturningMainline, "returning", "等待主线回归"); !ok {
		t.Fatal("first transition failed")
	}
	first, _ := r.Snapshot(mission.ID)
	if _, ok := r.Transition(mission.ID, StateReturningMainline, "returning", "等待主线回归"); !ok {
		t.Fatal("second transition failed")
	}
	second, _ := r.Snapshot(mission.ID)
	if len(second.Trace) != len(first.Trace) {
		t.Fatalf("duplicate transition should not grow trace: before=%d after=%d", len(first.Trace), len(second.Trace))
	}
}
