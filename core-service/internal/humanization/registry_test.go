package humanization

import (
	"testing"
	"time"
)

func baseContext() Context {
	return Context{
		Opportunity:              true,
		SecondsSinceLastBehavior: 300,
		BehaviorCount60s:         0,
		MaxBehaviorsPerMinute:    1,
		SecondsSinceThroatClear:  600,
		SecondsSinceCough:        600,
		SecondsSinceCorrection:   600,
		SecondsSinceRepeat:       600,
		SecondsSinceInversion:    600,
		PreferLightDisfluency:    true,
		PreferRepetition:         true,
		PreferInversion:          true,
		AllowNonVerbal:           true,
		Capabilities: Capabilities{
			SupportsParalinguisticMarks: false,
			HasThroatClearAsset:         true,
			HasCoughAsset:               true,
		},
	}
}

func TestSensitiveContextSuppressesBehavior(t *testing.T) {
	ctx := baseContext()
	ctx.Complaint = true
	plan, err := NewDefaultRegistry().Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Enabled || plan.Event.Kind != KindNone {
		t.Fatalf("sensitive context should suppress behavior: %#v", plan)
	}
}

func TestCooldownPreventsOveruse(t *testing.T) {
	ctx := baseContext()
	ctx.SecondsSinceLastBehavior = 5
	plan, err := NewDefaultRegistry().Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Enabled {
		t.Fatalf("cooldown should suppress behavior: %#v", plan)
	}
}

func TestThroatClearUsesAudioMicroSegmentWhenTTSCannotExpressIt(t *testing.T) {
	ctx := baseContext()
	ctx.Capabilities.SupportsParalinguisticMarks = false
	ctx.Capabilities.HasThroatClearAsset = true
	plan, err := NewDefaultRegistry().Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Event.Kind != KindThroatClear || plan.Event.Delivery != DeliveryAudioSegment {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

type customBehavior struct{}

func (customBehavior) Name() string  { return "custom.double_take" }
func (customBehavior) Priority() int { return 3000 }
func (customBehavior) CanHandle(ctx Context, _ Thresholds) bool {
	return ctx.Opportunity && !blocked(ctx)
}
func (customBehavior) Build(_ Context, _ Thresholds) (Plan, error) {
	return Plan{
		Enabled: true,
		Event: Event{
			Kind:        Kind("DOUBLE_TAKE"),
			Delivery:    DeliveryTextDirective,
			Instruction: "自定义行为",
			MaxCount:    1,
		},
	}, nil
}

func TestCustomBehaviorCanBeRegisteredWithoutChangingRegistry(t *testing.T) {
	registry := NewDefaultRegistry()
	registry.Register(customBehavior{})
	plan, err := registry.Plan(baseContext())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Strategy != "custom.double_take" || plan.Event.Kind != Kind("DOUBLE_TAKE") {
		t.Fatalf("custom behavior not selected: %#v", plan)
	}
}

func TestGenericRuleCanRepresentTraitStateOrUserBehavior(t *testing.T) {
	now := time.Date(2026, 9, 30, 3, 50, 0, 0, time.UTC)
	registry := NewDefaultRegistry()
	registry.RegisterRule(Rule{
		ID:          "host.short_sentence",
		Source:      RuleSourceTrait,
		BaseWeight:  500,
		Intensity:   0.7,
		Cooldown:    2 * time.Minute,
		MaxPer5M:    2,
		Instruction: "主播偏好短句，当前回答尽量用短句自然表达。",
		Channel:     ChannelText,
		Conditions: RuleConditions{
			EventTypes: []string{"reply_chat"},
			Heat:       []string{"warm"},
		},
	})
	ctx := baseContext()
	ctx.Now = now
	ctx.EventType = "reply_chat"
	ctx.Heat = "warm"
	plan, err := registry.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Strategy != "humanization.rule.host.short_sentence" {
		t.Fatalf("generic rule not selected: %#v", plan)
	}
	if plan.Event.Source != RuleSourceTrait || plan.Event.RuleID != "host.short_sentence" {
		t.Fatalf("rule metadata not preserved: %#v", plan.Event)
	}
	if plan.Event.Channel != ChannelText || plan.Event.Intensity != 0.7 {
		t.Fatalf("channel/intensity not preserved: %#v", plan.Event)
	}
}

func TestGenericRuleHonorsExpiryCooldownAndUsageCaps(t *testing.T) {
	now := time.Date(2026, 9, 30, 3, 55, 0, 0, time.UTC)
	rule := Rule{
		ID:            "session.throat_light",
		Source:        RuleSourceState,
		BaseWeight:    1000,
		Cooldown:      3 * time.Minute,
		MaxPer5M:      1,
		MaxPerSession: 2,
		ExpiresAt:     now.Add(time.Hour),
		Instruction:   "当前场嗓子不舒服，整体说轻一点。",
		Channel:       ChannelTTSStyle,
	}
	registry := NewRegistry(DefaultThresholds(), NoopStrategy{})
	registry.RegisterRule(rule)

	ctx := baseContext()
	ctx.Now = now
	ctx.RecentRuleUsage = map[string]int{rule.ID: 1}
	plan, err := registry.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Enabled {
		t.Fatalf("5m usage cap must suppress rule: %#v", plan)
	}

	ctx.RecentRuleUsage = map[string]int{}
	ctx.LastRuleUsedAt = map[string]time.Time{rule.ID: now.Add(-time.Minute)}
	plan, err = registry.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Enabled {
		t.Fatalf("cooldown must suppress rule: %#v", plan)
	}

	ctx.LastRuleUsedAt = map[string]time.Time{}
	ctx.Now = now.Add(2 * time.Hour)
	plan, err = registry.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Enabled {
		t.Fatalf("expired state rule must not run: %#v", plan)
	}
}
