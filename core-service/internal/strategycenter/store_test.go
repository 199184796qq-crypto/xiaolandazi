package strategycenter

import (
	"testing"
	"time"
)

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

func TestInteractionPreferenceZeroDisablesAutomaticCategory(t *testing.T) {
	store := New()
	roomID := int64(15)
	store.PutRoomInteractionPreferences(roomID, RoomInteractionPreferences{
		RoomID:               roomID,
		OverallInteraction:   50,
		QuestionPreference:   0,
		WelcomePreference:    50,
		EngagementPreference: 50,
		ChatPreference:       50,
		ConversionPreference: 50,
		AutoHeat:             true,
	})
	if got := store.InteractionPreferenceFactor(roomID, "question"); got != 0 {
		t.Fatalf("question preference 0 must disable automatic interaction, factor=%v", got)
	}
	if got := store.AdjustInteractionWeight(roomID, "reply_chat", 50); got <= 0 {
		t.Fatalf("chat should remain enabled when only question is 0, weight=%d", got)
	}

	store.PutRoomInteractionPreferences(roomID, RoomInteractionPreferences{
		RoomID:               roomID,
		OverallInteraction:   0,
		QuestionPreference:   100,
		WelcomePreference:    100,
		EngagementPreference: 100,
		ChatPreference:       100,
		ConversionPreference: 100,
		AutoHeat:             true,
	})
	for _, kind := range []string{"question", "welcome", "engagement", "chat", "conversion"} {
		if got := store.InteractionPreferenceFactor(roomID, kind); got != 0 {
			t.Fatalf("overall preference 0 must disable %s, factor=%v", kind, got)
		}
	}
}

func TestRuleWeightReturnsConfiguredAndRuntimeAdjustedWeight(t *testing.T) {
	store := New()
	configured, effective, enabled := store.RuleWeight(0, "interaction", "reply_like", Signals{Likes30s: 300})
	if !enabled {
		t.Fatal("reply_like should be enabled by default")
	}
	if configured != 8 || effective != 2 {
		t.Fatalf("reply_like weights configured=%d effective=%d want 8/2", configured, effective)
	}

	policy := DefaultPolicy()
	for i := range policy.Rules {
		if policy.Rules[i].Category == "interaction" && policy.Rules[i].Key == "reply_like" {
			policy.Rules[i].Enabled = false
			policy.Rules[i].BaseProbability = 0
		}
	}
	store.Put(0, policy)
	configured, effective, enabled = store.RuleWeight(0, "interaction", "reply_like", Signals{Likes30s: 300})
	if enabled || configured != 0 || effective != 0 {
		t.Fatalf("disabled reply_like should return 0/0/false, got %d/%d/%v", configured, effective, enabled)
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

func TestResumeDiversityPenalizesRecentRepeatAndBoostsCoverageDebt(t *testing.T) {
	store := New()
	roomID := int64(15)
	stageID := "live:15:stage-a"
	startedAt := time.Date(2026, 9, 29, 18, 0, 0, 0, time.UTC)
	candidates := []string{"DIRECT", "BRIDGE", "FUSION_SKIP"}

	first := store.PickForRoom(roomID, stageID, 0, "resume", candidates, Signals{}, "first")
	if first.Key == "" {
		t.Fatal("first resume selection missing")
	}
	store.RecordSelection(roomID, stageID, startedAt, first)

	second := store.PickForRoom(roomID, stageID, 0, "resume", candidates, Signals{}, "second")
	if len(second.Candidates) != len(candidates) {
		t.Fatalf("resume diversity must not add/remove semantic candidates, got %#v", second.Candidates)
	}
	seen := map[string]Candidate{}
	for _, candidate := range second.Candidates {
		seen[candidate.Key] = candidate
	}
	recent := seen[first.Key]
	if recent.RepeatPenalty >= 1 {
		t.Fatalf("recently selected strategy must be penalized, first=%s candidate=%#v", first.Key, recent)
	}
	boosted := false
	for _, key := range candidates {
		if key == first.Key {
			continue
		}
		candidate := seen[key]
		if candidate.CoverageDebt >= 1 && candidate.DiversityBoost > 1 && candidate.Weight > candidate.BaseWeight {
			boosted = true
		}
	}
	if !boosted {
		t.Fatalf("at least one eligible-but-missed strategy should receive coverage boost: %#v", second.Candidates)
	}
}

func TestResumeDiversityStatsTrackEligibilitySelectionAndResetByStage(t *testing.T) {
	store := New()
	roomID := int64(16)
	stageA := "live:16:stage-a"
	startedAt := time.Date(2026, 9, 29, 18, 0, 0, 0, time.UTC)
	candidates := []string{"DIRECT", "BRIDGE"}

	selection := store.PickForRoom(roomID, stageA, 0, "resume", candidates, Signals{}, "stats")
	store.RecordSelection(roomID, stageA, startedAt, selection)
	stats := store.StageStats(roomID)
	byKey := map[string]ProbabilityStat{}
	for _, item := range stats.Items {
		if item.Category == "resume" {
			byKey[item.Key] = item
		}
	}
	if len(byKey) != 2 {
		t.Fatalf("resume stats=%#v", byKey)
	}
	for _, key := range candidates {
		item := byKey[key]
		if item.EligibleCount != 1 {
			t.Fatalf("%s eligible_count=%d want 1", key, item.EligibleCount)
		}
		if key == selection.Key {
			if item.SelectedCount != 1 || item.CoverageDebt != 0 || item.LastSelectedAt.IsZero() {
				t.Fatalf("selected stat unexpected for %s: %#v", key, item)
			}
		} else if item.SelectedCount != 0 || item.CoverageDebt != 1 || item.ConsecutiveMiss != 1 {
			t.Fatalf("missed stat unexpected for %s: %#v", key, item)
		}
	}

	stageB := "live:16:stage-b"
	fresh := store.PickForRoom(roomID, stageB, 0, "resume", candidates, Signals{}, "fresh")
	for _, candidate := range fresh.Candidates {
		if candidate.CoverageDebt != 0 || candidate.RepeatPenalty != 1 || candidate.DiversityBoost != 1 {
			t.Fatalf("new live stage must reset diversity runtime state: %#v", candidate)
		}
	}
}
