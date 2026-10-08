package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechruntime"
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
	index := len(g.calls) - 1
	if index >= len(g.outputs) {
		index = len(g.outputs) - 1
	}
	return agentgateway.Response{Text: g.outputs[index], Provider: "fixture-vendor", Model: "fixture-model", LatencyMS: 1}, nil
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
	g := &styleTestFixtureGateway{outputs: []string{exactSegmentTextSeed(225, 0), exactSegmentTextSeed(225, 1)}}
	text, result, _, audit, repaired, err := generateAnchorStyleTest(context.Background(), g, ctx, "事实优先", "自然解释", 225)
	if err != nil || text == "" || !audit.Passed || repaired || result.LatencyMS != 2 {
		t.Fatalf("failed: %v %+v", err, audit)
	}
	if len(g.calls) != 2 || g.calls[0].Stage != "speech_generation" || g.calls[1].Stage != "speech_style_wrapper" {
		t.Fatal("base render should be followed by one natural style wrapper")
	}
	if strings.Contains(g.calls[0].Messages[1].Content, "主播风格元提示词") || strings.Contains(g.calls[0].Messages[1].Content, "哥哥姐姐们") {
		t.Fatal("base render consumed anchor style before final wrapping")
	}
}

func TestShortSampleLowStyleDoesNotBlockPreview(t *testing.T) {
	source := strings.Repeat("哥哥姐姐们啊，你们听我们家把重点讲清楚哟。", 24)
	profile := stylecontract.Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: stylecontract.Version}}, source)
	bad := exactSegmentTextSeed(220, 0)
	g := &styleTestFixtureGateway{outputs: []string{bad, exactSegmentTextSeed(220, 1)}}
	text, _, _, _, repaired, err := generateAnchorStyleTest(context.Background(), g, model.LiveAgentFullShowGenerationContext{AnchorStyle: profile, UseAnchorStyle: true, StyleMatchIntensity: 100}, "", "解释", len([]rune(bad)))
	if err != nil || text == "" || repaired || len(g.calls) != 2 {
		t.Fatalf("short-sample density unexpectedly blocked preview: err=%v text=%q repaired=%v calls=%d", err, text, repaired, len(g.calls))
	}
}

func TestPersistentHardFactFailureReturnsErrorWithoutTemplateFiller(t *testing.T) {
	source := strings.Repeat("先慢慢解释呀，接着再说明呀。", 15)
	profile := stylecontract.Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: stylecontract.Version, Instructions: []string{"规则甲", "规则乙", "规则丙", "规则丁", "规则戊", "规则己", "规则庚", "规则辛"}, Habits: []model.LiveAnchorLiteralHabit{{Kind: "particle", Text: "呀"}}}}, source)
	bad := strings.Repeat("库存还剩9999件。", 25)
	g := &styleTestFixtureGateway{outputs: []string{bad, bad, bad}}
	observer := &anchorStyleGenerationObserver{}
	text, _, _, audit, repaired, _, err := generateAnchorStyleTestContinuing(context.Background(), g, model.LiveAgentFullShowGenerationContext{AnchorStyle: profile, UseAnchorStyle: true}, "", "解释", 220, nil, observer)
	if err == nil || text != "" || !repaired || len(g.calls) != 3 {
		t.Fatalf("unsafe failure should stop instead of speaking filler: err=%v text=%q audit=%+v repaired=%v calls=%d", err, text, audit, repaired, len(g.calls))
	}
	if len(observer.FallbackSegments) != 0 || strings.Contains(text, "9999") {
		t.Fatalf("unsafe claim or removed filler fallback leaked: observer=%+v text=%q", observer, text)
	}
	var gateErr *anchorStyleTestGateError
	if !errors.As(err, &gateErr) {
		t.Fatalf("expected quality gate error, got: %v", err)
	}
}

func TestProviderFailureRetriesThenReturnsErrorWithoutNeutralFiller(t *testing.T) {
	source := strings.Repeat("哥哥姐姐们啊，我们家把重点讲清楚哟。", 20)
	profile := stylecontract.Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: stylecontract.Version}}, source)
	g := &failingStyleTestGateway{}
	observer := &anchorStyleGenerationObserver{}
	text, _, _, audit, repaired, _, err := generateAnchorStyleTestContinuing(context.Background(), g, model.LiveAgentFullShowGenerationContext{AnchorStyle: profile, UseAnchorStyle: true}, "", "解释", 220, nil, observer)
	if err == nil || text != "" || !repaired || g.calls != 3 {
		t.Fatalf("provider failures should stop cleanly: err=%v text=%q audit=%+v repaired=%v calls=%d", err, text, audit, repaired, g.calls)
	}
	if len(observer.FallbackSegments) != 0 || strings.Contains(text, "timeout") {
		t.Fatalf("provider error leaked into speech: observer=%+v text=%q", observer, text)
	}
}

func TestAddressingOverrideNeverMechanicallyPrefixesMissingTerms(t *testing.T) {
	profile := model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{
		Version:            stylecontract.Version,
		Habits:             []model.LiveAnchorLiteralHabit{{Kind: "audience_address", Text: "哥哥姐姐们"}},
		AddressingOverride: &model.LiveAnchorAddressingOverride{AudienceMode: "replace", AudienceTerms: []string{"老乡们"}},
	}}
	plain, changed := applyAnchorAddressingOverride("先把已经确认的信息说明白。", profile, 80)
	if changed || plain != "先把已经确认的信息说明白。" {
		t.Fatalf("missing term was mechanically prefixed: changed=%t text=%q", changed, plain)
	}
	replaced, changed := applyAnchorAddressingOverride("哥哥姐姐们，先看已经确认的信息。", profile, 80)
	if !changed || strings.Contains(replaced, "哥哥姐姐们") || !strings.Contains(replaced, "老乡们") {
		t.Fatalf("existing term was not replaced in place: changed=%t text=%q", changed, replaced)
	}
}

