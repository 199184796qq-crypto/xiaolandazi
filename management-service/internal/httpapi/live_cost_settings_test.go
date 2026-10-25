package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"livecompanion/management/internal/auth"
)

func TestLiveCostSettingsRequireAuthenticatedAdministrator(t *testing.T) {
	server := &Server{auth: auth.NewResolver("test", nil)}
	for _, handler := range []http.HandlerFunc{server.systemLiveCostSettingsGet, server.systemLiveCostSettingsPut} {
		response := httptest.NewRecorder()
		handler(response, httptest.NewRequest(http.MethodGet, "/api/v1/system/live-cost-settings", nil))
		if response.Code != http.StatusUnauthorized || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(response.Code, response.Body.String())
		}
	}
}
