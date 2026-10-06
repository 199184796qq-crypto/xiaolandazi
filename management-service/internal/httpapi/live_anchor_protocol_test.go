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

type failingStyleTestGateway struct{ calls int }

func (g *failingStyleTestGateway) Complete(_ context.Context, _ agentgateway.Request) (agentgateway.Response, error) {
	g.calls++
	return agentgateway.Response{Provider: "fixture-vendor", Model: "fixture-model", LatencyMS: 1}, errors.New("fixture provider timeout")
}
func TestShortSampleDoesNotForceDensityRepair(t *testing.T) {
	source := strings.Repeat("我慢慢讲呀，先把这个说明白。", 16)
	profile := stylecontract.Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: stylecontract.Version, Instructions: []string{"短句解释", "原词自称", "句尾语气", "自然转场", "准确回答", "不编造", "不促销", "不喊叫"}, Habits: []model.LiveAnchorLiteralHabit{{Kind: "particle", Text: "呀"}, {Kind: "self_address", Text: "我"}}}}, source)
	ctx := model.LiveAgentFullShowGenerationContext{UseAnchorStyle: true, AnchorStyle: profile}
	g := &styleTestFixtureGateway{outputs: []string{strings.Repeat("先给大家把这个说明白。", 20), source}}
	text, result, _, audit, repaired, err := generateAnchorStyleTest(context.Background(), g, ctx, "事实优先", "自然解释", 225)
	if err != nil || text == "" || !audit.Passed || repaired || result.LatencyMS != 1 {
		t.Fatalf("failed: %v %+v", err, audit)
	}
	if len(g.calls) != 1 || g.calls[0].Stage != "speech_generation" {
		t.Fatal("short sample density should remain advisory")
	}
	if !strings.Contains(g.calls[0].Messages[1].Content, profile.Delivery.Rulebook) {
		t.Fatal("test did not consume exact compiled rules")
	}
}

func TestShortSampleLowStyleDoesNotBlockPreview(t *testing.T) {
	source := strings.Repeat("哥哥姐姐们啊，你们听我们家把重点讲清楚哟。", 24)
	profile := stylecontract.Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: stylecontract.Version}}, source)
	bad := strings.Repeat("先说明当前内容，再继续补充相关信息。", 14)
	g := &styleTestFixtureGateway{outputs: []string{bad, bad, bad}}
	text, _, _, _, repaired, err := generateAnchorStyleTest(context.Background(), g, model.LiveAgentFullShowGenerationContext{AnchorStyle: profile, UseAnchorStyle: true, StyleMatchIntensity: 100}, "", "解释", len([]rune(bad)))
	if err != nil || text == "" || repaired || len(g.calls) != 1 {
		t.Fatalf("short-sample density unexpectedly blocked preview: err=%v text=%q repaired=%v calls=%d", err, text, repaired, len(g.calls))
	}
}

func TestPersistentHardFactFailureFallsBackWithoutUnsafeText(t *testing.T) {
	source := strings.Repeat("先慢慢解释呀，接着再说明呀。", 15)
	profile := stylecontract.Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: stylecontract.Version, Instructions: []string{"规则甲", "规则乙", "规则丙", "规则丁", "规则戊", "规则己", "规则庚", "规则辛"}, Habits: []model.LiveAnchorLiteralHabit{{Kind: "particle", Text: "呀"}}}}, source)
	bad := strings.Repeat("库存还剩9999件。", 25)
	g := &styleTestFixtureGateway{outputs: []string{bad, bad, bad}}
	observer := &anchorStyleGenerationObserver{}
	text, _, _, audit, repaired, _, err := generateAnchorStyleTestContinuing(context.Background(), g, model.LiveAgentFullShowGenerationContext{AnchorStyle: profile, UseAnchorStyle: true}, "", "解释", 220, nil, observer)
	if err != nil || text == "" || !audit.Passed || !repaired || len(g.calls) != 3 {
		t.Fatalf("unsafe fallback behavior: err=%v text=%q audit=%+v repaired=%v calls=%d", err, text, audit, repaired, len(g.calls))
	}
	if !observer.DegradedStyle || len(observer.FallbackSegments) != 1 || strings.Contains(text, "9999") {
		t.Fatalf("fallback was not marked or retained unsafe claim: observer=%+v text=%q", observer, text)
	}
	var gateErr *anchorStyleTestGateError
	if errors.As(err, &gateErr) {
		t.Fatalf("fallback should complete the segment, got gate error: %v", gateErr)
	}
}

