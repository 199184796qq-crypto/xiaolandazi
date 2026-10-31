package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/messagecaptain"
)

func TestWelcomeReadyHTTPIdentityAuthenticationAndIdempotence(t *testing.T) {
	s, old := v2ServerItem(t)
	s.agentDecisions.ClearRoom(94)
	var raw map[string]any
	json.Unmarshal(old.PurposeKnowledge, &raw)
	raw["welcome_quota"] = true
	encoded, _ := json.Marshal(raw)
	item := s.agentDecisions.Enqueue(94, agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "welcome", MissionKind: "welcome_named", PurposeKnowledge: encoded, TTLSeconds: 60}).Item
	s.agentDecisions.ClaimNext(94)
	s.messages = messagecaptain.New()
	var admitted time.Time
	s.messages.Register("welcome-fixture", func(_ context.Context, m messagecaptain.Message) error {
		if m.Kind != "interaction.v2.welcome-ready" {
			return nil
		}
		if admitted.IsZero() {
			admitted = time.Now()
		}
		raw["welcome_ready_at"] = admitted
		raw["expires_at"] = admitted.Add(30 * time.Second)
		next, _ := json.Marshal(raw)
		if !s.agentDecisions.AdmitWelcome(94, item.ID, admitted.Add(30*time.Second), next) {
			t.Error("queue rejected ready")
		}
		return nil
	})
	url := "/internal/v1/rooms/94/agent-decisions/" + item.ID + "/welcome-ready?tenant_id=7"
	h := s.Handler()
	unauth := httptest.NewRecorder()
	h.ServeHTTP(unauth, httptest.NewRequest(http.MethodPost, url, nil))
	if unauth.Code != 401 || !admitted.IsZero() {
		t.Fatal("internal authorization missing", unauth.Code)
	}
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest(http.MethodPost, url, nil)
		r.Header.Set("X-Core-Token", "token")
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		if out.Code != 200 {
			t.Fatal(out.Code, out.Body.String())
		}
		var response struct {
			Expires time.Time `json:"expires_at"`
		}
		json.Unmarshal(out.Body.Bytes(), &response)
		if !response.Expires.Equal(admitted.Add(30 * time.Second)) {
			t.Fatal("TTL renewed")
		}
	}
	wrong := httptest.NewRequest(http.MethodPost, "/internal/v1/rooms/94/agent-decisions/"+item.ID+"/welcome-ready?tenant_id=8", nil)
	wrong.Header.Set("X-Core-Token", "token")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, wrong)
	if out.Code != 409 {
		t.Fatal("cross tenant", out.Code)
	}
	s.agentWork.StopAgent(94, "manual")
	r := httptest.NewRequest(http.MethodPost, url, nil)
	r.Header.Set("X-Core-Token", "token")
	out = httptest.NewRecorder()
	h.ServeHTTP(out, r)
	if out.Code != 409 {
		t.Fatal("stopped show", out.Code)
	}
}
