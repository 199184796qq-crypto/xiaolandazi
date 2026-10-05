package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"livecompanion/management/internal/auth"
)

func TestRoomCustomerContactRequiresAuthenticationAndNoCache(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/15/customer-contact", nil)
	r.SetPathValue("roomID", "15")
	s.roomCustomerContact(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("customer contact may not be cached")
	}
}