func TestApprovedFactTemplateFallbackUsesOnlyAssignedFact(t *testing.T) {
	generation := model.LiveAgentFullShowGenerationContext{AuthorizedFacts: []model.LiveAgentGenerationFact{
		{FactID: "product:1:spec", ProductName: "一号商品", Label: "规格", Value: "5L", CanGenerate: true},
		{FactID: "product:2:price", ProductName: "二号商品", Label: "价格", Value: "99元", CanGenerate: true},
	}}
	text, ok := approvedFactTemplateFallback(generation, model.LiveSpeechExpansionStep{PrimaryFactID: "product:1:spec"}, 60, 120)
	if !ok || !strings.Contains(text, "一号商品") || !strings.Contains(text, "5L") || strings.Contains(text, "99元") || strings.Contains(text, "这个重点大家可以先留意") || len([]rune(text)) >= 60 {
		t.Fatalf("fact template escaped assigned scope: ok=%v text=%q", ok, text)
	}
}

func TestFailedMiddleSegmentSkipsFillerAndNextSegmentStillStreams(t *testing.T) {
	bad := strings.Repeat("库存还剩9999件。", 20)
	good := strings.Repeat("接着把已经确认的内容自然说明白，", 12) + "这一段到这里。"
	g := &styleTestFixtureGateway{outputs: []string{bad, bad, bad, good}}
	observer := &anchorStyleGenerationObserver{}
	generation := model.LiveAgentFullShowGenerationContext{ExpansionPlans: []model.LiveSpeechExpansionPlan{{Steps: []model.LiveSpeechExpansionStep{
		{Index: 1, StartSecond: 0, EndSecond: 30, TargetChars: 100},
		{Index: 2, StartSecond: 30, EndSecond: 60, TargetChars: 100},
	}}}}
	text, _, _, audit, repaired, _, err := generateAnchorStyleTestContinuing(context.Background(), g, generation, "", "解释", 200, nil, observer)
	if err != nil || !audit.Passed || !repaired || text == "" || len(g.calls) != 4 {
		t.Fatalf("fallback/recovery failed: err=%v audit=%+v repaired=%v calls=%d text=%q", err, audit, repaired, len(g.calls), text)
	}
	if len(observer.SkippedSegments) != 1 || observer.SkippedSegments[0] != 1 || len(observer.FallbackSegments) != 0 || strings.Contains(text, "这个重点大家可以先留意") || strings.Contains(text, "9999") {
		t.Fatalf("failed segment leaked or filler fallback survived: observer=%+v text=%q", observer, text)
	}
}

func TestOverlayOnlyPreviewDoesNotRequireSampleStyle(t *testing.T) {
	output := exactSegmentTextSeed(240, 0)
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
	if !strings.Contains(g.calls[0].Messages[1].Content, generation.StyleOverlayPrompt) || strings.Contains(g.calls[0].Messages[1].Content, "主播风格元提示词") {
		t.Fatal("base prompt did not keep the plan overlay separate from anchor style")
	}
}

func TestStyleWrapperChangingNumberFallsBackToAuditedBase(t *testing.T) {
	source := strings.Repeat("哥哥姐姐们啊，我们家把重点讲清楚哟。", 80)
	profile := stylecontract.Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: stylecontract.Version}}, source)
	base := "这款商品正式价格是99元，符合需要的可以点击1号链接查看。"
	g := &styleTestFixtureGateway{outputs: []string{"哥哥姐姐们，这款商品正式价格是100元，现在点击1号链接查看。"}}
	generation := model.LiveAgentFullShowGenerationContext{
		UseAnchorStyle: true, StyleMatchIntensity: 100, AnchorStyle: profile,
		AuthorizedFacts: []model.LiveAgentGenerationFact{{FactID: "price", SourceKind: "product", Predicate: "price", Label: "价格", Value: "99元", LinkKey: "1号链接", CanGenerate: true}},
	}
	history := strings.Repeat("先说明当前内容，再继续补充相关信息。", 90)
	wrapped, _, called, fallback := applyAnchorStyleWrapper(context.Background(), g, generation, "", model.LiveSpeechExpansionStep{ContentRole: "action", CTAType: "direct", PrimaryFactID: "price"}, speechruntime.SegmentSpec{TargetChars: 40, MinChars: 20, MaxChars: 90}, base, history)
	if !called || !fallback || strings.Contains(wrapped, "100元") || !strings.Contains(wrapped, "99元") {
		t.Fatalf("unsafe wrapper did not fall back: called=%t fallback=%t text=%q", called, fallback, wrapped)
	}
}

func TestAnchorStyleRepetitionGuardRejectsLoopAndRepeatedOpening(t *testing.T) {
	looped := "先把已经确认的信息看清楚，再判断是否适合自己。先把已经确认的信息看清楚，再判断是否适合自己。"
	if issues := anchorStyleRepetitionIssues("", looped); len(issues) == 0 {
		t.Fatalf("internal sentence loop was not rejected: %q", looped)
	}
	history := "你们先留意这一点，前面已经把规格说明过了。"
	candidate := "你们先留意这一点，这一段继续讲使用方法。"
	if issues := anchorStyleRepetitionIssues(history, candidate); len(issues) == 0 {
		t.Fatalf("repeated opening was not rejected: history=%q candidate=%q", history, candidate)
	}
	natural := "换到日常做饭的场景来看，这次重点讲下锅后的味道。"
	if issues := anchorStyleRepetitionIssues(history, natural); len(issues) != 0 {
		t.Fatalf("natural transition was rejected: %v", issues)
	}
}
