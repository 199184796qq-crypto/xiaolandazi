package httpapi

import (
	"livecompanion/management/internal/auth"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelPoolEndpointsRequireAuthenticatedAdministrator(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	for _, handler := range []http.HandlerFunc{s.systemModelPoolGet, s.systemModelPoolSave, s.systemModelPoolDelete, s.systemModelPoolModels, s.systemModelPoolImport} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodPost, "/api/v1/system/model-pool", nil))
		if w.Code != http.StatusUnauthorized || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("unauthenticated pool access", w.Code, w.Body.String())
		}
	}
}
