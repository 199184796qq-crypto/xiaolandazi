package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/stylecontract"
	"strings"
	"testing"
)

type styleTestFixtureGateway struct {
	calls   []agentgateway.Request
	outputs []string
}

func (g *styleTestFixtureGateway) Complete(_ context.Context, r agentgateway.Request) (agentgateway.Response, error) {
	g.calls = append(g.calls, r)
	return agentgateway.Response{Text: g.outputs[len(g.calls)-1], Provider: "fixture-vendor", Model: "fixture-model", LatencyMS: 1}, nil
}
func TestPortableGenerationReturnsStyleDeviationAsSoftScore(t *testing.T) {
	source := strings.Repeat("我慢慢讲呀，先把这个说明白。", 16)
	profile := stylecontract.Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: stylecontract.Version, Instructions: []string{"短句解释", "原词自称", "句尾语气", "自然转场", "准确回答", "不编造", "不促销", "不喊叫"}, Habits: []model.LiveAnchorLiteralHabit{{Kind: "particle", Text: "呀"}, {Kind: "self_address", Text: "我"}}}}, source)
	ctx := model.LiveAgentFullShowGenerationContext{UseAnchorStyle: true, AnchorStyle: profile}
	g := &styleTestFixtureGateway{outputs: []string{strings.Repeat("先给大家把这个说明白。", 20), source}}
	text, result, check, audit, repaired, err := generateAnchorStyleTest(context.Background(), g, ctx, "事实优先", "自然解释", 225)
	if err != nil || text == "" || check.Passed || !audit.Passed || repaired || result.LatencyMS != 1 {
		t.Fatalf("failed: %v %+v %+v", err, check, audit)
	}
	if len(g.calls) != 1 || g.calls[0].Stage != "speech_generation" {
		t.Fatal("soft style deviation should not trigger a complete rewrite")
	}
	if !strings.Contains(g.calls[0].Messages[1].Content, profile.Delivery.Rulebook) {
		t.Fatal("test did not consume exact compiled rules")
	}
}

func TestPersistentHardFactFailureNeverReturnsSuccessfulPreview(t *testing.T) {
	source := strings.Repeat("先慢慢解释呀，接着再说明呀。", 15)
	profile := stylecontract.Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: stylecontract.Version, Instructions: []string{"规则甲", "规则乙", "规则丙", "规则丁", "规则戊", "规则己", "规则庚", "规则辛"}, Habits: []model.LiveAnchorLiteralHabit{{Kind: "particle", Text: "呀"}}}}, source)
	bad := strings.Repeat("库存还剩9999件。", 25)
	g := &styleTestFixtureGateway{outputs: []string{bad, bad, bad}}
	text, _, _, _, _, err := generateAnchorStyleTest(context.Background(), g, model.LiveAgentFullShowGenerationContext{AnchorStyle: profile, UseAnchorStyle: true}, "", "解释", 220)
	if err == nil || text != "" || len(g.calls) != 3 {
		t.Fatal("nonconforming text escaped gate")
	}
	var gateErr *anchorStyleTestGateError
	if !errors.As(err, &gateErr) || gateErr.Attempts != 3 || gateErr.ActualChars == 0 {
		t.Fatalf("missing gate diagnostics: %T %v", err, err)
	}
}

func TestOverlayOnlyPreviewDoesNotRequireSampleStyle(t *testing.T) {
	output := strings.Repeat("先和大家轻松聊一句，再把重点自然说明白。", 12)
	g := &styleTestFixtureGateway{outputs: []string{output}}
	generation := model.LiveAgentFullShowGenerationContext{
		UseAnchorStyle:     true,
		StyleOverlayPrompt: "【方案级叠加风格】\n- 偶尔轻松一下：只在自然转场处加入一句轻微自嘲",
		StyleOverlayCount:  1,
	}
	text, _, check, audit, repaired, err := generateAnchorStyleTest(context.Background(), g, generation, "事实优先", "自然打招呼", 240)
	if err != nil || text != output || !check.Passed || !audit.Passed || repaired {
		t.Fatalf("overlay-only preview failed: err=%v check=%+v audit=%+v", err, check, audit)
	}
	if !strings.Contains(g.calls[0].Messages[1].Content, generation.StyleOverlayPrompt) || !strings.Contains(g.calls[0].Messages[1].Content, "未提供主播样本风格") {
		t.Fatal("overlay-only prompt did not carry the compiled persona rule")
	}
}
