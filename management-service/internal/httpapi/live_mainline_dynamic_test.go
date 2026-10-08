package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechexpander"
	"livecompanion/management/internal/speechruntime"
)

type dynamicPreviewFixture struct {
	plans       int
	renders     int
	bad         bool
	outputReady *bool
}

func (g *dynamicPreviewFixture) Complete(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
	if req.ResponseFormat == agentgateway.ResponseJSON {
		g.plans++
		if strings.Contains(req.Messages[1].Content, `"future_slots"`) {
			if g.plans > 1 && g.outputReady != nil && !*g.outputReady {
				return agentgateway.Response{}, errors.New("first horizon did not stream before replanning")
			}
			if g.bad {
				return agentgateway.Response{Text: `{"decisions":[{"index":1,"role":"value","primary_fact_id":"invented"}]}`, LatencyMS: 1}, nil
			}
			return agentgateway.Response{Text: `{"decisions":[{"index":1,"role":"scenario","primary_fact_id":"name","support_fact_ids":[],"reason":"先从使用场景进入"},{"index":2,"role":"decision","primary_fact_id":"name","support_fact_ids":[],"reason":"再承接选择判断"}]}`, LatencyMS: 1}, nil
		}
		if g.plans > 1 && g.outputReady != nil && !*g.outputReady {
			return agentgateway.Response{}, errors.New("segment did not reach user before next planning")
		}
		if g.bad {
			return agentgateway.Response{Text: `{"role":"value","primary_fact_id":"invented"}`, LatencyMS: 1}, nil
		}
		role := "scenario"
		if g.plans > 1 {
			role = "decision"
			if !strings.Contains(req.Messages[1].Content, `"completed_units":1`) || !strings.Contains(req.Messages[1].Content, `"content_role":"scenario"`) {
				return agentgateway.Response{}, errors.New("second planning did not receive actual first choice")
			}
		}
		return agentgateway.Response{Text: fmt.Sprintf(`{"role":%q,"primary_fact_id":"name","support_fact_ids":[],"reason":"根据前文自然续接"}`, role), LatencyMS: 1}, nil
	}
	g.renders++
	target := 100
	for _, message := range req.Messages {
		if match := segmentTargetPattern.FindStringSubmatch(message.Content); len(match) == 2 {
			fmt.Sscanf(match[1], "%d", &target)
			break
		}
	}
	return agentgateway.Response{Text: exactSegmentTextSeed(target, g.renders), Provider: "fixture", Model: "fixture", LatencyMS: 1}, nil
}

func TestLongPreviewReplansOnlyAfterEightUnitsHaveStreamed(t *testing.T) {
	steps := make([]model.LiveSpeechExpansionStep, 9)
	for index := range steps {
		steps[index] = model.LiveSpeechExpansionStep{Index: index + 1, EndSecond: (index + 1) * 30, TargetChars: 100}
	}
	facts := []model.LiveAgentGenerationFact{{FactID: "name", SourceKind: "product", Predicate: "product_name", Value: "衬衫", LinkKey: "1", CanGenerate: true}}
	strategy := speechexpander.ResolveContentStrategy(speechexpander.ContentStrategyInput{LiveType: "commerce", ConversionIntensity: 55}, facts)
	ready := false
	streamed := 0
	gateway := &dynamicPreviewFixture{outputReady: &ready}
	observer := &anchorStyleGenerationObserver{Strategy: &strategy, Segment: func(_ string, _ int) { ready = true; streamed++ }}
	generation := model.LiveAgentFullShowGenerationContext{AuthorizedFacts: facts, ExpansionPlans: []model.LiveSpeechExpansionPlan{{Steps: steps}}}
	text, _, _, _, _, _, err := generateAnchorStyleTestContinuing(context.Background(), gateway, generation, "", "介绍衣服", 900, nil, observer)
	if err != nil || text == "" || gateway.plans != 2 || streamed != 9 {
		t.Fatalf("horizon streaming failed: err=%v plans=%d streamed=%d chars=%d", err, gateway.plans, streamed, len([]rune(text)))
	}
}

