package main

import (
	"testing"
	"time"
)

func TestDelayedPongSurvivesNewProbeAndCannotReplay(t *testing.T) {
	now := time.Now()
	p := newDevicePresence(now)
	first := p.challengeAt("current", now)
	second := p.challengeAt("current", now.Add(5*time.Second))
	if !p.pong(first, now.Add(7*time.Second)) {
		t.Fatal("a newer challenge rejected a genuine delayed response")
	}
	if p.pong(first, now.Add(8*time.Second)) || p.pong("foreign-1", now.Add(9*time.Second)) {
		t.Fatal("replayed/foreign response refreshed presence")
	}
	if !p.pong(second, now.Add(11*time.Second)) || !p.seen().Equal(now.Add(11*time.Second)) {
		t.Fatal("current response did not refresh the real receipt time")
	}
}

func TestExpiredPongAndChallengeFloodCannotExtendPresence(t *testing.T) {
	now := time.Now()
	p := newDevicePresence(now)
	first := p.challengeAt("current", now)
	if p.pong(first, now.Add(deviceResponseTimeout)) || !p.seen().Equal(now) {
		t.Fatal("expired response refreshed presence")
	}
	for i := 0; i < 1000; i++ {
		p.challengeAt("current", now.Add(deviceResponseTimeout))
	}
	if len(p.pending) > int(deviceResponseTimeout/deviceProbeInterval)+1 {
		t.Fatal("challenge window grew without bound")
	}
}

func TestConsecutiveSlowPongsKeepGenuineConnectionAlive(t *testing.T) {
	now := time.Now()
	p := newDevicePresence(now)
	var probes []string
	for i := 0; i < 12; i++ {
		at := now.Add(time.Duration(i) * deviceProbeInterval)
		probes = append(probes, p.challengeAt("current", at))
		if i >= 2 && !p.pong(probes[i-2], at) {
			t.Fatalf("ten-second response rejected after %d probes", i)
		}
	}
	if !p.seen().Equal(now.Add(11 * deviceProbeInterval)) {
		t.Fatal("slow but genuine peer did not remain live")
	}
}

func TestAudioControlRequiresPlayingIntentAndRoom(t *testing.T) {
	for _, tc := range []struct {
		state, control string
		room           int64
		allowed        bool
	}{
		{"bound", "", 26, true}, {"bound", "playing", 26, true},
		{"bound", "paused", 26, false}, {"bound", "disconnected", 26, false},
		{"bound", "unknown", 26, false}, {"claimed", "playing", 26, false},
		{"bound", "playing", 0, false},
	} {
		if got := deviceAudioAllowed(provisioning{State: tc.state, AudioControl: tc.control, RoomID: tc.room}); got != tc.allowed {
			t.Fatalf("%+v: allowed=%v", tc, got)
		}
	}
}
