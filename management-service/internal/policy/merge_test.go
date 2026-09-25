package policy

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestL3CanReplaceAndDisableL2ButNotL1(t *testing.T) {
	l1 := &model.LivePolicyVersion{
		ID: 1,
		Rules: []model.LivePolicyRule{
			{Key: "safety.no_fake_facts", Text: "不得编造商品事实", ExecutionMode: "intent", Enabled: true},
		},
	}
	l2 := &model.LivePolicyVersion{
		ID: 2,
		Rules: []model.LivePolicyRule{
			{Key: "business.elderly", Text: "老人问题统一谨慎提示", ExecutionMode: "intent", Enabled: true},
			{Key: "business.cta", Text: "使用行业默认催单", ExecutionMode: "intent", Enabled: true},
		},
	}
	l3 := &model.LivePolicyVersion{
		ID: 3,
		Overrides: []model.LivePolicyOverride{
			{Key: "business.elderly", Operation: "replace", Text: "普通食用问题正常回答，涉及疾病用药再提高谨慎等级", ExecutionMode: "intent"},
			{Key: "business.cta", Operation: "disable"},
			{Key: "safety.no_fake_facts", Operation: "replace", Text: "可以随便编", ExecutionMode: "intent"},
		},
	}

	effective := BuildEffective("food", l1, l2, l3)
	if len(effective.Rules) != 2 {
		t.Fatalf("rules=%d want 2", len(effective.Rules))
	}
	if effective.Rules[0].Key != "safety.no_fake_facts" || effective.Rules[0].SourceLayer != "L1" {
		t.Fatalf("first rule=%+v", effective.Rules[0])
	}
	if effective.Rules[1].Key != "business.elderly" || effective.Rules[1].SourceLayer != "L3" {
		t.Fatalf("replacement=%+v", effective.Rules[1])
	}
	if len(effective.Conflicts) != 1 || effective.Conflicts[0].Code != "l1_locked" {
		t.Fatalf("conflicts=%+v", effective.Conflicts)
	}
}

func TestVerbatimModePreserved(t *testing.T) {
	l2 := &model.LivePolicyVersion{
		ID: 2,
		Rules: []model.LivePolicyRule{
			{Key: "script.shipping", Text: "现在下单按顺序发货", ExecutionMode: "verbatim", FixedText: "现在下单按顺序发货", Enabled: true},
		},
	}
	effective := BuildEffective("general", nil, l2, nil)
	if len(effective.Rules) != 1 {
		t.Fatalf("rules=%d", len(effective.Rules))
	}
	if effective.Rules[0].ExecutionMode != "verbatim" || effective.Rules[0].FixedText == "" {
		t.Fatalf("rule=%+v", effective.Rules[0])
	}
}

func TestRenderPromptUsesExpressionHierarchy(t *testing.T) {
	prompt := RenderPrompt(model.LiveEffectivePolicy{
		IndustryCode: "food",
		Rules: []model.LiveEffectivePolicyRule{
			{Key: "l1.truth", Text: "事实要真实", SourceLayer: "L1", ExecutionMode: "intent"},
		},
	})
	for _, required := range []string{
		"规则层是通用判断与表达方法",
		"行业层是在规则层方法上",
		"用户层是在规则层和行业层上",
		"规则层不等于禁止清单",
		"自然、热情、好听、可直接播出且不违规",
		"[规则层][l1.truth]",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt missing %q: %s", required, prompt)
		}
	}
}

func TestValidateL3RejectsLockedL1Key(t *testing.T) {
	l1 := &model.LivePolicyVersion{
		Rules: []model.LivePolicyRule{
			{Key: "safety.no_fake_facts", Text: "不得编造", Enabled: true},
		},
	}
	conflicts := ValidateDraft("L3", nil, []model.LivePolicyOverride{
		{Key: "safety.no_fake_facts", Operation: "disable"},
	}, l1)
	if len(conflicts) == 0 || conflicts[0].Code != "l1_locked" {
		t.Fatalf("conflicts=%+v", conflicts)
	}
}

func TestL3AddCannotReuseLockedL1Key(t *testing.T) {
	l1 := &model.LivePolicyVersion{
		ID: 1,
		Rules: []model.LivePolicyRule{
			{Key: "safety.no_fake_facts", Text: "不得编造", Enabled: true},
		},
	}
	l3 := &model.LivePolicyVersion{
		ID: 3,
		Overrides: []model.LivePolicyOverride{
			{
				Key:           "safety.no_fake_facts",
				Operation:     "add",
				Text:          "新增另一条同名规则",
				ExecutionMode: "intent",
			},
		},
	}

	conflicts := ValidateDraft("L3", nil, l3.Overrides, l1)
	if len(conflicts) == 0 || conflicts[0].Code != "l1_locked" {
		t.Fatalf("validation conflicts=%+v", conflicts)
	}

	effective := BuildEffective("general", l1, nil, l3)
	if len(effective.Rules) != 1 {
		t.Fatalf("rules=%+v", effective.Rules)
	}
	if effective.Rules[0].SourceLayer != "L1" {
		t.Fatalf("rule source=%s want L1", effective.Rules[0].SourceLayer)
	}
	if len(effective.Conflicts) != 1 || effective.Conflicts[0].Code != "l1_locked" {
		t.Fatalf("effective conflicts=%+v", effective.Conflicts)
	}
}
