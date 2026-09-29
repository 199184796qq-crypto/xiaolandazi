package httpapi

import (
	"testing"

	"livecompanion/management/internal/model"
)

func validStrategyInputForTest() model.LiveStrategyCenterInput {
	return model.LiveStrategyCenterInput{
		AddressingMode: "system",
		Rules: []model.LiveStrategyRule{
			{Category: "interrupt", Key: "a", Name: "A", Enabled: true, BaseProbability: 40},
			{Category: "interrupt", Key: "b", Name: "B", Enabled: true, BaseProbability: 30},
			{Category: "interrupt", Key: "c", Name: "C", Enabled: true, BaseProbability: 30},
			{Category: "resume", Key: "DIRECT", Name: "直接续播", Enabled: true, BaseProbability: 20},
			{Category: "resume", Key: "BRIDGE", Name: "桥接恢复", Enabled: true, BaseProbability: 20},
			{Category: "resume", Key: "FUSION_SKIP", Name: "融合跳过", Enabled: true, BaseProbability: 20},
			{Category: "resume", Key: "CROSS_RESUME", Name: "跨段恢复", Enabled: true, BaseProbability: 20},
			{Category: "resume", Key: "RE_ANCHOR", Name: "重新锚定", Enabled: true, BaseProbability: 20},
			{Category: "resume", Key: "SWITCH_PLAN", Name: "切换主线", Enabled: false, BaseProbability: 0},
			{Category: "interaction", Key: "welcome_named", Name: "点名欢迎", Enabled: true, BaseProbability: 5},
			{Category: "interaction", Key: "reply_chat", Name: "回复弹幕", Enabled: true, BaseProbability: 20},
		},
		Addressing: []model.LiveAddressingOption{
			{Key: "friend", Text: "朋友", Enabled: true, Probability: 50, SystemDefault: true},
			{Key: "everyone", Text: "大家", Enabled: true, Probability: 50, SystemDefault: true},
		},
	}
}

func TestValidateStrategyCenterAcceptsValidMinimumsAndTotals(t *testing.T) {
	input := validStrategyInputForTest()
	if err := validateStrategyCenterInput(&input); err != nil {
		t.Fatalf("valid strategy config rejected: %v", err)
	}
	if err := validateAddressingOptions(input.Addressing, input.AddressingMode); err != nil {
		t.Fatalf("valid addressing config rejected: %v", err)
	}
	for _, rule := range input.Rules {
		switch {
		case rule.Category == "interrupt" && rule.MinProbability != 10:
			t.Fatalf("interrupt minimum should normalize to 10, got %#v", rule)
		case rule.Category == "resume" && rule.MinProbability != 20:
			t.Fatalf("resume minimum should normalize to 20, got %#v", rule)
		case rule.Category == "interaction" && rule.Key == "reply_chat" && rule.MinProbability != 20:
			t.Fatalf("reply_chat minimum should normalize to 20, got %#v", rule)
		case rule.Category == "interaction" && rule.Key != "reply_chat" && rule.MinProbability != 5:
			t.Fatalf("ordinary interaction minimum should normalize to 5, got %#v", rule)
		}
	}
}

func TestValidateStrategyCenterNormalizesInterruptTotal(t *testing.T) {
	input := validStrategyInputForTest()
	input.Rules[0].BaseProbability = 50
	if err := validateStrategyCenterInput(&input); err != nil {
		t.Fatalf("interrupt weights should auto-normalize: %v", err)
	}
	total := 0
	for _, rule := range input.Rules {
		if rule.Category == "interrupt" && rule.Enabled {
			total += rule.BaseProbability
			if rule.BaseProbability < 10 {
				t.Fatalf("interrupt floor violated: %#v", rule)
			}
		}
	}
	if total != 100 {
		t.Fatalf("normalized interrupt total must be 100, got %d", total)
	}
}

