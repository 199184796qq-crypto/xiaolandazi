package httpapi

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
)

var segmentTargetPattern = regexp.MustCompile(`本段目标(\d+)字`)

type adaptiveSegmentGateway struct {
	calls         []agentgateway.Request
	oversizeFirst bool
}

func exactSegmentText(size int) string {
	if size < 1 {
		return "。"
	}
	unit := []rune("哥哥姐姐们，咱们顺着这个重点自然说清楚，听明白就行。")
	result := make([]rune, 0, size)
	for len(result) < size {
		remaining := size - len(result)
		if remaining >= len(unit) {
			result = append(result, unit...)
		} else {
			result = append(result, unit[:remaining]...)
		}
	}
	result[len(result)-1] = '。'
	return string(result)
}

func (g *adaptiveSegmentGateway) Complete(_ context.Context, request agentgateway.Request) (agentgateway.Response, error) {
	g.calls = append(g.calls, request)
	target := 100
	for _, message := range request.Messages {
		match := segmentTargetPattern.FindStringSubmatch(message.Content)
		if len(match) == 2 {
			target, _ = strconv.Atoi(match[1])
			break
		}
	}
	if g.oversizeFirst && len(g.calls) == 1 {
		target += 100
	}
	return agentgateway.Response{Text: exactSegmentText(target), Provider: "fixture", Model: "fixture", LatencyMS: 2}, nil
}

func TestAnchorStyleCoreDimensionsPreserveLiteralHabits(t *testing.T) {
	profile := normalizeAnchorStyleProfile(model.LiveAgentPlanAnchorStyleProfile{
		Dimensions: []model.LiveAgentPlanAnchorStyleDimension{
			{Key: "self_address", Rule: "介绍自家时用我们家，不把哥哥姐姐们当自称", Level: "高", Confidence: "high"},
			{Key: "address_position", Rule: "转场句首或强调句尾使用哥哥姐姐们，约每三到五句一次", Level: "高", Confidence: "high"},
			{Key: "catchphrases", Rule: "解释前用我再给大家说一下，句末自然用哟，对呀用于承接", Level: "高", Confidence: "high"},
		},
	})
	for _, key := range []string{"self_address", "address_position", "catchphrases"} {
		found := false
		for _, item := range profile.Dimensions {
			if item.Key == key && item.Confidence == "high" && item.Rule != "" {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing core rule %s", key)
		}
	}
	prompt := anchorStyleTestPrompt(profile, model.LiveAgentFullShowGenerationContext{}, "正式规则", "欢迎新粉", 500)
	for _, expected := range []string{"目标500字", "475到525字", "我们家", "哥哥姐姐们", "我再给大家说一下", "哟", "本次运行预算", "风格热度=70/100", "原文证据只证明说话方式", "这不是摘要任务", "允许同一事实非连续重复", "fact_expansion 是用户明确选择", "换个说法、再重复一遍", "虚拟时间 steps", "内部模拟参数", "没有正式商品事实时", "测试不保存、不发布、不生成声音"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("test prompt missing %q", expected)
		}
	}
}

func TestTrimAnchorStyleCandidateStopsAtSentenceBoundary(t *testing.T) {
	text := strings.Repeat("哥哥姐姐们，这一句是可以直接播放的完整口播。", 120)
	trimmed, ok := trimAnchorStyleCandidate(text, 1800, 2200)
	if !ok {
		t.Fatal("expected long candidate to be trimmed")
	}
	count := len([]rune(trimmed))
	if count < 1800 || count > 2200 {
		t.Fatalf("trimmed chars=%d", count)
	}
	if !strings.HasSuffix(trimmed, "。") {
		t.Fatalf("candidate did not stop naturally: %q", trimmed[len(trimmed)-12:])
	}
}

func TestAnchorStyleTargetRangeAndTokenBudget(t *testing.T) {
	minChars, maxChars := anchorStyleTargetRange(500)
	if minChars != 475 || maxChars != 525 {
		t.Fatalf("range=%d..%d", minChars, maxChars)
	}
	if got := anchorStyleGenerationMaxTokens(500); got != 1400 {
		t.Fatalf("500-char max_tokens=%d", got)
	}
	if got := anchorStyleGenerationMaxTokens(3000); got != 6400 {
		t.Fatalf("3000-char max_tokens=%d", got)
	}
	paragraphs, paragraphChars := anchorStyleParagraphPlan(500)
	if paragraphs != 5 || paragraphChars != 100 {
		t.Fatalf("paragraph plan=%d x %d", paragraphs, paragraphChars)
	}
	short := anchorStyleLengthRepairGuidance(415, 500, 450, 550)
	if !strings.Contains(short, "少35字") || !strings.Contains(short, "不得少于450字") {
		t.Fatalf("short guidance=%s", short)
	}
	long := anchorStyleLengthRepairGuidance(564, 500, 450, 550)
	if !strings.Contains(long, "多14字") || !strings.Contains(long, "不得超过550字") {
		t.Fatalf("long guidance=%s", long)
	}
}

