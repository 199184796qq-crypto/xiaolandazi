package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/auth"
)

func TestEditedRoomNumberNormalization(t *testing.T) {
	for _, value := range []string{" 521958972384 ", "https://live.douyin.com/521958972384?from=web_code_link", "http://live.douyin.com/521958972384/"} {
		number, err := normalizeEditedRoomNumber(value)
		if err != nil || number != "521958972384" {
			t.Fatal(number, err)
		}
	}
	for _, value := range []string{"", " ", "https://evil.test/123", "https://live.douyin.com.evil.test/123", "https://user@live.douyin.com/123", "https://live.douyin.com/123/path", "123?other", strings.Repeat("1", 129), "1\n2", "房间"} {
		if _, err := normalizeEditedRoomNumber(value); err == nil {
			t.Fatal(value)
		}
	}
}
func TestRoomNumberRequiresLogin(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPatch, "/api/v1/rooms/29/number", strings.NewReader(`{"external_room_id":"123","expected_external_room_id":"old"}`))
	r.SetPathValue("roomID", "29")
	s.updateRoomNumber(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code, w.Body.String())
	}
}
