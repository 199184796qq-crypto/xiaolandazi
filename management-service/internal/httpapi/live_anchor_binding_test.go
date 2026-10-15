package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/auth"
)

func TestAnchorBindAndCancelRequireAuthentication(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	mux := http.NewServeMux()
	s.registerLiveAnchorStyleRoutes(mux)
	for _, method := range []string{"POST", "DELETE"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/api/v1/live-anchor-styles/7/bind-plan?tenant_id=14", strings.NewReader(`{"plan_id":5}`))
		mux.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal(method, w.Code, w.Body.String())
		}
	}
}
