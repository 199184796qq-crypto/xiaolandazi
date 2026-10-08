package httpapi

import (
	"livecompanion/management/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPresenterAvatarRouteRequiresAuthentication(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	mux := http.NewServeMux()
	s.registerLiveAnchorStyleRoutes(mux)
	r := httptest.NewRequest(http.MethodPatch, "/api/v1/live-anchor-styles/7/avatar", strings.NewReader(`{"avatar_type":"male"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized avatar accepted: %d", w.Code)
	}
}
