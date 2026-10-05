package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/auth"
)

func TestSpeechModelAdministrationRequiresAuthentication(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	for _, tc := range []struct {
		method  string
		handler http.HandlerFunc
	}{
		{http.MethodGet, s.systemSpeechModelsGet},
		{http.MethodPut, s.systemSpeechModelsSave},
		{http.MethodPost, s.systemSpeechModelsTest},
		{http.MethodPost, s.systemSpeechModelsList},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, "/api/v1/system/speech-models", strings.NewReader(`{"api_key":"fixture-only-secret"}`))
		tc.handler(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: %d %s", tc.method, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "fixture-only-secret") {
			t.Fatal("credential returned on unauthorized request")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("administrative response is cacheable")
		}
	}
}

func TestSpeechInlineCredentialResolution(t *testing.T) {
	s := &Server{}
	p, err := s.speechProfileWithKey(context.Background(), agentgateway.SpeechModelInput{NewAPIKey: " fixture-only-key "})
	if err != nil || p.APIKey != "fixture-only-key" {
		t.Fatal("inline key should require no saved profile", err)
	}
	if _, err = s.speechProfileWithKey(context.Background(), agentgateway.SpeechModelInput{}); err == nil {
		t.Fatal("empty profile should not resolve a default key")
	}
	if _, err = s.speechProfileWithKey(context.Background(), agentgateway.SpeechModelInput{NewAPIKey: strings.Repeat("x", 4097)}); err == nil {
		t.Fatal("oversized key accepted")
	}
}
