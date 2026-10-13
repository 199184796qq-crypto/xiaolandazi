package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/auth"
)

func TestHardwareAPIDeniesMissingOrInvalidToken(t *testing.T) {
	for _, configured := range []string{"", "secret"} {
		s := &Server{}
		s.SetXiaozhiInternalToken(configured)
		for _, supplied := range []string{"", "wrong"} {
			r := httptest.NewRequest(http.MethodPost, "/internal/v1/xiaozhi/provision", nil)
			r.Header.Set("X-Xiaozhi-Internal-Token", supplied)
			w := httptest.NewRecorder()
			s.hardwareProvision(w, r)
			if w.Code != 401 {
				t.Fatal(configured, supplied, w.Code)
			}
		}
	}
}
func TestClaimLimiter(t *testing.T) {
	var l deviceClaimLimiter
	for i := 0; i < 5; i++ {
		if !l.allow(25) {
			t.Fatal("legitimate attempts rejected")
		}
	}
	if l.allow(25) {
		t.Fatal("brute force not limited")
	}
	if !l.allow(26) {
		t.Fatal("independent user blocked")
	}
}

func TestCustomerAccountUnclaimRequiresLoginAndPOST(t *testing.T) {
	s := &Server{auth: auth.NewResolver("production", nil)}
	mux := http.NewServeMux()
	s.registerDeviceProvisioningRoutes(mux)
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		r := httptest.NewRequest(method, "/api/v1/live/devices/7/unclaim", strings.NewReader(`{"confirmed":true}`))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		want := http.StatusMethodNotAllowed
		if method == http.MethodPost {
			want = http.StatusUnauthorized
		}
		if w.Code != want {
			t.Fatal(method, w.Code, w.Body.String())
		}
	}
}
