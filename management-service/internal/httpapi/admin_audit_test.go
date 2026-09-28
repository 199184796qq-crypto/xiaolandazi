package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"livecompanion/management/internal/model"
)

func TestAuditActionMapsRoomAndAgentLifecycle(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodPatch, "/api/v1/rooms/15/monitor", "room.monitor.update"},
		{http.MethodPost, "/api/v1/rooms/15/runtime/start", "agent.runtime.start"},
		{http.MethodPost, "/api/v1/rooms/15/runtime/pause", "agent.runtime.pause"},
		{http.MethodPost, "/api/v1/rooms/15/runtime/resume", "agent.runtime.resume"},
		{http.MethodPost, "/api/v1/rooms/15/runtime/stop", "agent.runtime.stop"},
		{http.MethodPost, "/api/v1/rooms/15/runtime/mode", "agent.runtime.mode_update"},
		{http.MethodPost, "/api/v1/rooms/15/runtime/plan", "agent.runtime.plan_update"},
		{http.MethodPost, "/api/v1/live/rooms/15/policy/versions/88/publish", "strategy.publish"},
		{http.MethodPost, "/api/v1/live/rooms/15/policy/versions/88/rollback", "strategy.rollback"},
		{http.MethodPost, "/api/v1/live/rooms/15/agent-learning/sessions", "agent.learning.session.create"},
		{http.MethodPost, "/api/v1/live/rooms/15/agent-learning/sessions/9/adopt", "agent.learning.adopt"},
		{http.MethodPost, "/api/v1/live/rooms/15/agent-memories/7/deactivate", "agent.memory.deactivate"},
		{http.MethodPost, "/api/v1/live/rooms/15/agent-memories/7/versions/3/rollback", "agent.memory.rollback"},
		{http.MethodPut, "/api/v1/live-agent-plans/4", "agent.plan.update"},
		{http.MethodPost, "/api/v1/live-agent-plans/4/scripts", "agent.plan.script.create"},
		{http.MethodPut, "/api/v1/live-agent-plans/4/scripts/8", "agent.plan.script.update"},
		{http.MethodPost, "/api/v1/live-agent-plans/4/scripts/8/analyze", "agent.plan.script.analyze"},
		{http.MethodPost, "/api/v1/live-agent-plans/4/room-bindings", "agent.plan.bind_room"},
		{http.MethodDelete, "/api/v1/live-agent-plans/4/room-bindings/15", "agent.plan.unbind_room"},
		{http.MethodPost, "/api/v1/live/devices/22/bind", "device.bind_room"},
		{http.MethodPost, "/api/v1/live/devices/22/control", "device.control"},
		{http.MethodPost, "/api/v1/rooms/15/capture/audio/start", "room.recording.start"},
		{http.MethodPost, "/api/v1/rooms/15/capture/audio/stop", "room.recording.stop"},
		{http.MethodPut, "/api/v1/system/agent-routing/draft", "system.agent_routing.draft_save"},
		{http.MethodPost, "/api/v1/system/agent-routing/publish", "system.agent_routing.publish"},
		{http.MethodPost, "/api/v1/system/agent-routing/rollback", "system.agent_routing.rollback"},
		{http.MethodPost, "/api/v1/system/agent-routing/assist", "system.agent_routing.assist"},
		{http.MethodPut, "/api/v1/system/agent-understanding/policy", "system.agent_understanding.policy_save"},
		{http.MethodDelete, "/api/v1/system/agent-understanding/policy/tenant/14", "system.agent_understanding.policy_delete"},
		{http.MethodDelete, "/api/v1/rooms/15", "room.delete"},
	}
	for _, test := range tests {
		if got := auditAction(test.method, test.path); got != test.want {
			t.Fatalf("%s %s action=%q want %q", test.method, test.path, got, test.want)
		}
	}
}

func TestAuditActionDoesNotMislabelNestedRoomDelete(t *testing.T) {
	path := "/api/v1/rooms/15/agent-decisions/decision-1"
	got := auditAction(http.MethodDelete, path)
	if got == "room.delete" {
		t.Fatalf("nested room delete path was mislabeled as room.delete: %q", got)
	}
}

