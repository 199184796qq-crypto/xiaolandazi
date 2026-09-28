package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestCompileFullShowContextSeparatesFormalFactsFromDraftStructure(t *testing.T) {
	plan := model.LiveAgentPlan{ID: 9, Name: "测试方案"}
	facts := []model.LiveAgentPlanFact{
		{Category: "trade", Key: "活动价", Value: "69.9元", Status: "active", VersionNo: 2},
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
	if len(got.ProductLinks) != 1 || got.ProductLinks[0].LinkKey != "1号链接" {
		t.Fatalf("product links=%+v", got.ProductLinks)
	}
	if len(got.ScriptReferences) != 1 || got.ScriptReferences[0].ContentText != "姐妹们先别急着划走" || got.ScriptReferences[0].ExecutionMode != "verbatim" {
		t.Fatalf("script references=%+v", got.ScriptReferences)
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
