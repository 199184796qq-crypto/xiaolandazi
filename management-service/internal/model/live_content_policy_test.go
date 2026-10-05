package model

import (
	"testing"
	"time"
)

func TestContentPolicyTimeConstraints(t *testing.T) {
	base := DefaultLiveContentPolicy()
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*LiveContentPolicy)
	}{
		{"unknown mode", func(p *LiveContentPolicy) { p.ContentMode = "custom" }},
		{"zero faq lifetime", func(p *LiveContentPolicy) { p.FAQTTLSeconds = 0 }},
		{"refresh after expiry", func(p *LiveContentPolicy) { p.RefreshAheadSeconds = p.FAQTTLSeconds }},
		{"repetition never possible", func(p *LiveContentPolicy) { p.MinRepeatSeconds = p.FAQTTLSeconds }},
		{"unbounded versions", func(p *LiveContentPolicy) { p.FAQVariantCount = 100 }},
		{"bad replacement", func(p *LiveContentPolicy) { p.ReplacementPercent = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := base
			test.change(&p)
			if p.Validate() == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
}

func TestContentPolicyRecordingNeverRegenerated(t *testing.T) {
	p := DefaultLiveContentPolicy()
	p.ContentMode = LiveContentUserAudio
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	refresh, expiry := p.ContentExpiry("mainline", now)
	if !refresh.IsZero() || !expiry.IsZero() {
		t.Fatal("user recording got AI expiry")
	}
	refresh, expiry = p.ContentExpiry("faq", now)
	if !expiry.Equal(now.Add(2*time.Hour)) || !refresh.Equal(now.Add(110*time.Minute)) {
		t.Fatal("interaction expiry does not use configured TTL")
	}
	p.ContentMode = LiveContentAIDynamic
	refresh, expiry = p.ContentExpiry("mainline", now)
	if !expiry.Equal(now.Add(2*time.Hour)) || !refresh.Equal(now.Add(110*time.Minute)) {
		t.Fatal("mainline refresh window incorrect")
	}
}

func TestContentPolicyTwoHourQuarterDefaults(t *testing.T) {
	p := DefaultLiveContentPolicy()
	if p.MainlineTTLSeconds != 7200 || p.FAQTTLSeconds != 7200 || p.ReplacementPercent != 25 || !p.AutoRefreshEnabled {
		t.Fatalf("unexpected refresh defaults: %+v", p)
	}
}

func TestContentModeDefaultsAndRevocation(t *testing.T) {
	for _, mode := range []string{"", LiveContentAIPregenerated, LiveContentUserAudio, LiveContentAIDynamic, "bad"} {
		access := ResolveLiveContentMode(mode, false, "operator")
		if len(access.AvailableModes) != 2 || access.DynamicAuthorized || access.AuthorizationSource != "default" || access.ContentMode == LiveContentAIDynamic {
			t.Fatalf("advanced leaked without entitlement: %#v", access)
		}
		if mode == LiveContentUserAudio && access.ContentMode != mode {
			t.Fatal("recorded audio default was removed")
		}
	}
	access := ResolveLiveContentMode(LiveContentAIDynamic, true, "operator")
	if len(access.AvailableModes) != 3 || access.ContentMode != LiveContentAIDynamic || access.AuthorizationSource != "operator" {
		t.Fatalf("grant not effective: %#v", access)
	}
	revoked := ResolveLiveContentMode(access.ContentMode, false, "operator")
	if revoked.ContentMode != LiveContentAIPregenerated {
		t.Fatal("revoked advanced mode stayed selected")
	}
}
