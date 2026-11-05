package httpapi

import (
	"livecompanion/management/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoomNameValidation(t *testing.T) {
	for _, v := range []string{"鹅", "新的名字🦆", strings.Repeat("名", 128)} {
		if !validEditedRoomName(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"", strings.Repeat("名", 129), "名\n字", "名\x00字", string([]byte{0xff})} {
		if validEditedRoomName(v) {
			t.Fatal(v)
		}
	}
}
func TestRoomNameRequiresLogin(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPatch, "/api/v1/rooms/29/name", strings.NewReader(`{"name":"新名字","expected_name":"鹅"}`))
	r.SetPathValue("roomID", "29")
	s.updateRoomName(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code, w.Body.String())
	}
}
