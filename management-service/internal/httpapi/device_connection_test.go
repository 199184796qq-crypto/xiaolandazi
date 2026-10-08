package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceConnectionRequiresGatewayAndProof(t *testing.T) {
	s := &Server{xiaozhiInternalToken: "internal-test-token"}
	for _, test := range []struct {
		token, body string
		status      int
	}{
		{"", `{"phase":"shutdown"}`, 401},
		{"wrong", `{"phase":"shutdown"}`, 401},
		{"internal-test-token", `{"hardware_mac":"1c:29:04:31:0e:b8","room_id":26}`, 400},
	} {
		r := httptest.NewRequest("POST", "/internal/v1/xiaozhi/connection", strings.NewReader(test.body))
		r.Header.Set("X-Xiaozhi-Internal-Token", test.token)
		w := httptest.NewRecorder()
		s.hardwareConnection(w, r)
		if w.Code != test.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
