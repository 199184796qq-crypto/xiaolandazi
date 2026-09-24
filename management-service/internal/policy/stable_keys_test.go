package policy

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestEnsureStableRuleKeysGeneratesReadableUniqueKeys(t *testing.T) {
	rules := []model.LivePolicyRule{
		{Title: "事实真实性", Text: "不得编造事实", ExecutionMode: model.LivePolicyModeIntent},
		{Title: "事实真实性", Text: "另一条不同规则", ExecutionMode: model.LivePolicyModeIntent},
	}
	got := EnsureStableRuleKeys(model.LivePolicyLayerL1, rules, nil)
	if got[0].Key == "" || got[1].Key == "" {
		t.Fatalf("expected generated keys: %#v", got)
	}
	if got[0].Key == got[1].Key {
		t.Fatalf("keys must be unique: %q", got[0].Key)
	}
	if !strings.HasPrefix(got[0].Key, "l1.事实真实性.") {
		t.Fatalf("expected readable Chinese slug, got %q", got[0].Key)
	}
	if got[0].Text != rules[0].Text || got[0].ExecutionMode != rules[0].ExecutionMode {
		t.Fatalf("key generation must not modify rule content")
	}
}

func TestEnsureStableRuleKeysReusesBaseKeyByTitle(t *testing.T) {
	base := []model.LivePolicyRule{
		{Key: "l1.fact.truth", Title: "事实真实性", Text: "旧正文"},
	}
	rules := []model.LivePolicyRule{
		{Title: "事实真实性", Text: "修改后的正文", ExecutionMode: model.LivePolicyModeIntent},
	}
	got := EnsureStableRuleKeys(model.LivePolicyLayerL1, rules, base)
	if got[0].Key != "l1.fact.truth" {
		t.Fatalf("expected base key reuse, got %q", got[0].Key)
	}
}

func TestPrioritizeNewRulesPutsNewFirstAndKeepsExistingOrder(t *testing.T) {
	base := []model.LivePolicyRule{
		{Key: "l1.a", Title: "A"},
		{Key: "l1.b", Title: "B"},
		{Key: "l1.c", Title: "C"},
	}
	rules := []model.LivePolicyRule{
		{Key: "l1.b", Title: "B edited"},
		{Key: "l1.new-1", Title: "New 1"},
		{Key: "l1.a", Title: "A"},
		{Key: "l1.new-2", Title: "New 2"},
		{Key: "l1.c", Title: "C"},
	}

	got := PrioritizeNewRules(rules, base)
	want := []string{"l1.new-1", "l1.new-2", "l1.a", "l1.b", "l1.c"}
	if len(got) != len(want) {
		t.Fatalf("got %d rules, want %d", len(got), len(want))
	}
	for index, key := range want {
		if got[index].Key != key {
			t.Fatalf("rule %d key=%q want %q; got=%#v", index, got[index].Key, key, got)
		}
	}
	if got[3].Title != "B edited" {
		t.Fatalf("edited existing rule content was not preserved: %#v", got[3])
	}
}

func TestPrioritizeNewRulesKeepsSurvivingExistingOrder(t *testing.T) {
	base := []model.LivePolicyRule{
		{Key: "l1.a"},
		{Key: "l1.b"},
		{Key: "l1.c"},
	}
	rules := []model.LivePolicyRule{
		{Key: "l1.c"},
		{Key: "l1.b"},
	}

	got := PrioritizeNewRules(rules, base)
	if len(got) != 2 || got[0].Key != "l1.b" || got[1].Key != "l1.c" {
		t.Fatalf("surviving existing rules must keep base order: %#v", got)
	}
}

func TestEnsureStableOverrideKeysGeneratesOnlyForAdd(t *testing.T) {
	overrides := []model.LivePolicyOverride{
		{Operation: model.LivePolicyOverrideAdd, Title: "直播间欢迎", Text: "欢迎新来的朋友"},
		{Operation: model.LivePolicyOverrideReplace, Text: "替换内容"},
	}
	got := EnsureStableOverrideKeys(overrides, nil)
	if !strings.HasPrefix(got[0].Key, "l3.直播间欢迎.") {
		t.Fatalf("expected generated L3 add key, got %q", got[0].Key)
	}
	if got[1].Key != "" {
		t.Fatalf("replace without a known key must stay invalid, got %q", got[1].Key)
	}
}

func TestEnsureStableRuleKeysKeepsExplicitKey(t *testing.T) {
	rules := []model.LivePolicyRule{
		{Key: "l2.shipping.answer", Title: "物流回答", Text: "按当前物流回答"},
	}
	got := EnsureStableRuleKeys(model.LivePolicyLayerL2, rules, nil)
	if got[0].Key != rules[0].Key {
		t.Fatalf("explicit key changed from %q to %q", rules[0].Key, got[0].Key)
	}
}
