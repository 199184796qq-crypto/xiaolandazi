package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func maintenanceTestProfile() *deviceMaintenanceProfile {
	return &deviceMaintenanceProfile{Board: "bread-compact-wifi-s3cam", Chip: "esp32s3", FlashBytes: 16 << 20, PartitionLayout: "v2-16m", FirmwareVersion: "2.2.7", FirmwareSHA256: strings.Repeat("a", 64), BootID: strings.Repeat("b", 32), Storage: "partition"}
}

func TestDeviceReadEndKeepsCauseWithoutPeerText(t *testing.T) {
	for _, tc := range []struct {
		err    error
		reason string
		code   int
	}{
		{fmt.Errorf("wrapped: %w", &websocket.CloseError{Code: 1008, Text: "private remote payload"}), "peer_close", 1008},
		{os.ErrDeadlineExceeded, "read_timeout", 0},
		{io.EOF, "transport_eof", 0},
		{fmt.Errorf("private URL or credential"), "transport_read_error", 0},
	} {
		reason, code := deviceReadEnd(tc.err)
		if reason != tc.reason || code != tc.code {
			t.Fatalf("category=%s code=%d", reason, code)
		}
	}
}

func TestMaintenanceProfileCompatibility(t *testing.T) {
	if validMaintenanceProfile(nil) {
		t.Fatal("legacy profile accepted")
	}
	p := maintenanceTestProfile()
	if !validMaintenanceProfile(p) {
		t.Fatal("supported profile rejected")
	}
	p.FlashBytes = 8 << 20
	if validMaintenanceProfile(p) {
		t.Fatal("wrong flash accepted")
	}
	p = maintenanceTestProfile()
	p.FirmwareSHA256 = "not-a-hash"
	if validMaintenanceProfile(p) {
		t.Fatal("bad build identity accepted")
	}
}

func TestStartupFirmwareOfferLegacyAndFailureAreOptional(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/internal/v1/xiaozhi/firmware-check" || r.Header.Get("X-Xiaozhi-Internal-Token") != "test-internal" {
			t.Error("unexpected maintenance request")
		}
		var body struct {
			HardwareMAC   string                    `json:"hardware_mac"`
			DeviceProfile *deviceMaintenanceProfile `json:"device_profile"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || !validMaintenanceProfile(body.DeviceProfile) {
			t.Error("nested profile missing")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	g := &gateway{cfg: config{managementBaseURL: server.URL, internalToken: "test-internal"}}
	if g.startupFirmwareOffer(context.Background(), "device", nil) != nil || calls.Load() != 0 {
		t.Fatal("legacy device requested maintenance")
	}
	if g.startupFirmwareOffer(context.Background(), "device", maintenanceTestProfile()) != nil || calls.Load() != 1 {
		t.Fatal("optional failure not ignored")
	}
}

func TestMaintenanceOffersValidateSafetyMetadata(t *testing.T) {
	p := maintenanceTestProfile()
	f := &deviceFirmwareOffer{Version: "2.2.8", URL: "https://example.test/api/v1/device-firmware/image?grant=limited", SHA256: strings.Repeat("c", 64), Size: 3000000, Board: p.Board, Chip: p.Chip, FlashBytes: p.FlashBytes, PartitionLayout: p.PartitionLayout}
	if !validFirmwareOffer(f, p) {
		t.Fatal("valid offer rejected")
	}
	f.URL = "http://example.test/image"
	if validFirmwareOffer(f, p) {
		t.Fatal("insecure offer accepted")
	}
	f.URL = "https://example.test/image"
	f.PartitionLayout = "other"
	if validFirmwareOffer(f, p) {
		t.Fatal("wrong layout accepted")
	}
	d := &deviceDiagnosticsGrant{UploadURL: "https://example.test/api/v1/device-diagnostics/upload", Token: "limited", OwnerEpoch: strings.Repeat("d", 64), ExpiresAt: time.Now().Add(10 * time.Minute).Unix(), MaxBatchBytes: 32768, MaxBatchEvents: 128}
	if !validDiagnosticsGrant(d) {
		t.Fatal("valid grant rejected")
	}
	d.ExpiresAt = time.Now().Add(-time.Minute).Unix()
	if validDiagnosticsGrant(d) {
		t.Fatal("expired grant accepted")
	}
}

func TestMaintenanceWorkerCancellationDoesNotNeedWriter(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	g := &gateway{cfg: config{managementBaseURL: server.URL, internalToken: "test-internal"}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		g.runDeviceMaintenance(ctx, nil, "device", "connection", maintenanceTestProfile())
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request not started")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("optional request did not cancel")
	}
}

func TestMaintenanceNeverWritesIntoActiveAudio(t *testing.T) {
	// A nil socket would panic if optional traffic reached the transport.
	writer := &lockedConn{lastAudioWrite: time.Now()}
	writer.tryMaintenance(context.Background(), deviceMaintenanceResponse{})
	if !writer.mu.TryLock() {
		t.Fatal("audio writer lock retained")
	}
	writer.mu.Unlock()
}
