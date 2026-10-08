package httpapi

import (
	"livecompanion/management/internal/agentunderstanding"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func TestStrategyFactWordingOnlyUpdatePreservesUnmentionedFields(t *testing.T) {
	current := model.LiveAgentPlanFact{Category: "trade", Key: "售后", Value: "支持退货", ForbiddenWording: "禁止夸大", SafeRewrite: "按规则办理"}
	newText := "不说无条件退货"
	c := liveStrategyFactUpdateCandidate(current, liveStrategyExecutePayload{ForbiddenWording: &newText})
	if c.Value != current.Value || c.SafeRewrite != current.SafeRewrite || c.ForbiddenWording != newText {
		t.Fatal(c)
	}
	empty := ""
	c = liveStrategyFactUpdateCandidate(current, liveStrategyExecutePayload{SafeRewrite: &empty, FactValue: current.Value})
	if c.SafeRewrite != "" || c.ForbiddenWording != current.ForbiddenWording {
		t.Fatal("explicit clear or field preservation failed", c)
	}
	raw := `{"kind":"command","intent":"fact.update","target":{"fact_key":"售后"},"changes":{"forbidden_wording":"不说无条件退货","safe_rewrite":""}}`
	var out liveStrategyIntentModelOutput
	if err := decodeProductJSON(raw, &out); err != nil || out.Changes.ForbiddenWording == nil || out.Changes.SafeRewrite == nil || *out.Changes.SafeRewrite != "" {
		t.Fatal(out, err)
	}
	program := liveStrategyProgramOutput(agentunderstanding.UnifiedIntent{Kind: "command", Intent: "fact.update", Changes: map[string]any{"safe_rewrite": ""}})
	if program.Changes.SafeRewrite == nil || program.Changes.ForbiddenWording != nil {
		t.Fatal("program dropped empty-vs-absent distinction", program)
	}
}

func TestStrategyCustomCategoryResolutionAndPrompt(t *testing.T) {
	catalog, _, err := loadFactCategoryCatalog()
	if err != nil {
		t.Fatal(err)
	}
	input := &liveStrategyNewFactCategory{Label: "算账口径", Family: "product", Description: "商品价格的算账方式"}
	c, isNew, err := proposedFactCategory(catalog, input)
	if err != nil || !isNew || !strings.HasPrefix(c.Value, "custom_") || c.Family != "product" {
		t.Fatal(c, isNew, err)
	}
	candidate := model.LiveAgentPlanFactCandidate{Category: c.Value, Key: "算账方式", Value: "按实际价格计算", ForbiddenWording: "不能虚构差价", SafeRewrite: "按已有价格计算"}
	if validateLiveAgentPlanFactCandidate(candidate, catalog) == nil {
		t.Fatal("unknown category accepted before confirmation")
	}
	catalog.Categories = append(catalog.Categories, c)
	if err = validateLiveAgentPlanFactCandidate(candidate, catalog); err != nil {
		t.Fatal(err)
	}
	existing, isNew, err := proposedFactCategory(catalog, input)
	if err != nil || isNew || existing.Value != c.Value {
		t.Fatal("same-label category not reused")
	}
	if _, _, err = proposedFactCategory(catalog, &liveStrategyNewFactCategory{Label: input.Label, Family: "fulfillment"}); err == nil {
		t.Fatal("existing category silently reclassified")
	}
	md, source, err := loadLiveStrategyIntentPrompt()
	if err != nil || source == "" {
		t.Fatal(err)
	}
	for _, token := range []string{"{{categories}}", "fact_category.add", "new_fact_category", "forbidden_wording", "safe_rewrite", "省略fact_value", "图片"} {
		if !strings.Contains(md, token) {
			t.Fatal("missing prompt protocol", token)
		}
	}
	if normalizeLiveStrategyIntent("fact_category.add") != "fact_category.add" {
		t.Fatal("new category intent dropped")
	}
	if _, ok := liveStrategyExecutableActions["add_live_fact_category"]; !ok {
		t.Fatal("category has no executor")
	}
}
