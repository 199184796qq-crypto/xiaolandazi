package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProductionDoesNotRegisterDevAudioRoutes(t *testing.T) {
	server := New(nil, nil, nil, nil, nil, "production", "core-secret")
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/dev/audio/program/start", nil)
	req.Header.Set("X-Core-Token", "core-secret")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d want=404 body=%s", rec.Code, rec.Body.String())
	}
}
