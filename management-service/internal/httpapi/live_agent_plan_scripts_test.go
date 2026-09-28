package httpapi

import (
	"encoding/json"
	"slices"
	"testing"

	"livecompanion/management/internal/model"
)

func TestDetectPlanProductLinkKeys(t *testing.T) {
	text := "家用拍1号链接，一号链接今天69.9；商用选2号商品。3号是大规格，九号是另一款。"
	got := detectPlanProductLinkKeys(text)
	want := []string{"1号链接", "2号链接", "3号链接", "9号链接"}
	if !slices.Equal(got, want) {
		t.Fatalf("detected links=%v want=%v", got, want)
	}
}

func TestNormalizePlanScriptAnalysisComputesLinkCoverage(t *testing.T) {
	analysis := model.LiveAgentPlanScriptAnalysis{
		ProductLinks: []model.LiveAgentPlanProductLinkCandidate{
			{LinkKey: "一号链接", ProductName: "黄菜籽油", ReviewBucket: "adoptable"},
		},
		Facts:       []model.LiveAgentPlanFactCandidate{},
		RhythmNodes: []model.LiveAgentPlanRhythmNode{},
	}
	got := normalizePlanScriptAnalysis(analysis, "1号链接黄菜籽油，2号链接黑菜籽油")
	if got.Completeness.LinkCoveragePct != 50 {
		t.Fatalf("coverage=%d want=50", got.Completeness.LinkCoveragePct)
	}
	if !slices.Equal(got.Completeness.MissingLinkKeys, []string{"2号链接"}) {
		t.Fatalf("missing=%v want=[2号链接]", got.Completeness.MissingLinkKeys)
	}
}

func TestNormalizeAnchorStyleProfileKeepsFixedDimensionsAndCandidateStatus(t *testing.T) {
	profile := model.LiveAgentPlanAnchorStyleProfile{
		Summary: "短句偏多，互动频繁。",
		Dimensions: []model.LiveAgentPlanAnchorStyleDimension{
			{
				Key:            "sentence_rhythm",
				Level:          "高",
				Rule:           "重点信息常拆成连续短句表达。",
				EvidenceQuotes: []string{"你看哈，这个先说清楚。", "来，再说一个重点。", "多余证据"},
				Confidence:     "high",
				PromotionLevel: "stable",
			},
		},
		ReusableRules: []string{"重点信息常拆成连续短句表达。", "1号链接69.9元要反复强调"},
	}
	got := normalizeAnchorStyleProfile(profile)
	if len(got.Dimensions) != len(livePlanAnchorStyleDimensionDefinitions) {
		t.Fatalf("dimensions=%d want=%d", len(got.Dimensions), len(livePlanAnchorStyleDimensionDefinitions))
	}
	var sentence model.LiveAgentPlanAnchorStyleDimension
	for _, item := range got.Dimensions {
		if item.Key == "sentence_rhythm" {
			sentence = item
			break
		}
	}
	if sentence.PromotionLevel != "candidate" {
		t.Fatalf("promotion_level=%q want=candidate", sentence.PromotionLevel)
	}
	if len(sentence.EvidenceQuotes) != 2 {
		t.Fatalf("evidence=%d want=2", len(sentence.EvidenceQuotes))
	}
	if !slices.Equal(got.ReusableRules, []string{"重点信息常拆成连续短句表达。"}) {
		t.Fatalf("reusable_rules=%v", got.ReusableRules)
	}
}

func TestNormalizeAnchorStyleProfileRejectsSpecificProductValuesFromRule(t *testing.T) {
	profile := model.LiveAgentPlanAnchorStyleProfile{
		Dimensions: []model.LiveAgentPlanAnchorStyleDimension{
			{
				Key:        "price_expression",
				Level:      "高",
				Rule:       "先说130元，再强调69.9元。",
				Confidence: "high",
			},
		},
	}
	got := normalizeAnchorStyleProfile(profile)
	for _, item := range got.Dimensions {
		if item.Key != "price_expression" {
			continue
		}
		if item.Confidence != "low" || item.Level != "样本不足" {
			t.Fatalf("specific fact rule was not downgraded: %+v", item)
		}
		if livePlanStyleSpecificFactPattern.MatchString(item.Rule) {
			t.Fatalf("sanitized rule still contains specific value: %q", item.Rule)
		}
		return
	}
	t.Fatal("price_expression dimension missing")
}

func TestNormalizePlanScriptAnalysisJSONFlattensNestedQuoteArrays(t *testing.T) {
	raw := `{
  "product_links": [{"link_key":"1号链接","source_quotes":[["证据一"],["证据二","证据一"]]}],
  "facts": [],
  "rhythm_nodes": [],
  "anchor_style": {
    "dimensions": [{"key":"sentence_rhythm","evidence_quotes":[["短句证据"],"另一条"]}],
    "reusable_rules": [["规则一"],"规则二"],
    "candidate_patterns": [],
    "excluded_from_style": []
  }
}`
	var got model.LiveAgentPlanScriptAnalysis
	if err := json.Unmarshal([]byte(normalizePlanScriptAnalysisJSON(raw)), &got); err != nil {
		t.Fatalf("normalized analysis should decode: %v", err)
	}
	if !slices.Equal(got.ProductLinks[0].SourceQuotes, []string{"证据一", "证据二"}) {
		t.Fatalf("source_quotes=%v", got.ProductLinks[0].SourceQuotes)
	}
	if !slices.Equal(got.AnchorStyle.Dimensions[0].EvidenceQuotes, []string{"短句证据", "另一条"}) {
		t.Fatalf("evidence_quotes=%v", got.AnchorStyle.Dimensions[0].EvidenceQuotes)
	}
	if !slices.Equal(got.AnchorStyle.ReusableRules, []string{"规则一", "规则二"}) {
		t.Fatalf("reusable_rules=%v", got.AnchorStyle.ReusableRules)
	}
}
