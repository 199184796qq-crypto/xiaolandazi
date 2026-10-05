package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/model"
)

func TestLiveSupportScopeRejectsConflictingRoomIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		name, header, query, path string
		want                      int64
		ok                        bool
	}{
		{"header", "12", "", "", 12, true},
		{"query media URL", "", "?support_room_id=12", "", 12, true},
		{"room endpoint", "", "", "12", 12, true},
		{"consistent", "12", "?room_id=12&support_room_id=12", "12", 12, true},
		{"forged body query target", "12", "?room_id=13", "", 0, false},
		{"forged path", "12", "", "13", 0, false},
		{"invalid header cannot fall back", "bad", "?room_id=12", "", 0, false},
		{"no room scope", "", "?tenant_id=1", "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/live-agent-plans"+tc.query, nil)
			r.Header.Set("X-Live-Support-Room-ID", tc.header)
			r.SetPathValue("roomID", tc.path)
			got, ok := liveSupportScopeRoomID(r)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("scope=(%d,%v), want=(%d,%v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestLiveAgentPlanTenantRequiresExplicitSupportScopeForEveryStaffRole(t *testing.T) {
	s := &Server{}
	tenantID := int64(7)
	for _, role := range []string{"staff", "platform_admin", "sales_staff"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			t.Run(role+"/"+method, func(t *testing.T) {
				w := httptest.NewRecorder()
				r := httptest.NewRequest(method, "/api/v1/live-agent-plans?tenant_id=7", nil)
				// A legacy tenant_id on an employee account must not act as consent.
				actor := model.Actor{Role: role, UserID: 9, TenantID: &tenantID}
				if _, ok := s.resolveLiveAgentPlanTenant(w, r, actor, tenantID, method != http.MethodGet); ok || w.Code != http.StatusForbidden {
					t.Fatalf("staff tenant bypass: status=%d body=%s", w.Code, w.Body.String())
				}
			})
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/live-agent-plans", nil)
	if got, ok := s.resolveLiveAgentPlanTenant(w, r, model.Actor{Role: "customer", TenantID: &tenantID}, 0, false); !ok || got != tenantID {
		t.Fatal("customer own tenant lost access")
	}
	w = httptest.NewRecorder()
	if _, ok := s.resolveLiveAgentPlanTenant(w, r, model.Actor{Role: "customer", TenantID: &tenantID}, 8, false); ok || w.Code != http.StatusForbidden {
		t.Fatal("customer crossed tenant")
	}
}

func TestSupportConfigProjectionDoesNotExposeOrMutateOtherRooms(t *testing.T) {
	original := model.AgentConfigVersion{
		ID: 10, LifecycleStatus: "active", Layer1: map[string]any{"secret": true}, Persona: map[string]any{"private": true},
		SpeechConfig: map[string]any{"api_key": "private", "rooms": map[string]any{
			"12": map[string]any{"selected_voice": map[string]any{"voice_id": "allowed"}},
			"13": map[string]any{"selected_voice": map[string]any{"voice_id": "private"}},
		}},
	}
	got := supportConfigProjection(original, 12)
	if got.Layer1 != nil || got.Persona != nil || len(got.SpeechConfig) != 1 {
		t.Fatalf("tenant config exposed: %#v", got)
	}
	rooms := asSupportMap(got.SpeechConfig["rooms"])
	if len(rooms) != 1 || rooms["13"] != nil {
		t.Fatal("another room exposed")
	}
	asSupportMap(asSupportMap(rooms["12"])["selected_voice"])["voice_id"] = "changed"
	if reflect.DeepEqual(got.SpeechConfig, original.SpeechConfig) {
		t.Fatal("projection not isolated")
	}
	if asSupportMap(asSupportMap(asSupportMap(original.SpeechConfig["rooms"])["12"])["selected_voice"])["voice_id"] != "allowed" {
		t.Fatal("projection mutated original")
	}
}

func TestSupportMediaRequiresMatchingRoomNotJustTenant(t *testing.T) {
	s := &Server{}
	for _, tc := range []struct {
		room     int64
		metadata map[string]any
		want     bool
	}{
		{12, map[string]any{"room_id": float64(12)}, true},
		{12, map[string]any{"support_room_id": "12"}, true},
		{12, map[string]any{"room_id": float64(13)}, false},
		{12, nil, false}, {0, nil, false},
	} {
		if got := s.liveSupportMediaAllowed(context.Background(), tc.room, model.MediaAsset{TenantID: 7, Metadata: tc.metadata}); got != tc.want {
			t.Fatalf("media scope=%v want=%v", got, tc.want)
		}
	}
}

func TestCustomerStrategyHandlersDenyUnauthenticatedRequests(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	for _, tc := range []struct {
		method, path string
		handler      http.HandlerFunc
	}{
		{http.MethodGet, "/api/v1/live-agent-plans", s.liveAgentPlanList},
		{http.MethodPost, "/api/v1/live-agent-plans", s.liveAgentPlanCreate},
		{http.MethodGet, "/api/v1/live/agent/config-versions", s.liveAgentConfigVersions},
		{http.MethodPost, "/api/v1/live/agent/config-versions", s.liveCreateAgentConfigDraft},
		{http.MethodGet, "/api/v1/live/voice-profiles", s.liveListVoiceProfiles},
		{http.MethodGet, "/api/v1/live/media-assets", s.liveListMediaAssets},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc(tc.method+" "+tc.path, tc.handler)
			r := httptest.NewRequest(tc.method, tc.path+"?tenant_id=7&support_room_id=12", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