func TestValidateStrategyCenterAutoDisablesExcessResumeStrategies(t *testing.T) {
	input := validStrategyInputForTest()
	for i := range input.Rules {
		if input.Rules[i].Category == "resume" {
			input.Rules[i].Enabled = true
			input.Rules[i].BaseProbability = 20
		}
	}
	if err := validateStrategyCenterInput(&input); err != nil {
		t.Fatalf("oversized resume pool should auto-optimize: %v", err)
	}
	enabled := 0
	total := 0
	for _, rule := range input.Rules {
		if rule.Category == "resume" && rule.Enabled {
			enabled++
			total += rule.BaseProbability
			if rule.BaseProbability < 20 {
				t.Fatalf("resume floor violated: %#v", rule)
			}
		}
	}
	if enabled != 5 || total != 100 {
		t.Fatalf("expected five enabled resume strategies totaling 100, enabled=%d total=%d", enabled, total)
	}
}

func TestValidateStrategyCenterRaisesProbabilityToMinimum(t *testing.T) {
	input := validStrategyInputForTest()
	for i := range input.Rules {
		if input.Rules[i].Key == "reply_chat" {
			input.Rules[i].BaseProbability = 19
		}
	}
	if err := validateStrategyCenterInput(&input); err != nil {
		t.Fatalf("below-minimum probability should be raised automatically: %v", err)
	}
	for _, rule := range input.Rules {
		if rule.Key == "reply_chat" && rule.BaseProbability != 20 {
			t.Fatalf("reply_chat should be raised to 20, got %d", rule.BaseProbability)
		}
	}
}

func TestValidateAddressingNormalizesOnlyActiveMode(t *testing.T) {
	options := []model.LiveAddressingOption{
		{Key: "sys", Text: "朋友", Enabled: true, Probability: 100, SystemDefault: true},
		{Key: "custom-a", Text: "老哥", Enabled: true, Probability: 60, SystemDefault: false},
		{Key: "custom-b", Text: "兄弟", Enabled: true, Probability: 40, SystemDefault: false},
	}
	if err := validateAddressingOptions(options, "system"); err != nil {
		t.Fatalf("system mode should validate only system options: %v", err)
	}
	if err := validateAddressingOptions(options, "custom"); err != nil {
		t.Fatalf("custom mode should validate only custom options: %v", err)
	}
	options[2].Probability = 30
	if err := validateAddressingOptions(options, "custom"); err != nil {
		t.Fatalf("custom addressing total should auto-normalize: %v", err)
	}
	customTotal := 0
	for _, option := range options {
		if !option.SystemDefault && option.Enabled {
			customTotal += option.Probability
		}
	}
	if customTotal != 100 {
		t.Fatalf("custom addressing should normalize to 100, got %d", customTotal)
	}
}

func TestAddressingWeightsThirtyFiftyFortyNormalizeToHundred(t *testing.T) {
	options := []model.LiveAddressingOption{
		{Key: "a", Text: "宝子", Enabled: true, Probability: 30, SystemDefault: false},
		{Key: "b", Text: "老哥", Enabled: true, Probability: 50, SystemDefault: false},
		{Key: "c", Text: "朋友", Enabled: true, Probability: 40, SystemDefault: false},
	}
	if err := validateAddressingOptions(options, "custom"); err != nil {
		t.Fatalf("addressing weights should normalize: %v", err)
	}
	if options[0].Probability != 25 || options[1].Probability != 42 || options[2].Probability != 33 {
		t.Fatalf("unexpected normalized addressing weights: %#v", options)
	}
}

func TestAddressingSignatureOnlyTracksActiveMode(t *testing.T) {
	base := []model.LiveAddressingOption{
		{Key: "sys", Text: "朋友", Enabled: true, Probability: 100, SystemDefault: true},
		{Key: "custom", Text: "老哥", Enabled: true, Probability: 100, SystemDefault: false},
	}
	changedInactive := append([]model.LiveAddressingOption(nil), base...)
	changedInactive[1].Text = "兄弟"
	if addressingSignature("system", base) != addressingSignature("system", changedInactive) {
		t.Fatal("editing inactive custom addressing should not mark system addressing changed")
	}
	changedActive := append([]model.LiveAddressingOption(nil), base...)
	changedActive[0].Text = "朋友们"
	if addressingSignature("system", base) == addressingSignature("system", changedActive) {
		t.Fatal("editing active system addressing must mark addressing changed")
	}
	if addressingSignature("system", base) == addressingSignature("custom", base) {
		t.Fatal("switching addressing mode must mark addressing changed")
	}
}
