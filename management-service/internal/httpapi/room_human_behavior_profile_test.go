package httpapi

import (
	"livecompanion/management/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRetiredRoomHostStateRoutesAreUnavailable(t *testing.T) {
	// A nil store ensures no retired route can reach a database read or write.
	s := &Server{auth: auth.NewResolver("test", nil)}
	handler := s.Handler()
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPost, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(method, "/api/v1/rooms/29/human-behavior-profile", strings.NewReader(`{"state_text":"今天嗓子不舒服","state_expires_at":"2099-01-01T00:00:00Z"}`))
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusNotFound {
				t.Fatalf("retired route returned %d, want 404", w.Code)
			}
		})
	}
}
