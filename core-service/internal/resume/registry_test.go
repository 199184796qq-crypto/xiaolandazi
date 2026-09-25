package resume

import "testing"

func testUnit(id string) MainlineUnit {
	return MainlineUnit{ID: id, IndependentEntry: true, Cursor: Cursor{PlanID: "p", UnitID: id, SegmentID: id}}
}

func TestBuiltInResumeStrategies(t *testing.T) {
	registry := NewDefaultRegistry()

	direct, err := registry.Plan(Context{
		MainlineStillValid: true,
		NextUnits:          []MainlineUnit{testUnit("u2")},
	})
	if err != nil || direct.Mode != ModeDirect {
		t.Fatalf("direct=%#v err=%v", direct, err)
	}

	bridge, err := registry.Plan(Context{
		MainlineStillValid:    true,
		InteractionDurationMS: 6_000,
		SuggestedResumeTail:   "这个问题咱们说清楚了，接着看肉质这一块。",
		NextUnits:             []MainlineUnit{testUnit("u2")},
	})
	if err != nil || bridge.Mode != ModeBridge || bridge.BridgeText == "" {
		t.Fatalf("bridge=%#v err=%v", bridge, err)
	}

	skip, err := registry.Plan(Context{
		MainlineStillValid:  true,
		SuggestedMode:       ModeFusionSkip,
		SuggestedSkipUnits:  []string{"feeding"},
		SuggestedResumeTail: "饲喂说完了，咱们再往下看。",
		NextUnits: []MainlineUnit{
			testUnit("feeding"),
			testUnit("meat"),
		},
	})
	if err != nil || skip.Mode != ModeFusionSkip || skip.ResumeUnit != "meat" {
		t.Fatalf("skip=%#v err=%v", skip, err)
	}
}

type customStrategy struct{}

func (customStrategy) Name() string  { return "custom.after_order" }
func (customStrategy) Priority() int { return 2000 }
func (customStrategy) CanHandle(ctx Context, _ Thresholds) bool {
	return ctx.SuggestedMode == Mode("AFTER_ORDER")
}
func (customStrategy) Build(Context, Thresholds) (Plan, error) {
	return Plan{Mode: Mode("AFTER_ORDER"), BridgeText: "这一单接住了，咱们继续往下看。"}, nil
}

func TestCustomResumeStrategyDoesNotRequireRegistryChanges(t *testing.T) {
	registry := NewDefaultRegistry()
	registry.Register(customStrategy{})
	plan, err := registry.Plan(Context{SuggestedMode: Mode("AFTER_ORDER"), MainlineStillValid: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != Mode("AFTER_ORDER") || plan.Strategy != "custom.after_order" {
		t.Fatalf("plan=%#v", plan)
	}
}
