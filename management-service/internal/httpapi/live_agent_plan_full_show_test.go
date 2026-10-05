package httpapi

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechexpander"
)

type concurrentSegmentGateway struct {
	mu    sync.Mutex
	calls int
}

func (g *concurrentSegmentGateway) Complete(_ context.Context, request agentgateway.Request) (agentgateway.Response, error) {
	target := 100
	for _, message := range request.Messages {
		if match := segmentTargetPattern.FindStringSubmatch(message.Content); len(match) == 2 {
			fmtTarget, _ := strconv.Atoi(match[1])
			target = fmtTarget
			break
		}
	}
	g.mu.Lock()
	g.calls++
	g.mu.Unlock()
	return agentgateway.Response{Text: exactSegmentText(target), Provider: "fixture", Model: "fixture", LatencyMS: 1}, nil
}

func TestCompileFullShowContextSeparatesFormalFactsFromDraftStructure(t *testing.T) {
	plan := model.LiveAgentPlan{ID: 9, Name: "测试方案"}
	facts := []model.LiveAgentPlanFact{
		{Category: "trade", Key: "活动价", Value: "69.9元", ForbiddenWording: "老人小孩吃了更好", SafeRewrite: "家里日常做饭可以按页面信息选择", Status: "active", VersionNo: 2},
		{Category: "other", Key: "停用项", Value: "不要用", Status: "disabled", VersionNo: 1},
	}
	input := model.LiveAgentFullShowPreviewInput{
		DurationMinutes: 90,
		RoundMinutes:    7,
		VariantCount:    5,
		UseAnchorStyle:  true,
		ProductLinks: []model.LiveAgentPlanProductLinkCandidate{
			{LinkKey: "1号链接", ProductName: "黄菜籽油", ActivityPrice: "69.9元", ReviewBucket: "adoptable", SourceQuotes: []string{"原文证据"}},
			{LinkKey: "2号链接", ProductName: "冲突商品", ReviewBucket: "conflict"},
		},
		RhythmNodes: []model.LiveAgentPlanRhythmNode{
			{Order: 1, Title: "开场", ExecutionMode: "verbatim", FixedText: "69.9元必须这样说"},
		},
		AnchorStyle: model.LiveAgentPlanAnchorStyleProfile{
			Dimensions: []model.LiveAgentPlanAnchorStyleDimension{
				{Key: "sentence_rhythm", Group: "language", Label: "句子节奏", Level: "高", Rule: "重点信息拆成连续短句", EvidenceQuotes: []string{"69.9元就是今天价格"}},
				{Key: "qa_structure", Group: "interaction", Label: "答疑结构", Level: "样本不足", Rule: "证据不足"},
			},
			ReusableRules: []string{"重点信息拆成连续短句"},
		},
	}

	got := compileFullShowContext(plan, facts, nil, []model.LiveAgentPlanProductLink{{
		LinkKey: "1号链接", ProductName: "黄菜籽油", Spec: "5L", DailyPrice: "130元", Status: "active",
	}}, []model.LiveAgentPlanScriptReference{{
		ReferenceKey: "开场", Title: "开场参考", ContentText: "姐妹们先别急着划走", ExecutionMode: "verbatim", Status: "active", VersionNo: 2,
	}}, input)
	if len(got.FormalFacts) != 1 || got.FormalFacts[0].Value != "69.9元" {
		t.Fatalf("formal facts=%+v", got.FormalFacts)
	}
	if got.FormalFacts[0].ForbiddenWording != "老人小孩吃了更好" || got.FormalFacts[0].SafeRewrite == "" {
		t.Fatalf("fact boundary=%+v", got.FormalFacts[0])
	}
	if len(got.ProductLinks) != 1 || got.ProductLinks[0].LinkKey != "1号链接" {
		t.Fatalf("product links=%+v", got.ProductLinks)
	}
	if got.FactManifestVersion != model.LiveGenerationFactManifestVersion || len(got.AuthorizedFacts) != 4 {
		t.Fatalf("unified generation facts missing: version=%s facts=%+v", got.FactManifestVersion, got.AuthorizedFacts)
	}
	if len(got.ScriptReferences) != 0 {
		t.Fatalf("script references must not enter generation context: %+v", got.ScriptReferences)
	}
	if len(got.RhythmNodes) != 1 || got.RhythmNodes[0].FixedText != "" || got.RhythmNodes[0].ExecutionMode != "intent" {
		t.Fatalf("rhythm node not sanitized: %+v", got.RhythmNodes)
	}
	if len(got.AnchorStyle.Dimensions) != 1 || len(got.AnchorStyle.Dimensions[0].EvidenceQuotes) != 0 {
		t.Fatalf("style evidence leaked or sample-insufficient rule retained: %+v", got.AnchorStyle.Dimensions)
	}
	if got.RoundCount != 13 {
		t.Fatalf("round_count=%d want=13", got.RoundCount)
	}
	if got.ExpansionMode != "fixed_simulation" || len(got.ExpansionPlans) != 5 || got.ExpansionPlans[0].DurationSeconds != 420 {
		t.Fatalf("virtual time expansion plan missing: mode=%s plans=%+v", got.ExpansionMode, got.ExpansionPlans)
	}
	if got.FactExpansion.Freedom != 65 || got.FactExpansion.Level != "open" || !got.FactExpansion.UserAuthorized {
		t.Fatalf("fact expansion authorization missing: %+v", got.FactExpansion)
	}
}

