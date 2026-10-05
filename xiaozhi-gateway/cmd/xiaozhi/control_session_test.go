package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const controlTestMAC = "1c:29:04:31:0e:b8"

type controlHarness struct {
	g                  *gateway
	server             *httptest.Server
	backend            *httptest.Server
	events             chan map[string]any
	voiceCalls         atomic.Int64
	voiceRequestID     atomic.Value
	tenant             atomic.Int64
	voiceBlock         chan struct{}
	dispatchCalls      atomic.Int64
	semanticCalls      atomic.Int64
	dispatchHTTPStatus atomic.Int64
	dispatchBlocking   atomic.Bool
	dispatchBlock      chan struct{}
	voiceKind          atomic.Value
	voiceStatus        atomic.Value
	addressing         atomic.Value
	eventsHTTPStatus   atomic.Int64
	eventsStatus       atomic.Value
	eventsBilling      atomic.Value
	eventsBlocking     atomic.Bool
	eventsBlock        chan struct{}
}

func TestListenerBindingOnlyAllowsLocalDisplayAndAudioControls(t *testing.T) {
	for _, test := range []struct {
		role    string
		action  string
		allowed bool
	}{
		{role: "primary", action: "capture", allowed: true},
		{role: "listener", action: "volume", allowed: true},
		{role: "listener", action: "font_size", allowed: true},
		{role: "listener", action: "capture", allowed: false},
	} {
		if got := bindingRoleAllowsCommand(test.role, deviceCommand{Action: test.action}); got != test.allowed {
			t.Fatalf("role=%s action=%s allowed=%v, want %v", test.role, test.action, got, test.allowed)
		}
	}
}

