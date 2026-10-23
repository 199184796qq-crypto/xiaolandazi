package liveruntime

import (
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestOldManualPauseCannotFinalizeNewerDurableResume(t *testing.T) {
	now := time.Now().UTC()
	session := model.LiveRuntimeSession{Status: "running", LastBilledAt: now}
	state := coreAgentRuntimeState{State: "stopped", StopReason: "manual_pause", UpdatedAt: now.Add(-time.Second)}
	if !staleManualPauseBeforeResume(session, state) {
		t.Fatal("old pause was treated as a new stop")
	}
	for _, reason := range []string{"manual_stop", "manual", "quota_exhausted", "device_offline", "device_shutdown", "session_end", "live_finished", "core_restart", "system_error"} {
		copy := state
		copy.StopReason = reason
		if staleManualPauseBeforeResume(session, copy) {
			t.Fatal("real lifecycle stop ignored", reason)
		}
	}
	for _, updated := range []time.Time{now, now.Add(time.Second), {}} {
		copy := state
		copy.UpdatedAt = updated
		if staleManualPauseBeforeResume(session, copy) {
			t.Fatal("new or unknown pause receipt ignored", updated)
		}
	}
	for _, status := range []string{"paused", "stopped"} {
		copy := session
		copy.Status = status
		if staleManualPauseBeforeResume(copy, state) {
			t.Fatal(status)
		}
	}
	session.EndedAt = &now
	if staleManualPauseBeforeResume(session, state) {
		t.Fatal("ended session matched")
	}
	session.EndedAt = nil
	state.State = "stopping"
	if staleManualPauseBeforeResume(session, state) {
		t.Fatal("in-flight new stop was ignored")
	}
}