func TestCompileFullShowContextRejectsGhostAndExpiredBenefits(t *testing.T) {
	now := time.Date(2026, 10, 5, 20, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	got := compileFullShowContextAt(
		model.LiveAgentPlan{ID: 10, Name: "事实编译"},
		nil,
		[]model.LiveAgentPlanBenefit{
			{ID: 1, Key: "1号链接:current-benefit", LinkKey: "1号链接", ActivityPrice: "109.9元", Status: "active", StartsAt: &past, EndsAt: &future, VersionNo: 2},
			{ID: 2, Key: "2号链接:current-benefit", LinkKey: "2号链接", ActivityPrice: "89.9元", Status: "active", StartsAt: &past, EndsAt: &future, VersionNo: 1},
			{ID: 3, Key: "expired", LinkKey: "1号链接", Gift: "过期赠品", Status: "active", EndsAt: &past, VersionNo: 1},
			{ID: 4, Key: "room-wide", Gift: "全场赠品", Status: "active", StartsAt: &past, EndsAt: &future, VersionNo: 1},
		},
		[]model.LiveAgentPlanProductLink{
			{ID: 11, LinkKey: "1号链接", ProductName: "菜籽油", Spec: "5L", Status: "active", VersionNo: 3},
			{ID: 12, LinkKey: "2号链接", ProductName: "幽灵商品", Status: "disabled", VersionNo: 4},
		},
		model.LiveAgentFullShowPreviewInput{DurationMinutes: 30, RoundMinutes: 5, VariantCount: 1},
		now,
	)
	if len(got.ProductLinks) != 1 || got.ProductLinks[0].LinkKey != "1号链接" {
		t.Fatalf("inactive product leaked into generation: %+v", got.ProductLinks)
	}
	if len(got.Benefits) != 2 || got.Benefits[0].ID != 1 || got.Benefits[1].ID != 4 {
		t.Fatalf("ghost or expired benefit handling failed: %+v", got.Benefits)
	}
	for _, fact := range got.AuthorizedFacts {
		if strings.Contains(fact.Value, "89.9") || strings.Contains(fact.Value, "过期") || strings.Contains(fact.ProductName, "幽灵") {
			t.Fatalf("ghost fact entered unified manifest: %+v", fact)
		}
		if !fact.CanGenerate || fact.Status != "active" {
			t.Fatalf("unauthorized fact entered manifest: %+v", fact)
		}
	}
}

func TestFullShowVariantsAreGeneratedAsOrderedSmallUnits(t *testing.T) {
	generation := model.LiveAgentFullShowGenerationContext{PlanName: "持续口播测试", RoundMinutes: 2, VariantCount: 2, ExpansionMode: "fixed_simulation"}
	generation.ExpansionPlans = speechexpander.BuildFixedPlans(speechexpander.Input{DurationMinutes: 2, TargetChars: 500, VariantCount: 2})
	gateway := &concurrentSegmentGateway{}
	variants, provider, modelName, latency, err := generateFullShowVariants(context.Background(), generation, "", nil, gateway)
	if err != nil {
		t.Fatal(err)
	}
	if len(variants) != 2 || provider != "fixture" || modelName != "fixture" || latency != 4 {
		t.Fatalf("variants=%d provider=%s model=%s latency=%d", len(variants), provider, modelName, latency)
	}
	gateway.mu.Lock()
	calls := gateway.calls
	gateway.mu.Unlock()
	if calls != 4 {
		t.Fatalf("calls=%d want 2 variants x 2 time units", calls)
	}
	for _, variant := range variants {
		chars := len([]rune(variant.Text))
		if chars < 475 || chars > 525 || !strings.Contains(variant.Text, "\n\n") {
			t.Fatalf("variant %s was not assembled from time units: chars=%d text=%q", variant.VariantKey, chars, variant.Text)
		}
	}
}

func TestAuditFullShowVariantsRejectsInventedNumbersAndHardcodedInventory(t *testing.T) {
	ctx := model.LiveAgentFullShowGenerationContext{
		FormalFacts: []model.LiveAgentFullShowContextFact{
			{Key: "活动价", Value: "69.9元"},
		},
		ProductLinks: []model.LiveAgentPlanProductLink{
			{LinkKey: "1号链接", ProductName: "黄菜籽油"},
		},
		RoundMinutes:    7,
		UseDynamicFacts: true,
	}
	variants := []model.LiveAgentFullShowVariant{
		{VariantKey: "A", Text: strings.Repeat("今天一号链接活动价六十九块九，家里炒菜正常讲。", 45) + "库存还剩6桶。"},
		{VariantKey: "B", Text: strings.Repeat("今天1号链接活动价69.9元，家里炒菜正常讲。", 45) + "另外只要59元。"},
	}

	got := auditFullShowVariants(ctx, variants, nil)
	if got[0].Audit.Passed {
		t.Fatalf("hardcoded inventory should fail: %+v", got[0].Audit)
	}
	if got[1].Audit.Passed {
		t.Fatalf("invented number should fail: %+v", got[1].Audit)
	}
	foundInventory := false
	foundUnknownNumber := false
	for _, issue := range got[0].Audit.Issues {
		if issue.Code == "hardcoded_inventory" {
			foundInventory = true
		}
	}
	for _, issue := range got[1].Audit.Issues {
		if issue.Code == "unknown_numeric_claim" {
			foundUnknownNumber = true
		}
	}
	if !foundInventory || !foundUnknownNumber {
		t.Fatalf("missing expected audit issues inventory=%t unknown_number=%t", foundInventory, foundUnknownNumber)
	}
}

func TestFullShowSimilarityPercentDetectsNearDuplicate(t *testing.T) {
	left := strings.Repeat("刚进来的朋友先别急，今天我把这个产品怎么选给你讲清楚。", 20)
	right := left + "最后看一眼链接。"
	if score := fullShowSimilarityPercent(left, right); score < 70 {
		t.Fatalf("similarity=%d want>=70", score)
	}
}

func TestAuditFullShowVariantsRejectsSimulatedRoomSignals(t *testing.T) {
	ctx := model.LiveAgentFullShowGenerationContext{RoundMinutes: 1}
	variants := auditFullShowVariants(ctx, []model.LiveAgentFullShowVariant{{Text: "直播间现在有36个人，刚进来的朋友听我说。"}}, nil)
	if variants[0].Audit.Passed {
		t.Fatalf("simulated room state leaked into speech: %+v", variants[0].Audit)
	}
	found := false
	for _, issue := range variants[0].Audit.Issues {
		if issue.Code == "synthetic_room_signal" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing synthetic room signal issue: %+v", variants[0].Audit.Issues)
	}
}

func TestAuditFullShowVariantsRejectsCompositionMetaLeak(t *testing.T) {
	ctx := model.LiveAgentFullShowGenerationContext{RoundMinutes: 1}
	variants := auditFullShowVariants(ctx, []model.LiveAgentFullShowVariant{{Text: "品牌背书再重复一遍也不多余，这个信息再换个说法收一下。"}}, nil)
	if variants[0].Audit.Passed {
		t.Fatalf("composition instructions leaked into speech: %+v", variants[0].Audit)
	}
	found := false
	for _, issue := range variants[0].Audit.Issues {
		if issue.Code == "composition_meta_leak" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing composition meta leak issue: %+v", variants[0].Audit.Issues)
	}
}

func TestAuditFullShowVariantsRejectsForbiddenFactWording(t *testing.T) {
	ctx := model.LiveAgentFullShowGenerationContext{
		FormalFacts:  []model.LiveAgentFullShowContextFact{{Key: "适用表达", Value: "页面有明确配料信息", ForbiddenWording: "老人小孩吃了更好；包治百病", SafeRewrite: "可以按页面信息了解"}},
		RoundMinutes: 1,
	}
	got := auditFullShowVariants(ctx, []model.LiveAgentFullShowVariant{{Text: "这款油老人小孩吃了更好，大家放心买。"}}, nil)
	if got[0].Audit.Passed {
		t.Fatalf("forbidden fact wording should fail: %+v", got[0].Audit)
	}
	for _, issue := range got[0].Audit.Issues {
		if issue.Code == "forbidden_fact_wording" {
			return
		}
	}
	t.Fatalf("missing forbidden fact wording issue: %+v", got[0].Audit.Issues)
}