func TestProviderFailureRetriesThenUsesNeutralFallback(t *testing.T) {
	source := strings.Repeat("哥哥姐姐们啊，我们家把重点讲清楚哟。", 20)
	profile := stylecontract.Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: stylecontract.Version}}, source)
	g := &failingStyleTestGateway{}
	observer := &anchorStyleGenerationObserver{}
	text, _, _, audit, repaired, _, err := generateAnchorStyleTestContinuing(context.Background(), g, model.LiveAgentFullShowGenerationContext{AnchorStyle: profile, UseAnchorStyle: true}, "", "解释", 220, nil, observer)
	if err != nil || text == "" || !audit.Passed || !repaired || g.calls != 3 {
		t.Fatalf("provider failures did not fall back safely: err=%v text=%q audit=%+v repaired=%v calls=%d", err, text, audit, repaired, g.calls)
	}
	if !observer.DegradedStyle || len(observer.FallbackSegments) != 1 || strings.Contains(text, "timeout") {
		t.Fatalf("provider fallback metadata/text invalid: observer=%+v text=%q", observer, text)
	}
}

func TestForceSafeAnchorStyleTermsOnlyAddsStableNonSemanticHabits(t *testing.T) {
	state := stylecontract.RollingWindowState{
		Runtime: stylecontract.RuntimeEvaluation{HabitGroups: []stylecontract.RuntimeHabitEvaluation{
			{Kind: "audience_address", Stability: "stable"},
			{Kind: "particle", Stability: "stable"},
			{Kind: "catchphrase", Stability: "stable"},
		}},
		Needs: []stylecontract.RollingHabitDelta{
			{Kind: "audience_address", Term: "哥哥姐姐们", Delta: 1},
			{Kind: "particle", Term: "啊", Delta: 1},
			{Kind: "catchphrase", Term: "全网第一", Delta: 1},
		},
	}
	patched, additions := forceSafeAnchorStyleTerms("先把已经确认的信息说明白。", state, 80)
	if !strings.HasPrefix(patched, "哥哥姐姐们，") || !strings.Contains(patched, "啊。") {
		t.Fatalf("safe stable habits were not patched: %q additions=%v", patched, additions)
	}
	if strings.Contains(patched, "全网第一") {
		t.Fatalf("meaning-bearing catchphrase was mechanically injected: %q", patched)
	}
}

func TestApprovedFactTemplateFallbackUsesOnlyAssignedFact(t *testing.T) {
	generation := model.LiveAgentFullShowGenerationContext{AuthorizedFacts: []model.LiveAgentGenerationFact{
		{FactID: "product:1:spec", ProductName: "一号商品", Label: "规格", Value: "5L", CanGenerate: true},
		{FactID: "product:2:price", ProductName: "二号商品", Label: "价格", Value: "99元", CanGenerate: true},
	}}
	text, ok := approvedFactTemplateFallback(generation, model.LiveSpeechExpansionStep{PrimaryFactID: "product:1:spec"}, 60, 120)
	if !ok || !strings.Contains(text, "一号商品") || !strings.Contains(text, "5L") || strings.Contains(text, "99元") {
		t.Fatalf("fact template escaped assigned scope: ok=%v text=%q", ok, text)
	}
}

func TestFailedMiddleSegmentIsSkippedAndNextSegmentCarriesLength(t *testing.T) {
	bad := strings.Repeat("库存还剩9999件。", 20)
	good := strings.Repeat("先把重点自然说明白。", 20)
	g := &styleTestFixtureGateway{outputs: []string{bad, bad, bad, good}}
	observer := &anchorStyleGenerationObserver{}
	generation := model.LiveAgentFullShowGenerationContext{ExpansionPlans: []model.LiveSpeechExpansionPlan{{Steps: []model.LiveSpeechExpansionStep{
		{Index: 1, StartSecond: 0, EndSecond: 30, TargetChars: 100},
		{Index: 2, StartSecond: 30, EndSecond: 60, TargetChars: 100},
	}}}}
	text, _, _, audit, repaired, _, err := generateAnchorStyleTestContinuing(context.Background(), g, generation, "", "解释", 200, nil, observer)
	if err != nil || !audit.Passed || !repaired || text != good || len(g.calls) != 4 {
		t.Fatalf("skip/recovery failed: err=%v audit=%+v repaired=%v calls=%d text=%q", err, audit, repaired, len(g.calls), text)
	}
	if len(observer.SkippedSegments) != 1 || observer.SkippedSegments[0] != 1 || strings.Contains(text, "9999") {
		t.Fatalf("failed segment leaked or was not recorded: observer=%+v text=%q", observer, text)
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
