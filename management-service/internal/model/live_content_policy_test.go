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

func TestContentModeAvailableByDefaultPreservesSelection(t *testing.T) {
	for _, legacyGrant := range []bool{false, true} {
		for _, mode := range []string{"", LiveContentAIPregenerated, LiveContentUserAudio, LiveContentAIDynamic, "bad"} {
			access := ResolveLiveContentMode(mode, legacyGrant, "operator")
			if len(access.AvailableModes) != 3 || !access.DynamicAuthorized || access.AuthorizationSource != "default" {
				t.Fatalf("dynamic mode requires a separate grant: %#v", access)
			}
			want := mode
			if mode == "" || mode == "bad" {
				want = LiveContentAIPregenerated
			}
			if access.ContentMode != want {
				t.Fatalf("selection changed: got %s, want %s", access.ContentMode, want)
			}
		}
	}
}
