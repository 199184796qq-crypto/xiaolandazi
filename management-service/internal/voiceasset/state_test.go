package voiceasset

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestVoiceCleanupIdentityAndLegacyCompatibility(t *testing.T) {
	scope := Scope{Tenant: 14, Plan: 4, Category: "mainline", Voice: Digest("full voice")}
	s := Stamp{Scope: scope}
	legacy := Digest("paragraph")
	if s.Key(legacy) != legacy {
		t.Fatal("untouched legacy cache was invalidated")
	}
	a := s
	a.PlanEpoch = 1
	b := a
	b.VoiceEpoch = 1
	if a.Key(legacy) == legacy || a.Key(legacy) == b.Key(legacy) {
		t.Fatal("clear generation collision")
	}
	for _, other := range []Scope{{Tenant: 15, Plan: 4, Category: "mainline", Voice: scope.Voice}, {Tenant: 14, Plan: 5, Category: "mainline", Voice: scope.Voice}, {Tenant: 14, Plan: 4, Category: "interaction", Voice: scope.Voice}, {Tenant: 14, Plan: 4, Category: "mainline", Voice: Digest("other")}} {
		x := a
		x.Scope = other
		if x.Key(legacy) == a.Key(legacy) {
			t.Fatal("cross scope key collision")
		}
	}
}
func TestVoiceCleanupMetadataAndGuard(t *testing.T) {
	legacy := map[string]any{"plan_id": float64(4), "purpose": "live_agent_formal_voice_timeline"}
	s, ok := MetadataStamp(14, "generated_voice", legacy)
	if !ok || s.PlanEpoch != 0 || s.Scope.Plan != 4 {
		t.Fatal(s, ok)
	}
	s.Scope.Voice = Digest("voice")
	s.PlanEpoch = 3
	s.Metadata(legacy)
	raw, _ := json.Marshal(legacy)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	got, ok := MetadataStamp(14, "generated_voice", decoded)
	if !ok || got != s {
		t.Fatal("metadata did not round trip", got)
	}
	if _, ok = MetadataStamp(15, "generated_voice", decoded); ok {
		t.Fatal("foreign tenant accepted")
	}
	if _, ok = MetadataStamp(14, "uploaded_audio", decoded); ok {
		t.Fatal("source recording included")
	}
	calls := 0
	ctx := WithStamp(context.Background(), s, func(context.Context, Stamp) error { calls++; return ErrCleared })
	if !errors.Is(Check(ctx), ErrCleared) || calls != 1 {
		t.Fatal("clear guard bypass")
	}
	if _, e := Bind(ctx, Digest("mapped voice")); !errors.Is(e, ErrCleared) {
		t.Fatal("old task silently rebound after clear")
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if !errors.Is(Check(cancelCtx), context.Canceled) {
		t.Fatal("cancelled task admitted")
	}
}
func TestVoiceCleanupValidationAndRetry(t *testing.T) {
	for _, scope := range []Scope{{}, {Tenant: 14, Plan: 4, Category: "anything"}, {Tenant: 14, Plan: 4, Category: "mainline", Voice: "../"}} {
		if scope.Validate() == nil {
			t.Fatal(scope)
		}
	}
	for i := -2; i < 100; i++ {
		d := RetryDelay(i)
		if d < time.Second || d > 5*time.Minute {
			t.Fatal("unbounded retry", d)
		}
	}
}