func TestShouldAuditRequestSkipsHighFrequencyTechnicalTraffic(t *testing.T) {
	skipped := []string{
		"/api/v1/live/devices/22/heartbeat",
		"/api/v1/rooms/15/runtime/events",
		"/api/v1/live/rooms/15/agent-learning/intent",
		"/api/v1/live/rooms/15/agent-learning/chat",
		"/api/v1/live/rooms/15/agent/chat",
		"/api/v1/live/rooms/15/agent-learning/sessions/9/turns",
		"/api/v1/live/rooms/15/agent-learning/sessions/9/test",
		"/api/v1/live/rooms/15/policy-agent/chat",
		"/api/v1/rooms/15/agent-decisions/manual",
	}
	for _, path := range skipped {
		if shouldAuditRequest(http.MethodPost, path) {
			t.Fatalf("technical traffic must not enter operation audit: %s", path)
		}
	}
	for _, path := range []string{
		"/api/v1/rooms/15/monitor",
		"/api/v1/rooms/15/runtime/stop",
		"/api/v1/live/rooms/15/agent-learning/sessions/9/adopt",
		"/api/v1/live/devices/22/bind",
	} {
		if !shouldAuditRequest(http.MethodPost, path) {
			t.Fatalf("business mutation must be audited: %s", path)
		}
	}
}

func TestEnrichAuditFromPayloadRefinesMonitorAction(t *testing.T) {
	entry := model.AdminAuditLog{Action: "room.monitor.update"}
	enrichAuditFromPayload(map[string]any{"enabled": false}, &entry)
	if entry.Action != "room.monitor.disconnect" {
		t.Fatalf("action=%q want room.monitor.disconnect", entry.Action)
	}
	if entry.AfterState != `{"monitor_enabled":false}` {
		t.Fatalf("after_state=%q", entry.AfterState)
	}

	entry = model.AdminAuditLog{Action: "room.monitor.update"}
	enrichAuditFromPayload(map[string]any{"enabled": true}, &entry)
	if entry.Action != "room.monitor.connect" {
		t.Fatalf("action=%q want room.monitor.connect", entry.Action)
	}
	if entry.AfterState != `{"monitor_enabled":true}` {
		t.Fatalf("after_state=%q", entry.AfterState)
	}
}

func TestEnrichAuditFromPayloadCapturesBindingTargets(t *testing.T) {
	entry := model.AdminAuditLog{Action: "device.bind_room"}
	enrichAuditFromPayload(map[string]any{"room_id": float64(15), "binding_role": "primary"}, &entry)
	if entry.TargetRoomID != 15 {
		t.Fatalf("device binding room=%d want 15", entry.TargetRoomID)
	}
	if entry.DetailJSON == "" {
		t.Fatal("device binding detail_json must contain binding role")
	}

	entry = model.AdminAuditLog{Action: "agent.plan.bind_room"}
	enrichAuditFromPayload(map[string]any{"room_id": float64(16), "tenant_id": float64(14)}, &entry)
	if entry.TargetRoomID != 16 || entry.TargetTenantID != 14 {
		t.Fatalf("plan binding target=%#v", entry)
	}

	entry = model.AdminAuditLog{Action: "device.control"}
	enrichAuditFromPayload(map[string]any{"room_id": float64(15), "action": "stop"}, &entry)
	if entry.TargetRoomID != 15 || entry.Reason != "stop" {
		t.Fatalf("device control target=%#v", entry)
	}
}

func TestAuditSourceForActor(t *testing.T) {
	if got := auditSourceForActor(model.Actor{Role: "customer"}); got != "customer_web" {
		t.Fatalf("customer source=%q", got)
	}
	if got := auditSourceForActor(model.Actor{Role: "staff"}); got != "internal_web" {
		t.Fatalf("staff source=%q", got)
	}
}

func TestPositivePathValueWorksBeforeServeMuxRouting(t *testing.T) {
	tests := []struct {
		path string
		key  string
		want int64
	}{
		{"/api/v1/rooms/15/runtime/start", "roomID", 15},
		{"/api/v1/live/rooms/15/policy/versions/88/publish", "roomID", 15},
		{"/api/v1/live/rooms/15/policy/versions/88/publish", "versionID", 88},
		{"/api/v1/live/rooms/15/agent-learning/sessions/9/adopt", "sessionID", 9},
		{"/api/v1/live/rooms/15/agent-memories/7/versions/3/rollback", "memoryID", 7},
		{"/api/v1/live-agent-plans/4/room-bindings/15", "planID", 4},
		{"/api/v1/live-agent-plans/4/room-bindings/15", "roomID", 15},
		{"/api/v1/live/devices/22/bind", "deviceID", 22},
		{"/api/v1/admin/customers/501/reset-password", "userID", 501},
		{"/api/v1/admin/customers/by-tenant/14", "tenantID", 14},
	}
	for _, test := range tests {
		req := httptest.NewRequest(http.MethodPost, test.path, nil)
		if got := positivePathValue(req, test.key); got != test.want {
			t.Fatalf("path=%s key=%s got=%d want=%d", test.path, test.key, got, test.want)
		}
	}
}
