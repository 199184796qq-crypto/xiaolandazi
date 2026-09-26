package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestPreferredPolicyTestVersionPrefersDraft(t *testing.T) {
	active := model.LivePolicyVersion{ID: 1, VersionNo: 1, LifecycleStatus: "active"}
	ctx := model.LivePolicyContext{
		Active: &active,
		Versions: []model.LivePolicyVersion{
			active,
			{ID: 2, VersionNo: 2, LifecycleStatus: "draft"},
		},
	}
	got := preferredPolicyTestVersion(ctx)
	if got == nil || got.ID != 2 {
		t.Fatalf("got=%+v want draft id=2", got)
	}
}

func TestResolvePolicyTestMatchedRulesRejectsUnknownKeys(t *testing.T) {
	rules := []model.LiveEffectivePolicyRule{
		{Key: "l1.truth", Title: "事实真实性", SourceLayer: "L1", ExecutionMode: "intent"},
	}
	got := resolvePolicyTestMatchedRules(
		[]string{"unknown", "l1.truth", "l1.truth"},
		rules,
	)
	if len(got) != 1 || got[0].Key != "l1.truth" {
		t.Fatalf("matched=%+v", got)
	}
}

func TestBuildPolicyTestMessagesPreservesRefinementHistory(t *testing.T) {
	messages := buildPolicyTestMessages(
		"system",
		"太硬了，再自然一点",
		[]liveAgentChatHistoryItem{
			{Role: "user", Text: "库存紧张吗？"},
			{Role: "agent", Text: "库存情况需要核实。"},
		},
	)
	if len(messages) != 4 {
		t.Fatalf("messages=%d want 4: %+v", len(messages), messages)
	}
	if messages[1]["role"] != "user" || messages[1]["content"] != "库存紧张吗？" {
		t.Fatalf("first history=%+v", messages[1])
	}
	if messages[2]["role"] != "assistant" || messages[2]["content"] != "库存情况需要核实。" {
		t.Fatalf("assistant history=%+v", messages[2])
	}
	if messages[3]["content"] != "太硬了，再自然一点" {
		t.Fatalf("latest=%+v", messages[3])
	}
}

func TestBuildPolicyTestSystemPromptIsSandboxOnly(t *testing.T) {
	instruction := "纯测试环境；目标是怎样说最合适；禁止发送 TTS；禁止调用或声称调用真实动作；reply 要自然热情；blocked 表示原要求需要调整后再表达；matched_keys 只能来自有效规则；多轮时结合上一轮用户问题和主播回复继续优化"
	prompt, err := buildPolicyTestSystemPrompt(instruction, model.LiveEffectivePolicy{
		IndustryCode: "general",
		Rules: []model.LiveEffectivePolicyRule{
			{Key: "l1.truth", Title: "事实真实性", Text: "不得编造", SourceLayer: "L1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"纯测试环境",
		"禁止发送 TTS",
		"禁止调用或声称调用",
		"matched_keys",
		"怎样说最合适",
		"热情",
		"原要求需要调整后再表达",
		"上一轮用户问题和主播回复继续优化",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt missing %q", required)
		}
	}
}
