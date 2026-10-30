package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInteractionV2HTTPAuthAndMalformedTaskNeverReachModels(t *testing.T) {
	s := &Server{purposeInternalToken: "secret"}
	for _, tc := range []struct {
		name, token string
		input       map[string]any
		want        int
	}{{"auth", "wrong", map[string]any{}, 401}, {"scope", "secret", map[string]any{"protocol": "interaction_v2"}, 400}, {"task", "secret", map[string]any{"protocol": "interaction_v2", "tenant_id": 14, "room_id": 29, "plan_id": 7, "text": "问题"}, 422}} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.input)
			r := httptest.NewRequest(http.MethodPost, "/internal/v1/interaction-purpose/resolve", bytes.NewReader(raw))
			r.Header.Set("X-Core-Token", tc.token)
			out := httptest.NewRecorder()
			s.resolveInteractionPurpose(out, r)
			if out.Code != tc.want {
				t.Fatal(out.Code, out.Body.String())
			}
		})
	}
}
func TestInteractionV2ExpiredInputDoesNotAccessDatabase(t *testing.T) {
	s := &Server{}
	_, err := s.prepareInteractionV2(context.Background(), interactionV2Input{Tenant: 14, Room: 29, Plan: 7, Task: "t", Epoch: "e", ExpiresAt: time.Now().Add(-time.Second)})
	if err == nil {
		t.Fatal("expired preparation did not fail")
	}
}
