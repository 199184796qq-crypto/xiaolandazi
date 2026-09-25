package humanization

import "testing"

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
