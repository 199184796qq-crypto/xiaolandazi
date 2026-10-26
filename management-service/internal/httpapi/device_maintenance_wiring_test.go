package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeviceMaintenanceFullRouterKeepsDisabledServiceOptional(t *testing.T) {
	// Construct the real router: compatibility tests on the individual submux
	// cannot detect conflicts with the pre-existing device/customer routes.
	s := &Server{env: "test", publicWebURL: "https://example.test"}
	handler := s.Handler()
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "/internal/v1/xiaozhi/maintenance", http.StatusUnauthorized},
		{http.MethodPost, "/internal/v1/xiaozhi/firmware-check", http.StatusUnauthorized},
		{http.MethodPost, "/api/v1/device-diagnostics/upload", http.StatusServiceUnavailable},
	} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, nil))
		if rr.Code != tc.status {
			t.Fatalf("%s: status=%d", tc.path, rr.Code)
		}
	}
}
