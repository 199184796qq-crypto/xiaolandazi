package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"livecompanion/management/internal/coreclient"
)

func TestProbeCoreStatusReportsAvailabilityAndBootID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","boot_id":"core-test-boot","active_rooms":2}`))
	}))
	defer server.Close()

	api := &Server{core: coreclient.New(server.URL, "test-token")}
	status := api.probeCoreStatus(context.Background())
	if !status.Available {
		t.Fatal("core should be available")
	}
	if status.Status != "ok" {
		t.Fatalf("status=%q want ok", status.Status)
	}
	if status.CoreBootID != "core-test-boot" {
		t.Fatalf("boot_id=%q want core-test-boot", status.CoreBootID)
	}
	if status.ActiveRooms != 2 {
		t.Fatalf("active_rooms=%d want 2", status.ActiveRooms)
	}
}

func TestProbeCoreStatusReportsUnavailableOnTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	api := &Server{core: coreclient.New(url, "test-token")}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status := api.probeCoreStatus(ctx)
	if status.Available {
		t.Fatal("core should be unavailable")
	}
	if status.Status != "unavailable" {
		t.Fatalf("status=%q want unavailable", status.Status)
	}
}

func TestApplyCoreStatusTransitionTracksRecoveryAndBootChange(t *testing.T) {
	api := &Server{}
	first := coreRuntimeStatus{
		Available:   true,
		Status:      "ok",
		CoreBootID:  "boot-a",
		CheckedAt:   time.Now().UTC(),
		initialized: true,
	}
	api.applyCoreStatusTransition(context.Background(), first)

	offline := coreRuntimeStatus{
		Available:   false,
		Status:      "unavailable",
		CheckedAt:   first.CheckedAt.Add(time.Second),
		initialized: true,
	}
	api.applyCoreStatusTransition(context.Background(), offline)
	if api.coreStatus.Available {
		t.Fatal("core status should be offline")
	}
	if api.coreStatus.LastTransitionAt.IsZero() {
		t.Fatal("offline transition time was not recorded")
	}

	recovered := coreRuntimeStatus{
		Available:   true,
		Status:      "ok",
		CoreBootID:  "boot-b",
		CheckedAt:   offline.CheckedAt.Add(time.Second),
		initialized: true,
	}
	api.applyCoreStatusTransition(context.Background(), recovered)
	if !api.coreStatus.Available {
		t.Fatal("core status should be recovered")
	}
	if api.coreStatus.CoreBootID != "boot-b" {
		t.Fatalf("recovered boot_id=%q want boot-b", api.coreStatus.CoreBootID)
	}

	bootChanged := recovered
	bootChanged.CoreBootID = "boot-c"
	bootChanged.CheckedAt = recovered.CheckedAt.Add(time.Second)
	api.applyCoreStatusTransition(context.Background(), bootChanged)
	if api.coreStatus.CoreBootID != "boot-c" {
		t.Fatalf("boot change not tracked: %q", api.coreStatus.CoreBootID)
	}
}
