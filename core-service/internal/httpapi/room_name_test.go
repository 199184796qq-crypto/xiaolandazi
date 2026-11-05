package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoomNameHTTPAuthScopeAndJSON(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		token            bool
		want             int
	}{
		{"auth", "/internal/v1/rooms/29/name?tenant_id=14", `{"name":"名字","expected_name":"鹅"}`, false, 401},
		{"scope", "/internal/v1/rooms/29/name", `{"name":"名字","expected_name":"鹅"}`, true, 400},
		{"json", "/internal/v1/rooms/29/name?tenant_id=14", `{"name":"名字","external_room_id":"wrong"}`, true, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(nil, nil, nil, nil, nil, "production", "name-token")
			r := httptest.NewRequest(http.MethodPatch, tc.path, strings.NewReader(tc.body))
			if tc.token {
				r.Header.Set("X-Core-Token", "name-token")
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
