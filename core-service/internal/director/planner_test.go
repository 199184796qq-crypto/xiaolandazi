package director

import (
	"testing"

	"livecompanion/core/internal/resume"
)

func baseSnapshot() Snapshot {
	return Snapshot{
		RoomID:             1,
		ProductStoryRemain: true,
		Style: StyleProfile{
			Name:              "anchor-a",
			Tone:              "自然、接地气",
			SentenceRhythm:    "长短句交替",
			CTAStyle:          "轻量提醒",
			HumorCeiling:      1,
			DefaultTargetSecs: 12,
		},
		Signals: RoomSignals{
			Events30s:         10,
			Chat30s:           3,
			SecondsSinceHumor: 300,
		},
		Resume: resume.Context{
			MainlineStillValid: true,
			NextUnits: []resume.MainlineUnit{
				{ID: "next", IndependentEntry: true, Cursor: resume.Cursor{PlanID: "p", UnitID: "next", SegmentID: "next"}},
			},
		},
	}
}

func TestPlannerCombinesProgressAtmosphereStyleAndResume(t *testing.T) {
	snapshot := baseSnapshot()
	snapshot.Signals.ActionableQuestions = 1
	snapshot.Signals.Orders30s = 2

	plan, err := NewDefaultPlanner().Plan(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Progress != ProgressHoldForQNA {
		t.Fatalf("progress=%s", plan.Progress)
	}
	if plan.Atmosphere != AtmosphereCelebrate {
		t.Fatalf("atmosphere=%s", plan.Atmosphere)
	}
	if plan.HumorLevel != 1 {
		t.Fatalf("humor=%d", plan.HumorLevel)
	}
	if plan.Resume.Mode != resume.ModeDirect {
		t.Fatalf("resume=%s", plan.Resume.Mode)
	}
	if len(plan.StrategyTrace) < 4 {
		t.Fatalf("trace=%v", plan.StrategyTrace)
	}
}

func TestComplaintSuppressesHumor(t *testing.T) {
	snapshot := baseSnapshot()
	snapshot.Signals.NegativeFeedback30s = 1
	snapshot.Signals.Orders30s = 3

	plan, err := NewDefaultPlanner().Plan(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Atmosphere != AtmosphereCalmAndFocus {
		t.Fatalf("atmosphere=%s", plan.Atmosphere)
	}
	if plan.HumorLevel != 0 || plan.HumorInstruction != "" {
		t.Fatalf("humor should be disabled: %#v", plan)
	}
}

type customProgress struct{}

func (customProgress) Name() string          { return "custom.flash_sale" }
func (customProgress) Dimension() Dimension  { return DimensionProgress }
func (customProgress) Priority() int         { return 2000 }
func (customProgress) Match(s Snapshot) bool { return s.ProgressHint == "FLASH_SALE" }
func (customProgress) Apply(_ Snapshot, _ Plan) Contribution {
	return Contribution{
		Progress:         ProgressConvert,
		TargetSeconds:    6,
		PromptDirectives: []string{"使用新注册的闪购节奏策略。"},
	}
}

func TestNewStrategyCanBeRegisteredWithoutChangingPlanner(t *testing.T) {
	registry := NewDefaultRegistry()
	registry.Register(customProgress{})
	planner := NewPlanner(registry, nil)

	snapshot := baseSnapshot()
	snapshot.ProgressHint = "FLASH_SALE"
	plan, err := planner.Plan(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if plan.TargetSeconds != 6 {
		t.Fatalf("target seconds=%d", plan.TargetSeconds)
	}
	if plan.StrategyTrace[0] != "custom.flash_sale" {
		t.Fatalf("trace=%v", plan.StrategyTrace)
	}
}