func newControlHarness(t *testing.T, text string, block bool) *controlHarness {
	t.Helper()
	h := &controlHarness{events: make(chan map[string]any, 100), dispatchBlock: make(chan struct{}), eventsBlock: make(chan struct{})}
	h.tenant.Store(7)
	h.dispatchHTTPStatus.Store(200)
	h.voiceKind.Store("command")
	h.voiceStatus.Store("recognized")
	h.addressing.Store("neutral")
	h.eventsHTTPStatus.Store(200)
	h.eventsStatus.Store("")
	h.eventsBilling.Store("disabled")
	if block {
		h.voiceBlock = make(chan struct{})
	}
	h.backend = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Xiaozhi-Internal-Token") != "internal-unit-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		switch r.URL.Path {
		case "/internal/v1/xiaozhi/provision":
			writeJSON(w, 200, provisioning{DeviceID: 9, TenantID: h.tenant.Load(), State: "claimed", DeviceName: "客户自定义小蓝直播助手"})
		case "/internal/v1/xiaozhi/heartbeat":
			writeJSON(w, 200, map[string]string{"display_status": "连续待机"})
		case "/internal/v1/xiaozhi/voice":
			h.voiceCalls.Add(1)
			if r.Header.Get("X-Device-MAC") != controlTestMAC {
				t.Error("wrong device MAC")
			}
			h.voiceRequestID.Store(r.Header.Get("X-Request-ID"))
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				http.Error(w, "bad upload", 400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				http.Error(w, "bad upload", 400)
				return
			}
			defer file.Close()
			data, _ := io.ReadAll(file)
			if header.Filename != "command.ogg" || !bytes.HasPrefix(data, []byte("OggS")) || !bytes.Contains(data, []byte("OpusHead")) || !bytes.Contains(data, []byte("OpusTags")) {
				t.Error("voice was not valid Ogg framing")
			}
			if h.voiceBlock != nil {
				select {
				case <-h.voiceBlock:
				case <-r.Context().Done():
					return
				}
			}
			writeJSON(w, 200, map[string]any{"text": text, "request_id": r.Header.Get("X-Request-ID"), "event_id": 101, "kind": h.voiceKind.Load(), "status": h.voiceStatus.Load(), "speaker_addressing": h.addressing.Load(), "billing_mode": "disabled", "charged_beans": 0})
		case "/internal/v1/xiaozhi/command-events":
			var event map[string]any
			_ = json.NewDecoder(r.Body).Decode(&event)
			h.events <- event
			if h.eventsBlocking.Load() {
				select {
				case <-h.eventsBlock:
				case <-r.Context().Done():
					return
				}
			}
			if status := int(h.eventsHTTPStatus.Load()); status != 200 {
				http.Error(w, "receipt unavailable", status)
				return
			}
			state := event["status"]
			if override := h.eventsStatus.Load().(string); override != "" {
				state = override
			}
			writeJSON(w, 200, map[string]any{"request_id": event["request_id"], "event_id": 101, "status": state, "recorded": true, "billing_mode": h.eventsBilling.Load(), "charged_beans": 0})
		case "/internal/v1/xiaozhi/command-dispatch":
			h.dispatchCalls.Add(1)
			var input map[string]any
			_ = json.NewDecoder(r.Body).Decode(&input)
			if input["hardware_mac"] != controlTestMAC {
				t.Error("wrong dispatch identity")
			}
			if h.dispatchBlocking.Load() {
				select {
				case <-h.dispatchBlock:
				case <-r.Context().Done():
					return
				}
			}
			if status := int(h.dispatchHTTPStatus.Load()); status != 200 {
				http.Error(w, "dispatch denied", status)
				return
			}
			writeJSON(w, 200, map[string]any{"request_id": input["request_id"], "event_id": 101, "status": "executing", "dispatch_allowed": true, "billing_mode": "disabled", "charged_beans": 0})
		case "/internal/v1/xiaozhi/command-understand":
			h.semanticCalls.Add(1)
			var input map[string]any
			_ = json.NewDecoder(r.Body).Decode(&input)
			if input["text"] == "声音有点吵，收着点" {
				writeJSON(w, 200, map[string]any{"request_id": input["request_id"], "status": "understood", "action": "volume", "operation": "adjust", "value": -10, "message": "马上调低一点", "confidence": .98, "billing_mode": "disabled", "charged_beans": 0})
				return
			}
			writeJSON(w, 200, map[string]any{"request_id": input["request_id"], "status": "unsupported", "action": "", "operation": "", "value": nil, "message": "暂时不支持这个指令", "confidence": 1, "billing_mode": "disabled", "charged_beans": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	h.g = newGateway(config{managementBaseURL: h.backend.URL, internalToken: "internal-unit-token", secret: "private-unit-secret", publicWSURL: "wss://unit.example/xiaozhi/v1/"}, &bindingStore{data: map[string]int64{}})
	h.server = httptest.NewServer(h.g.handler())
	t.Cleanup(func() {
		h.server.Close()
		close(h.dispatchBlock)
		close(h.eventsBlock)
		if h.voiceBlock != nil {
			close(h.voiceBlock)
		}
		h.backend.Close()
	})
	return h
}

func TestManagedDispatchConflictDoesNotExecuteCommand(t *testing.T) {
	h := newControlHarness(t, "声音大一点", false)
	h.dispatchHTTPStatus.Store(409)
	peer := h.connect(t, true)
	peer.voice(t, "ctl-dispatch-denied-0001")
	status := peer.next(t, "control_status", "error")
	if status["request_id"] != "ctl-dispatch-denied-0001" || h.dispatchCalls.Load() != 1 {
		t.Fatal(status, h.dispatchCalls.Load())
	}
	select {
	case message := <-peer.messages:
		if message["type"] == "device_control" {
			t.Fatal("denied dispatch executed", message)
		}
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAbortDuringDispatchCannotExecuteLateGrant(t *testing.T) {
	h := newControlHarness(t, "声音大一点", false)
	h.dispatchBlocking.Store(true)
	peer := h.connect(t, true)
	const id = "ctl-dispatch-abort-0001"
	peer.voice(t, id)
	deadline := time.Now().Add(time.Second)
	for h.dispatchCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if h.dispatchCalls.Load() != 1 {
		t.Fatal("dispatch not reached")
	}
	peer.send(t, map[string]any{"type": "abort", "request_id": id})
	status := peer.next(t, "control_status", "error")
	if status["message"] != "已取消本次指令" {
		t.Fatal(status)
	}
	select {
	case message := <-peer.messages:
		if message["type"] == "device_control" {
			t.Fatal("late dispatch executed after abort", message)
		}
	case <-time.After(50 * time.Millisecond):
	}
}

type controlPeer struct {
	conn     *websocket.Conn
	messages chan map[string]any
	failures chan error
}

func (h *controlHarness) connect(t *testing.T, enabled bool, feedback ...bool) *controlPeer {
	t.Helper()
	headers := http.Header{"Device-Id": []string{controlTestMAC}, "Client-Id": []string{"client-unit"}, "Authorization": []string{"Bearer " + h.g.deviceToken(controlTestMAC, "client-unit")}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.server.URL, "http")+"/xiaozhi/v1/", headers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	peer := &controlPeer{conn: conn, messages: make(chan map[string]any, 100), failures: make(chan error, 1)}
	go func() {
		for {
			var message map[string]any
			if err := conn.ReadJSON(&message); err != nil {
				peer.failures <- err
				return
			}
			peer.messages <- message
		}
	}()
	withFeedback := len(feedback) > 0 && feedback[0]
	if err := conn.WriteJSON(map[string]any{"type": "hello", "version": 1, "transport": "websocket", "audio_params": map[string]any{"format": "opus", "sample_rate": 16000, "channels": 1}, "features": map[string]bool{"device_control": enabled, "camera_capture": enabled, "device_feedback": withFeedback}}); err != nil {
		t.Fatal(err)
	}
	peer.next(t, "hello", "")
	status := peer.next(t, "device_status", "")
	if status["device_name"] != "客户自定义小蓝直播助手" {
		t.Fatal("gateway changed a customer-owned device name", status)
	}
	return peer
}

func (peer *controlPeer) send(t *testing.T, message any) {
	t.Helper()
	if err := peer.conn.WriteJSON(message); err != nil {
		t.Fatal(err)
	}
}
func (peer *controlPeer) next(t *testing.T, kind, state string) map[string]any {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case message := <-peer.messages:
			if message["type"] == kind && (state == "" || message["state"] == state) {
				return message
			}
		case err := <-peer.failures:
			t.Fatal("websocket closed unexpectedly", err)
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s/%s", kind, state)
		}
	}
}

func (peer *controlPeer) voice(t *testing.T, id string) {
	t.Helper()
	peer.send(t, map[string]any{"type": "listen", "state": "detect", "text": "小蓝"})
	peer.send(t, map[string]any{"type": "listen", "state": "start", "mode": "auto", "request_id": id})
	status := peer.next(t, "control_status", "listening")
	if status["request_id"] != id {
		t.Fatal("request ID changed", status)
	}
	for i := 0; i < 5; i++ {
		if err := peer.conn.WriteMessage(websocket.BinaryMessage, []byte{0xf8, 0xff, 0xfe}); err != nil {
			t.Fatal(err)
		}
	}
	peer.send(t, map[string]any{"type": "listen", "state": "stop", "request_id": id})
}

func TestManagedVoiceControlRoundTripAndActualACK(t *testing.T) {
	h := newControlHarness(t, "小蓝，音量调整到30%", false)
	peer := h.connect(t, true)
	const id = "ctl-round-trip-0001"
	peer.voice(t, id)
	command := peer.next(t, "device_control", "")
	if command["request_id"] != id || command["action"] != "volume" || command["operation"] != "set" || command["value"] != float64(30) {
		t.Fatal(command)
	}
	if h.voiceRequestID.Load() != id || h.voiceCalls.Load() != 1 {
		t.Fatal("wrong internal ASR identity")
	}
	peer.send(t, map[string]any{"type": "device_control_ack", "request_id": "wrong-id", "action": "volume", "status": "success", "value": 30})
	peer.send(t, map[string]any{"type": "device_control_ack", "request_id": id, "action": "font_size", "status": "success", "value": 1})
	select {
	case status := <-peer.messages:
		if status["state"] == "done" {
			t.Fatal("forged ACK accepted", status)
		}
	case <-time.After(50 * time.Millisecond):
	}
	peer.send(t, map[string]any{"type": "device_control_ack", "request_id": id, "action": "volume", "status": "success", "value": 30})
	status := peer.next(t, "control_status", "done")
	if status["request_id"] != id || status["message"] != "音量已调整到30%" {
		t.Fatal(status)
	}
	select {
	case event := <-h.events:
		if event["request_id"] != id || event["status"] != "succeeded" || event["value"] != float64(30) {
			t.Fatal(event)
		}
	case <-time.After(time.Second):
		t.Fatal("no command audit")
	}
	peer.send(t, map[string]any{"type": "device_control_ack", "request_id": id, "action": "volume", "status": "success", "value": 30})
	select {
	case event := <-h.events:
		t.Fatal("duplicate ACK recorded twice", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestManagedNaturalLanguageFallsBackToSemanticAllowlist(t *testing.T) {
	h := newControlHarness(t, "声音有点吵，收着点", false)
	peer := h.connect(t, true)
	const id = "ctl-semantic-roundtrip-0001"
	peer.voice(t, id)
	command := peer.next(t, "device_control", "")
	if command["request_id"] != id || command["action"] != "volume" || command["operation"] != "adjust" || command["value"] != float64(-10) {
		t.Fatal(command)
	}
	if h.semanticCalls.Load() != 1 || h.dispatchCalls.Load() != 1 {
		t.Fatal("semantic command did not pass the full authorization path", h.semanticCalls.Load(), h.dispatchCalls.Load())
	}
}

func TestManagedAbortDoesNotCloseWebSocketOrExecuteLateASR(t *testing.T) {
	h := newControlHarness(t, "音量调整到30%", true)
	peer := h.connect(t, true)
	const id = "ctl-abort-test-0001"
	peer.voice(t, id)
	peer.next(t, "control_status", "processing")
	peer.send(t, map[string]any{"type": "abort", "request_id": id})
	status := peer.next(t, "control_status", "error")
	if status["message"] != "已取消本次指令" {
		t.Fatal(status)
	}
	peer.send(t, map[string]any{"type": "listen", "state": "start", "request_id": "ctl-still-connected-0002"})
	peer.next(t, "control_status", "error") // Rate-limit response proves the same socket remains live.
	select {
	case message := <-peer.messages:
		if message["type"] == "device_control" {
			t.Fatal("aborted ASR executed", message)
		}
	case <-time.After(50 * time.Millisecond):
	}
}

func TestOldFirmwareDoesNotUploadASROrRequireControlACK(t *testing.T) {
	h := newControlHarness(t, "音量30%", false)
	peer := h.connect(t, false)
	peer.send(t, map[string]any{"type": "listen", "state": "start", "request_id": "ctl-old-fw-0001"})
	status := peer.next(t, "control_status", "error")
	if !strings.Contains(status["message"].(string), "更新") {
		t.Fatal(status)
	}
	_ = peer.conn.WriteMessage(websocket.BinaryMessage, []byte{0xf8, 0xff, 0xfe})
	peer.send(t, map[string]any{"type": "listen", "state": "stop"})
	peer.send(t, map[string]any{"type": "abort"})
	if h.voiceCalls.Load() != 0 {
		t.Fatal("legacy firmware billed ASR")
	}
}

func TestControlRegistryOwnershipRateLimitsAndRequestReplay(t *testing.T) {
	registry := newControlRegistry()
	p := provisioning{DeviceID: 9, TenantID: 7, State: "claimed"}
	now := time.Now()
	lease, err := registry.acquire(controlTestMAC, "client", "ctl-limit-0001", p, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.acquire(controlTestMAC, "other-client", "ctl-limit-0002", p, now.Add(3*time.Second)); err == nil {
		t.Fatal("concurrent WS command accepted")
	}
	registry.release(lease)
	if _, err := registry.acquire(controlTestMAC, "client", "ctl-limit-0001", p, now.Add(3*time.Second)); err == nil {
		t.Fatal("replayed request ID accepted")
	}
	if _, err := registry.acquire(controlTestMAC, "client", "ctl-limit-0002", p, now.Add(time.Second)); err == nil {
		t.Fatal("cooldown bypassed")
	}
	if _, err := registry.acquire("other", "client", "ctl-limit-0002", provisioning{DeviceID: 9, State: "claimable"}, now); err == nil {
		t.Fatal("unclaimed device accepted")
	}
	if _, err := registry.acquire("other", "client", strings.Repeat("a", 97), p, now); err == nil {
		t.Fatal("oversize request ID accepted")
	}
}

func TestControlListenAndACKTimeoutRestoreDelivery(t *testing.T) {
	// A real WS pair lets the controller emit status events without mocking away
	// its local audio gating behavior; Core is not even part of this control path.
	connections := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err == nil {
			connections <- conn
		}
	}))
	defer server.Close()
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	serverConn := <-connections
	defer serverConn.Close()
	h := newControlHarness(t, "音量30%", false)
	writer := &lockedConn{conn: serverConn}
	control := newTerminalControlSession(context.Background(), h.g, writer, controlTestMAC, "client", 16000, true, true)
	control.start(provisioning{DeviceID: 9, TenantID: 7, State: "claimed"}, "ctl-timeout-0001", time.Now())
	if !writer.audioPaused {
		t.Fatal("local delivery not paused")
	}
	control.tick(time.Now().Add(9 * time.Second))
	if control.state != "" || writer.audioPaused || h.voiceCalls.Load() != 0 {
		t.Fatal("empty listen timeout failed")
	}
	control.start(provisioning{DeviceID: 9, TenantID: 7, State: "claimed"}, "ctl-timeout-0002", time.Now().Add(3*time.Second))
	control.command = commandValue("volume", "set", 30)
	control.state = "pending"
	control.deadline = time.Now().Add(-time.Second)
	control.tick(time.Now())
	if control.state != "" || writer.audioPaused {
		t.Fatal("ACK timeout failed to restore delivery")
	}
	control.acknowledge(controlMessage{RequestID: "ctl-timeout-0002", Action: "volume", Status: "success", Value: json.RawMessage("30")})
	if control.state != "" {
		t.Fatal("late ACK restarted command")
	}
	control.start(provisioning{DeviceID: 9, TenantID: 7, State: "claimed"}, "ctl-timeout-0003", time.Now().Add(6*time.Second))
	control.appendPacket([]byte{0xf8, 0xff, 0xfe})
	control.process()
	if control.state != "processing" || writer.audioPaused {
		t.Fatal("ASR wait incorrectly suppresses downlink")
	}
	control.abort()
}

func TestACKCannotClaimUnappliedSettingsOrUnuploadedCapture(t *testing.T) {
	connections := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err == nil {
			connections <- conn
		}
	}))
	defer server.Close()
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	serverConn := <-connections
	defer serverConn.Close()
	h := newControlHarness(t, "音量30%", false)
	writer := &lockedConn{conn: serverConn}
	for index, test := range []struct {
		action    string
		operation string
		value     json.RawMessage
	}{
		{"volume", "set", json.RawMessage("null")}, {"volume", "set", json.RawMessage("30.5")}, {"volume", "set", json.RawMessage("70")}, {"volume", "set", json.RawMessage("101")}, {"volume", "unmute", json.RawMessage("0")}, {"font_size", "query", json.RawMessage("3")}, {"font_size", "reset", json.RawMessage("2")}, {"capture", "set", nil},
	} {
		control := newTerminalControlSession(context.Background(), h.g, writer, controlTestMAC, "client", 16000, true, true)
		id := fmt.Sprintf("ctl-invalid-ack-%04d", index)
		control.start(provisioning{DeviceID: 9, TenantID: 7, State: "claimed"}, id, time.Now().Add(time.Duration(index*3)*time.Second))
		control.state = "pending"
		if test.action == "volume" && test.operation == "set" {
			control.command = commandValue("volume", "set", 30)
		} else {
			control.command = deviceCommand{Action: test.action, Operation: test.operation}
		}
		control.acknowledge(controlMessage{RequestID: id, Action: test.action, Status: "success", Value: test.value})
		select {
		case event := <-h.events:
			if event["status"] != "failed" || event["request_id"] != id {
				t.Fatal("invalid ACK reported success", event)
			}
		case <-time.After(time.Second):
			t.Fatal("no failed ACK audit")
		}
		if control.state != "" {
			t.Fatal("invalid ACK left command pending")
		}
	}
}
