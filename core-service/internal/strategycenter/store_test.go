package strategycenter

import "testing"

func TestWelcomeNamedProbabilityDropsAsEntryFlowRises(t *testing.T) {
	store := New()
	low := store.Trigger(0, "interaction", "welcome_named", Signals{Entries30s: 2}, "same-event")
	high := store.Trigger(0, "interaction", "welcome_named", Signals{Entries30s: 80}, "same-event")
	if low.Probability <= high.Probability {
		t.Fatalf("expected low-flow named welcome probability > high-flow probability, low=%d high=%d", low.Probability, high.Probability)
	}
	if low.Probability != 18 || high.Probability != 2 {
		t.Fatalf("unexpected named welcome probabilities low=%d high=%d", low.Probability, high.Probability)
	}
}

func TestBatchWelcomeProbabilityAlsoDropsAtHighFlow(t *testing.T) {
	store := New()
	low := store.Trigger(0, "interaction", "welcome_batch", Signals{Entries30s: 2}, "same-event")
	high := store.Trigger(0, "interaction", "welcome_batch", Signals{Entries30s: 80}, "same-event")
	if low.Probability <= high.Probability {
		t.Fatalf("expected low-flow batch welcome probability > high-flow probability, low=%d high=%d", low.Probability, high.Probability)
	}
	if low.Probability != 16 || high.Probability != 9 {
		t.Fatalf("unexpected batch welcome probabilities low=%d high=%d", low.Probability, high.Probability)
	}
	if namedHigh := store.Trigger(0, "interaction", "welcome_named", Signals{Entries30s: 80}, "same-event"); high.Probability <= namedHigh.Probability {
		t.Fatalf("high-flow batch welcome should remain relatively preferred over named welcome, batch=%d named=%d", high.Probability, namedHigh.Probability)
	}
}

func TestPickIsStableForSameDecisionSeed(t *testing.T) {
	store := New()
	candidates := []string{"read_comment_softly", "hard_cut", "ask_controller", "thinking_pause", "repeat_confirm"}
	a := store.Pick(0, "interrupt", candidates, Signals{}, "decision-42")
	b := store.Pick(0, "interrupt", candidates, Signals{}, "decision-42")
	if a.Key == "" || a.Key != b.Key || a.Roll != b.Roll {
		t.Fatalf("selection must be stable for same seed: a=%#v b=%#v", a, b)
	}
}

func TestTenantInheritsLatestGlobalRulesAndKeepsCustomAddressing(t *testing.T) {
	store := New()
	global := DefaultPolicy()
	for i := range global.Rules {
		if global.Rules[i].Key == "reply_chat" {
			global.Rules[i].BaseProbability = 35
		}
	}
	store.Put(0, global)
	store.Put(14, Policy{
		TenantID:       14,
		Rules:          []Rule{{Category: "interaction", Key: "reply_chat", Enabled: true, BaseProbability: 99}},
		AddressingMode: "custom",
		Addressing: []AddressingOption{
			{Key: "custom_1", Text: "老哥", Enabled: true, Probability: 100, SystemDefault: false},
		},
	})

	resolved := store.Resolve(14)
	foundReplyChat := false
	for _, rule := range resolved.Rules {
		if rule.Key == "reply_chat" {
			foundReplyChat = true
			if rule.BaseProbability != 35 {
				t.Fatalf("tenant must inherit latest global reply_chat probability, got %d", rule.BaseProbability)
			}
		}
	}
	if !foundReplyChat {
		t.Fatal("reply_chat rule missing after tenant resolve")
	}
	if resolved.AddressingMode != "custom" || len(resolved.Addressing) != 1 || resolved.Addressing[0].Text != "老哥" {
		t.Fatalf("custom addressing should be preserved, got %#v", resolved)
	}
}

func TestTenantSystemAddressingFollowsLatestGlobalAddressing(t *testing.T) {
	store := New()
	global := DefaultPolicy()
	global.Addressing = []AddressingOption{{Key: "friend", Text: "朋友们", Enabled: true, Probability: 100, SystemDefault: true}}
	store.Put(0, global)
	store.Put(8, Policy{
		TenantID:       8,
		AddressingMode: "system",
		Addressing:     []AddressingOption{{Key: "old", Text: "旧称呼", Enabled: true, Probability: 100, SystemDefault: true}},
	})

	resolved := store.Resolve(8)
	if resolved.AddressingMode != "system" || len(resolved.Addressing) != 1 || resolved.Addressing[0].Text != "朋友们" {
		t.Fatalf("system addressing should follow latest global policy, got %#v", resolved)
	}
}

func TestAllowedHonorsEnabledConfiguredRules(t *testing.T) {
	store := New()
	if !store.Allowed(0, "resume", "BRIDGE") {
		t.Fatal("BRIDGE should be allowed by default")
	}
	if store.Allowed(0, "resume", "SWITCH_PLAN") {
		t.Fatal("disabled SWITCH_PLAN must not be allowed")
	}
}
