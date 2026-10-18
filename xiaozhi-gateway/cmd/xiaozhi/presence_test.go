package main

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestDevicePresenceRejectsUnmatchedAndReplayedPong(t *testing.T) {
	now := time.Now()
	p := newDevicePresence(now)
	nonce := p.challenge("connection")
	if p.pong("", now.Add(time.Second)) || p.pong("other", now.Add(time.Second)) || p.seen() != now {
		t.Fatal("unproven timer refreshed presence")
	}
	if !p.pong(nonce, now.Add(2*time.Second)) {
		t.Fatal("valid device response rejected")
	}
	if p.pong(nonce, now.Add(5*time.Second)) || p.seen() != now.Add(2*time.Second) {
		t.Fatal("replayed pong refreshed presence")
	}
}

func TestDeviceShutdownRequiresCurrentConnection(t *testing.T) {
	h := newControlHarness(t, "", false)
	peer := h.connect(t, false)
	var connected map[string]any
	select {
	case connected = <-h.connectionEvents:
	case <-time.After(time.Second):
		t.Fatal("connection was not registered")
	}
	peer.send(t, map[string]any{"type": "device_shutdown", "session_id": "other"})
	if ack := peer.next(t, "device_shutdown_ack", ""); ack["success"] != false {
		t.Fatal("foreign shutdown accepted", ack)
	}
	for len(h.connectionEvents) > 0 {
		event := <-h.connectionEvents
		if event["phase"] == "shutdown" {
			t.Fatal("foreign shutdown reached management")
		}
	}
	peer.send(t, map[string]any{"type": "device_shutdown", "session_id": connected["connection_id"]})
	if ack := peer.next(t, "device_shutdown_ack", ""); ack["success"] != true {
		t.Fatal("shutdown was not acknowledged", ack)
	}
	select {
	case <-peer.failures:
	case <-time.After(time.Second):
		t.Fatal("shutdown kept socket open")
	}
	found := false
	for len(h.connectionEvents) > 0 {
		event := <-h.connectionEvents
		if event["phase"] == "shutdown" {
			found = true
			if event["connection_id"] != connected["connection_id"] || event["hardware_mac"] != controlTestMAC {
				t.Fatal("wrong shutdown scope", event)
			}
		}
	}
	if !found {
		t.Fatal("authenticated shutdown did not reach management")
	}
}

func TestNoPongDoesNotCreateSyntheticHeartbeats(t *testing.T) {
	t.Parallel()
	h := newControlHarness(t, "", false)
	peer := h.connect(t, false)
	peer.conn.SetPingHandler(func(string) error { return nil })
	// A TCP connection that stays open while the box stops responding must
	// expire independently of the kernel's TCP failure detection.
	select {
	case <-peer.failures:
	case <-time.After(deviceResponseTimeout + 3*time.Second):
		t.Fatal("half-open connection never expired")
	}
	heartbeats := 0
	for len(h.connectionEvents) > 0 {
		event := <-h.connectionEvents
		if event["phase"] == "heartbeat" {
			heartbeats++
		}
	}
	// One initial response can race with installing the test ping handler;
	// subsequent gateway timer ticks must never synthesize responses.
	if heartbeats > 1 {
		t.Fatalf("unanswered connection generated %d heartbeats", heartbeats)
	}
}

func TestOldFirmwarePongKeepsPresenceAlive(t *testing.T) {
	t.Parallel()
	h := newControlHarness(t, "", false)
	peer := h.connect(t, false)
	// An immediate pong may share the connect timestamp and is then correctly
	// coalesced. Allow two periodic probes for two strictly newer reports.
	deadline := time.After(2*deviceProbeInterval + 2*time.Second)
	count := 0
	for count < 2 {
		select {
		case event := <-h.connectionEvents:
			if event["phase"] == "heartbeat" {
				count++
			}
		case err := <-peer.failures:
			t.Fatal("compatible firmware disconnected", err)
		case <-deadline:
			t.Fatal("device responses were not reported")
		}
	}
	if err := peer.conn.WriteControl(websocket.PongMessage, []byte("forged"), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
}
