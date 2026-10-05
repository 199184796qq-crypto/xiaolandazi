package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