func TestAnchorStyleGenerationUsesOrderedTimeUnitsAndCarriesLengthDebt(t *testing.T) {
	steps := make([]model.LiveSpeechExpansionStep, 4)
	for index := range steps {
		steps[index] = model.LiveSpeechExpansionStep{Index: index + 1, StartSecond: index * 60, EndSecond: (index + 1) * 60, Stage: "fact_direct", Goal: "自然推进", TargetChars: 200, ExpressionMoves: []string{"直述重点", "问后自答"}}
	}
	generation := model.LiveAgentFullShowGenerationContext{ExpansionMode: "fixed_simulation", ExpansionPlans: []model.LiveSpeechExpansionPlan{{Version: model.LiveSpeechExpansionVersion, Mode: "fixed_simulation", VariantKey: "A", TargetChars: 800, Steps: steps}}}
	gateway := &adaptiveSegmentGateway{}
	text, _, check, audit, _, err := generateAnchorStyleTest(context.Background(), gateway, generation, "", "自然说明", 800)
	if err != nil {
		t.Fatal(err)
	}
	if len(gateway.calls) != 4 {
		t.Fatalf("calls=%d want one call per time unit", len(gateway.calls))
	}
	if chars := len([]rune(text)); chars < 760 || chars > 840 {
		t.Fatalf("chars=%d", chars)
	}
	if !check.Passed || !audit.Passed {
		t.Fatalf("check=%+v audit=%+v", check, audit)
	}
	for index, call := range gateway.calls {
		if !strings.Contains(call.Messages[1].Content, "第"+strconv.Itoa(index+1)+"/4个小段") {
			t.Fatalf("call %d is not an ordered time unit", index+1)
		}
		if !strings.Contains(call.Messages[1].Content, "主播时间记忆（仅用于连续承接，不是事实来源）") || !strings.Contains(call.Messages[1].Content, `"memory"`) {
			t.Fatalf("call %d did not receive the shadow mainline context", index+1)
		}
	}
}

func TestAnchorStylePreviewCarriesActiveEngagementIntoNextUnit(t *testing.T) {
	steps := []model.LiveSpeechExpansionStep{
		{Index: 1, StartSecond: 0, EndSecond: 5, TargetChars: 200, Stage: "value", Goal: "先讲清重点", InteractionOpportunity: true},
		{Index: 2, StartSecond: 5, EndSecond: 10, TargetChars: 200, Stage: "scene", Goal: "接着讲使用场景"},
	}
	generation := model.LiveAgentFullShowGenerationContext{
		ExpansionMode:  "fixed_simulation",
		ExpansionPlans: []model.LiveSpeechExpansionPlan{{Version: model.LiveSpeechExpansionVersion, Mode: "fixed_simulation", VariantKey: "A", TargetChars: 400, Steps: steps}},
	}
	gateway := &adaptiveSegmentGateway{}
	if _, _, _, _, _, err := generateAnchorStyleTest(context.Background(), gateway, generation, "", "自然说明", 400); err != nil {
		t.Fatal(err)
	}
	if len(gateway.calls) != 2 || !strings.Contains(gateway.calls[1].Messages[1].Content, `"active_engagement"`) {
		t.Fatalf("active engagement was not carried into the next unit: calls=%d", len(gateway.calls))
	}
}

func TestOversizedSegmentIsReturnedForLocalShortening(t *testing.T) {
	gateway := &adaptiveSegmentGateway{oversizeFirst: true}
	text, _, _, audit, repaired, err := generateAnchorStyleTest(context.Background(), gateway, model.LiveAgentFullShowGenerationContext{}, "", "自然说明", 240)
	if err != nil {
		t.Fatal(err)
	}
	if len(gateway.calls) != 2 || !repaired || !audit.Passed || text == "" {
		t.Fatalf("calls=%d repaired=%v audit=%+v", len(gateway.calls), repaired, audit)
	}
	found := false
	for _, message := range gateway.calls[1].Messages {
		if strings.Contains(message.Content, "请缩短") {
			found = true
		}
	}
	if !found {
		t.Fatal("oversized unit was not returned for shortening")
	}
}
