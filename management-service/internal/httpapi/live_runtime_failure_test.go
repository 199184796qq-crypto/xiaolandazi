package httpapi

import (
	"errors"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
)

func TestPublicLiveFailureExplainsSourceChangeWithoutProviderSecrets(t *testing.T) {
	failure := publicLiveFailure(19, errors.New("正式事实或轮盘已变化，请重新保存发布安全方案"))
	if failure.SessionID != 19 || failure.Code != "plan_sources_changed" || !strings.Contains(failure.Message, "事实或轮盘已变化") || !strings.Contains(failure.Action, "重新保存方案") || strings.Contains(failure.Action, "发布") {
		t.Fatal(failure)
	}
	failure = publicLiveFailure(20, errors.New("provider https://example.test?api_key=private-key failed"))
	if strings.Contains(failure.Message, "private-key") || strings.Contains(failure.Message, "example.test") {
		t.Fatal("provider secret leaked", failure)
	}
}

func TestLiveRuntimeFailureDoesNotApplyToReplacementSession(t *testing.T) {
	snapshot := model.LiveRuntimeSnapshot{AgentState: "working", Session: &model.LiveRuntimeSession{ID: 2, Status: "stopped"}}
	applyLiveRuntimeFailure(&snapshot, &anchorcycle.LiveFailure{SessionID: 1, Message: "old failure"})
	if snapshot.Failure != nil || snapshot.AgentState != "working" {
		t.Fatal("old failure stopped replacement run")
	}
	applyLiveRuntimeFailure(&snapshot, &anchorcycle.LiveFailure{SessionID: 2, Code: "formal_mainline_failed", Message: "failed"})
	if snapshot.Failure == nil || snapshot.Failure.SessionID != 2 || snapshot.AgentState != "stopped" {
		t.Fatal(snapshot)
	}
}

func TestRecoveringSessionCannotBePresentedAsStoppedBySavedFailure(t *testing.T) {
	for _, status := range []string{"running", "paused", "starting", ""} {
		t.Run(status, func(t *testing.T) {
			snapshot := model.LiveRuntimeSnapshot{AgentState: "working", Session: &model.LiveRuntimeSession{ID: 476, Status: status}}
			applyLiveRuntimeFailure(&snapshot, &anchorcycle.LiveFailure{SessionID: 476, Code: "core_request_failed", Message: "音频交付失败"})
			if snapshot.Failure != nil || snapshot.AgentState != "working" {
				t.Fatal("saved request error invented a session stop", snapshot)
			}
		})
	}
}