func TestDynamicPreviewPlansOneHorizonAndStreamsAcceptedSegments(t *testing.T) {
	facts := []model.LiveAgentGenerationFact{{FactID: "name", SourceKind: "product", Predicate: "product_name", Value: "衬衫", LinkKey: "1", CanGenerate: true}}
	strategy := speechexpander.ResolveContentStrategy(speechexpander.ContentStrategyInput{LiveType: "commerce", ConversionIntensity: 55}, facts)
	generation := model.LiveAgentFullShowGenerationContext{AuthorizedFacts: facts, ExpansionPlans: []model.LiveSpeechExpansionPlan{{Steps: []model.LiveSpeechExpansionStep{{Index: 1, EndSecond: 60}, {Index: 2, EndSecond: 120}}}}}
	ready := false
	outputs := []string{}
	gateway := &dynamicPreviewFixture{outputReady: &ready}
	observer := &anchorStyleGenerationObserver{Strategy: &strategy, Segment: func(text string, _ int) { ready = true; outputs = append(outputs, text) }}
	text, _, _, _, _, checkpoint, err := generateAnchorStyleTestContinuing(context.Background(), gateway, generation, "", "介绍衣服", 400, nil, observer)
	if err != nil {
		t.Fatal(err)
	}
	if gateway.plans != 1 || len(outputs) != 2 || len(outputs[0]) >= len(text) || outputs[1] != text {
		t.Fatalf("outputs=%d plans=%d", len(outputs), gateway.plans)
	}
	if strategy.SchedulingMode != "model_horizon_streaming" || len(strategy.ActualSteps) != 2 || strategy.ActualSteps[0].Source != "model_horizon" || strategy.ActualSteps[0].Role != "scenario" || strategy.ActualSteps[1].Role != "action" || strategy.ActualSteps[1].Source != "conversion_quota" || checkpoint.RecentUnits[0].ContentRole != "scenario" {
		t.Fatalf("actual trace=%+v checkpoint=%+v", strategy, checkpoint)
	}
}

func TestInvalidPlanningFallsBackWithoutDiscardingSpeech(t *testing.T) {
	strategy := speechexpander.ResolveContentStrategy(speechexpander.ContentStrategyInput{LiveType: "commerce", ConversionIntensity: 55}, nil)
	gateway := &dynamicPreviewFixture{bad: true}
	observer := &anchorStyleGenerationObserver{Strategy: &strategy}
	text, _, _, _, _, _, err := generateAnchorStyleTestContinuing(context.Background(), gateway, model.LiveAgentFullShowGenerationContext{}, "", "讲当前主题", 240, nil, observer)
	if err != nil || text == "" || strategy.ActualSteps[0].Source != "fallback" {
		t.Fatalf("text=%q strategy=%+v err=%v", text, strategy, err)
	}
}

func TestPlannerRejectsUnprovidedMaterialWithContinuation(t *testing.T) {
	step, receipt, _ := planNextMainlineSegment(context.Background(), &dynamicPreviewFixture{bad: true}, model.LiveAgentFullShowGenerationContext{}, speechexpander.ResolveContentStrategy(speechexpander.ContentStrategyInput{LiveType: "commerce"}, nil), model.LiveSpeechExpansionStep{}, speechruntime.Continuation{CompletedUnits: 12}, `{"memory":{}}`, "", "续讲")
	if receipt.Source != "fallback" || receipt.Index != 13 || step.PrimaryFactID != "" {
		t.Fatalf("invalid decision escaped: %+v", receipt)
	}
}

func TestConversionQuotaCorrectsAllValuePlanWithoutInventingScarcity(t *testing.T) {
	planned := make([]model.LiveSpeechExpansionStep, 8)
	receipts := make([]speechexpander.ContentDecisionReceipt, 8)
	for index := range planned {
		planned[index] = model.LiveSpeechExpansionStep{Index: index + 1, ContentRole: "value", Stage: "value", MainlinePhase: "body"}
		receipts[index] = speechexpander.ContentDecisionReceipt{Index: index + 1, Role: "value", Source: "model_horizon"}
	}
	facts := []model.LiveAgentGenerationFact{{FactID: "size", SourceKind: "product", Predicate: "spec", Label: "尺码", Value: "S到XL", LinkKey: "1号链接", CanGenerate: true}}
	strategy := speechexpander.ResolveContentStrategy(speechexpander.ContentStrategyInput{LiveType: "commerce", ConversionIntensity: 100}, facts)
	planned, _ = enforceMainlineConversionQuota(planned, receipts, facts, strategy, 0)
	actions := 0
	for _, step := range planned {
		if step.ContentRole != "action" {
			continue
		}
		actions++
		if step.CTAType != "confirm" {
			t.Fatalf("unsupported CTA selected: %+v", step)
		}
	}
	if actions != 4 {
		t.Fatalf("actions=%d want=4 plan=%+v", actions, planned)
	}
}
