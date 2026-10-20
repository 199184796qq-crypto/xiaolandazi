package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlanVoiceRateRequiresAuthentication(t *testing.T) {
	for _, method := range []string{"GET", "PUT"} {
		s := &Server{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/api/v1/live-agent-plans/1/voice-rate", strings.NewReader(`{"rate":1.1}`))
		s.liveAgentPlanVoiceRate(w, r)
		if w.Code != 401 {
			t.Fatal(method, w.Code, w.Body.String())
		}
	}
}
