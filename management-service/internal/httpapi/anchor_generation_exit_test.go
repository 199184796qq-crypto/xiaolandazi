package httpapi

import (
	"errors"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"testing"
	"time"
)

func TestProducerFailureRequiresExactRunningOwner(t *testing.T) {
	session := model.LiveRuntimeSession{ID: 218, ExecutionRealm: "prod", Status: "running"}
	if !producerOwnsRunningSession(session, 218, "prod") {
		t.Fatal("matching owner rejected")
	}
	if producerOwnsRunningSession(session, 219, "prod") || producerOwnsRunningSession(session, 218, "dev") {
		t.Fatal("replacement/realm accepted")
	}
	for _, status := range []string{"paused", "stopped", "starting"} {
		session.Status = status
		if producerOwnsRunningSession(session, 218, "prod") {
			t.Fatal("non-running owner accepted", status)
		}
	}
	session.Status = "running"
	now := time.Now()
	session.EndedAt = &now
	if producerOwnsRunningSession(session, 218, "prod") {
		t.Fatal("ended owner accepted")
	}
}

func TestProducerHonorsExplicitCoreReasonNotCachedRoomStatus(t *testing.T) {
	for _, reason := range []string{"live_finished", "manual", "manual_pause", "device_offline", "device_shutdown", "quota_exhausted", "core_restart"} {
		for _, status := range []string{"stopped", "stopping"} {
			if got := explicitProducerStop(coreAgentRuntimeState{State: status, StopReason: reason}); got != reason {
				t.Fatal(got)
			}
		}
		if got := explicitProducerStop(coreAgentRuntimeState{State: "working", StopReason: reason}); got != "" {
			t.Fatal("stale reason used", got)
		}
	}
	if got := explicitProducerStop(coreAgentRuntimeState{State: "stopped"}); got != "" {
		t.Fatal("empty stop invented")
	}
}

func TestNormalStopsCannotShowStaleVoiceFailure(t *testing.T) {
	for _, reason := range []string{"live_finished", "manual_stop", "device_offline", "device_shutdown", "quota_exhausted", "core_restart"} {
		snapshot := model.LiveRuntimeSnapshot{AgentState: "stopped", Session: &model.LiveRuntimeSession{ID: 218, Status: "stopped", StopReason: reason}}
		applyLiveRuntimeFailure(&snapshot, &anchorcycle.LiveFailure{SessionID: 218, Code: "formal_mainline_failed"})
		if snapshot.Failure != nil {
			t.Fatal("normal lifecycle stop showed voice failure", reason)
		}
	}
	snapshot := model.LiveRuntimeSnapshot{Session: &model.LiveRuntimeSession{ID: 218, Status: "stopped", StopReason: "formal_mainline_failed"}}
	applyLiveRuntimeFailure(&snapshot, &anchorcycle.LiveFailure{SessionID: 218, Code: "formal_mainline_failed"})
	if snapshot.Failure == nil {
		t.Fatal("real failure hidden")
	}
}

func TestStaleGenerationFailureIsNotMislabeledVoiceFailure(t *testing.T) {
	for _, err := range []error{anchorcycle.ErrStale, errors.New(anchorcycle.ErrStale.Error())} {
		if failure := publicLiveFailure(218, err); failure.Code != "runtime_state_changed" {
			t.Fatal(failure)
		}
	}
}
