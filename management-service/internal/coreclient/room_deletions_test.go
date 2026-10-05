package coreclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRoomDeletionRequiresEveryReplicaAndCanRetry(t *testing.T) {
	var firstCalls, secondCalls atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls.Add(1)
		if r.Method != http.MethodDelete || r.URL.Path != "/internal/v1/rooms/15" || r.URL.Query().Get("tenant_id") != "27" || r.Header.Get("X-Core-Token") != "test-token" {
			t.Errorf("incorrect deletion request: %s %s", r.Method, r.URL)
		}
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer second.Close()
	client := New(first.URL+","+second.URL, "test-token")
	if err := client.DeleteRoomEverywhere(context.Background(), 27, 15); err == nil {
		t.Fatal("one failed replica must keep the deletion pending")
	}
	if firstCalls.Load() != 1 || secondCalls.Load() != 1 {
		t.Fatal("cleanup must reach the second replica even after the first fails")
	}
	fail.Store(false)
	if err := client.DeleteRoomEverywhere(context.Background(), 27, 15); err != nil {
		t.Fatal(err)
	}
	if firstCalls.Load() != 2 || secondCalls.Load() != 2 {
		t.Fatal("retry must revisit every replica idempotently")
	}
}
