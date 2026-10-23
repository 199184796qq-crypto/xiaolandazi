package coreclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRoomOwnerRecoveryNeverFailsOverReadOrUnknownWrite(t *testing.T) {
	var calls [2]atomic.Int32
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[0].Add(1)
		http.Error(w, "temporary", http.StatusServiceUnavailable)
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[1].Add(1)
		http.Error(w, "temporary", http.StatusServiceUnavailable)
	}))
	defer b.Close()
	client := New(a.URL+","+b.URL, "")
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPost} {
		resp, err := client.DoRoomOwner(context.Background(), 14, 29, method, "/recovery", nil, map[string]string{"command": "start"})
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatal(resp.StatusCode)
		}
	}
	if (calls[0].Load() != 3 || calls[1].Load() != 0) && (calls[1].Load() != 3 || calls[0].Load() != 0) {
		t.Fatal("replayed on another owner", calls[0].Load(), calls[1].Load())
	}
}
