package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestAudioPauseDisconnectResumePreservesWebSocketPresence(t *testing.T) {
	t.Parallel()
	var control atomic.Value
	control.Store("playing")
	var starts, cancels, connects, heartbeats atomic.Int64
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rooms/26/composite.pcm" {
			http.NotFound(w, r)
			return
		}
		starts.Add(1)
		// No media or ffmpeg is needed: cancellation must reach the actual
		// upstream HTTP request even while it is waiting for response headers.
		<-r.Context().Done()
		cancels.Add(1)
	}))
	defer core.Close()
	management := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Xiaozhi-Internal-Token") != "unit-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		switch r.URL.Path {
		case "/internal/v1/xiaozhi/provision":
			writeJSON(w, 200, provisioning{TenantID: 14, DeviceID: 7, RoomID: 26, State: "bound", AudioControl: control.Load().(string)})
		case "/internal/v1/xiaozhi/connection":
			var event struct {
				Phase string `json:"phase"`
			}
			if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
				t.Error(err)
			}
			if event.Phase == "connect" {
				connects.Add(1)
			}
			if event.Phase == "heartbeat" {
				heartbeats.Add(1)
			}
			writeJSON(w, 200, map[string]any{"applied": true, "display_status": "连续待机"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer management.Close()
	g := newGateway(config{managementBaseURL: management.URL, coreBaseURL: core.URL, internalToken: "unit-token", secret: "unit-secret"}, &bindingStore{data: map[string]int64{}})
	server := httptest.NewServer(g.handler())
	defer server.Close()
	const mac = "1c:29:04:31:0e:b8"
	headers := http.Header{"Device-Id": []string{mac}, "Client-Id": []string{"audio-control"}, "Authorization": []string{"Bearer " + g.deviceToken(mac, "audio-control")}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/xiaozhi/v1/", headers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5*deviceProbeInterval + 4*time.Second))
	if err := conn.WriteJSON(map[string]any{"type": "hello", "version": 1, "transport": "websocket"}); err != nil {
		t.Fatal(err)
	}
	waitStatus := func(want string) {
		t.Helper()
		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				t.Fatal("presence connection was closed", err)
			}
			if msg["type"] == "device_status" && msg["audio_control"] == want {
				return
			}
		}
	}
	waitCount := func(value *atomic.Int64, want int64) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for value.Load() < want && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if value.Load() != want {
			t.Fatalf("counter=%d want=%d", value.Load(), want)
		}
	}
	waitStatus("playing")
	waitCount(&starts, 1)
	control.Store("paused")
	waitStatus("paused")
	waitCount(&cancels, 1)
	control.Store("disconnected")
	waitStatus("disconnected")
	if starts.Load() != 1 {
		t.Fatal("stopped intent started room audio")
	}
	control.Store("playing")
	waitStatus("playing")
	waitCount(&starts, 2)
	if connects.Load() != 1 || heartbeats.Load() < 2 {
		t.Fatalf("audio controls replaced/expired real presence: connects=%d heartbeats=%d", connects.Load(), heartbeats.Load())
	}
}
