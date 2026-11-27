package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/model"
)

func TestLivePlanSupportRoutesRequireAuthentication(t *testing.T) {
	handler := (&Server{auth: auth.NewResolver("test", nil)}).Handler()
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/v1/live-agent-plans/9/support-requests"},
		{"POST", "/api/v1/live-agent-plans/9/support-requests/20"},
		{"DELETE", "/api/v1/live-agent-plans/9/support-requests/1"},
		{"GET", "/api/v1/liveops/plan-support-requests"},
		{"GET", "/api/v1/admin/workspace/liveops"},
		{"GET", "/api/v1/admin/workspace/liveops/overview"},
		{"GET", "/api/v1/admin/workspace/liveops/plans/9"},
		{"GET", "/api/v1/admin/workspace/liveops/rooms/1/plan-bindings"},
		{"PUT", "/api/v1/admin/workspace/liveops/rooms/1/plan-bindings/9"},
		{"PATCH", "/api/v1/rooms/1/monitor"},
		{"POST", "/api/v1/liveops/plan-support-requests/1/accept"},
		{"POST", "/api/v1/liveops/plan-support-requests/1/reject"},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s returned %d", route.method, route.path, w.Code)
		}
	}
}

func TestLivePlanSupportScope(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, plan, room, asset, header string
		want                                            bool
	}{
		{"unbound plan", "GET", "/api/v1/live-agent-plans/9", "9", "", "", "9", true},
		{"shared workspace", "GET", "/api/v1/live-agent-plans/9/workspace?room_id=0&tenant_id=7", "9", "", "", "9", true},
		{"save current plan", "PUT", "/api/v1/live-agent-plans/9/editing-draft", "9", "", "", "9", true},
		{"other plan", "PUT", "/api/v1/live-agent-plans/10/editing-draft", "10", "", "", "9", false},
		{"customer plan list", "GET", "/api/v1/live-agent-plans", "", "", "", "9", false},
		{"archive", "POST", "/api/v1/live-agent-plans/9/archive", "9", "", "", "9", false},
		{"bind", "POST", "/api/v1/live-agent-plans/9/room-bindings", "9", "", "", "9", false},
		{"publish to room", "POST", "/api/v1/live-agent-plans/9/versions/2/publish", "9", "", "", "9", false},
		{"question stats rooms", "GET", "/api/v1/customer/agent-chat/question-stats/9/rooms", "9", "", "", "9", true},
		{"question stats sessions", "GET", "/api/v1/customer/agent-chat/question-stats/9/rooms/2/sessions", "9", "2", "", "9", true},
		{"question stats different plan", "GET", "/api/v1/customer/agent-chat/question-stats/10/rooms/2/sessions", "10", "2", "", "9", false},
		{"question stats conflicting room", "GET", "/api/v1/customer/agent-chat/question-stats/9/rooms/2/sessions?support_room_id=2", "9", "2", "", "9", false},
		{"question stats room mutation", "POST", "/api/v1/customer/agent-chat/question-stats/9/rooms/2/sessions", "9", "2", "", "9", false},
		{"actual live room", "GET", "/api/v1/rooms/2/live-agent-plan", "", "2", "", "9", false},
		{"spoof room", "GET", "/api/v1/live-agent-plans/9?room_id=0&room_id=2", "9", "", "", "9", false},
		{"conflicting scopes", "GET", "/api/v1/live-agent-plans/9?support_room_id=2", "9", "", "", "9", false},
		{"conflicting plan", "GET", "/api/v1/live-agent-plans/9?support_plan_id=10", "9", "", "", "9", false},
		{"scoped media", "GET", "/api/v1/live/media-assets/3/content?support_plan_id=9", "", "", "3", "", true},
		{"scoped voices read", "GET", "/api/v1/live/voice-profiles", "", "", "", "9", true},
		{"shared voices mutation", "POST", "/api/v1/live/voice-profiles", "", "", "", "9", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.target, nil)
			r.SetPathValue("planID", tc.plan)
			r.SetPathValue("roomID", tc.room)
			r.SetPathValue("assetID", tc.asset)
			if tc.header != "" {
				r.Header.Set("X-Live-Support-Plan-ID", tc.header)
			}
			id, present, valid := livePlanSupportScope(r)
			if !present || (valid && livePlanSupportEndpointAllowed(r, id)) != tc.want {
				t.Fatalf("scope for %s %s: id %d, present %v, valid %v", tc.method, tc.target, id, present, valid)
			}
			if !tc.want {
				// Rejection must happen before any database lookup or legacy
				// room authorization fallback.
				rec := httptest.NewRecorder()
				s := &Server{}
				if _, ok := s.resolveLiveAgentPlanTenant(rec, r, model.Actor{Role: "platform_admin", UserID: 1}, 0, r.Method != http.MethodGet); ok || rec.Code != http.StatusForbidden {
					t.Fatalf("unauthorized request passed: HTTP %d", rec.Code)
				}
			}
		})
	}
}

func TestLivePlanSupportScopedVoicePreview(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/live/voice-profiles/3/preview?support_plan_id=9", nil)
	req.SetPathValue("profileID", "3")
	if !livePlanSupportEndpointAllowed(req, 9) {
		t.Fatal("plan support cannot preview a referenced voice")
	}
	req.URL.Path = "/api/v1/live/voice-profiles/3/name"
	if livePlanSupportEndpointAllowed(req, 9) {
		t.Fatal("plan support can modify a shared voice")
	}
}
