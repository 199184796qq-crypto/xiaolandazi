package coreclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClientParsesCoreCluster(t *testing.T) {
	client := New("http://core-a:8081/, http://core-b:8081", "token")
	if got := client.NodeCount(); got != 2 {
		t.Fatalf("NodeCount=%d", got)
	}
	first := client.baseURLForRoom(3, 9)
	for i := 0; i < 100; i++ {
		if got := client.baseURLForRoom(3, 9); got != first {
			t.Fatalf("room routing changed: first=%q got=%q", first, got)
		}
	}
	if first != "http://core-a:8081" && first != "http://core-b:8081" {
		t.Fatalf("unexpected routed node %q", first)
	}
}

func TestSingleCoreAlwaysRoutesToOnlyNode(t *testing.T) {
	client := New("http://core-only:8081", "token")
	if got := client.baseURLForRoom(100, 200); got != "http://core-only:8081" {
		t.Fatalf("unexpected routed node %q", got)
	}
}
func TestRoomRoutingGoldenFixtureMatchesCoreShard(t *testing.T) {
	client := New("http://core-0:8081,http://core-1:8081,http://core-2:8081,http://core-3:8081", "token")
	if got := client.baseURLForRoom(27, 991); got != "http://core-1:8081" {
		t.Fatalf("golden room route=%q", got)
	}
}

func TestDoAnyFailsOverOnServerFailure(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer second.Close()

	client := New(first.URL+","+second.URL, "token")
	resp, err := client.DoAny(
		context.Background(),
		http.MethodPost,
		"/internal/v1/rooms/runtime-states",
		nil,
		map[string]any{"items": []any{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestDoRoomFailsOverWhenPreferredCoreIsUnavailable(t *testing.T) {
	var firstFail atomic.Bool
	var secondFail atomic.Bool
	var firstHits atomic.Int64
	var secondHits atomic.Int64

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits.Add(1)
		if firstFail.Load() {
			http.Error(w, "not room owner", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		if secondFail.Load() {
			http.Error(w, "not room owner", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer second.Close()

	client := New(first.URL+","+second.URL, "token")
	tenantID, roomID := int64(27), int64(991)
	if client.baseURLForRoom(tenantID, roomID) == first.URL {
		firstFail.Store(true)
	} else {
		secondFail.Store(true)
	}

	resp, err := client.DoRoom(
		context.Background(),
		tenantID,
		roomID,
		http.MethodGet,
		"/internal/v1/rooms/991/preview",
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if firstHits.Load() != 1 || secondHits.Load() != 1 {
		t.Fatalf(
			"expected preferred+fallback attempt, first_hits=%d second_hits=%d",
			firstHits.Load(),
			secondHits.Load(),
		)
	}
}

func TestDoRoomDoesNotReplayPostAcrossCoreNodes(t *testing.T) {
	var firstHits atomic.Int64
	var secondHits atomic.Int64
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits.Add(1)
		http.Error(w, "ambiguous write failure", http.StatusInternalServerError)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer second.Close()

	client := New(first.URL+","+second.URL, "token")
	tenantID, roomID := int64(27), int64(991)
	primary := client.baseURLForRoom(tenantID, roomID)
	if primary != first.URL {
		client = New(second.URL+","+first.URL, "token")
		primary = client.baseURLForRoom(tenantID, roomID)
	}

	resp, err := client.DoRoom(
		context.Background(),
		tenantID,
		roomID,
		http.MethodPost,
		"/internal/v1/rooms/991/action",
		nil,
		map[string]any{"action": "start"},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if primary == first.URL && firstHits.Load() != 1 {
		t.Fatalf("first primary hits=%d", firstHits.Load())
	}
	if primary == second.URL && secondHits.Load() != 1 {
		t.Fatalf("second primary hits=%d", secondHits.Load())
	}
	if firstHits.Load()+secondHits.Load() != 1 {
		t.Fatalf(
			"POST must not be replayed, first_hits=%d second_hits=%d",
			firstHits.Load(),
			secondHits.Load(),
		)
	}
}
