package agentunderstanding

import "testing"

func TestFactWordingStructuredNaturalLanguageDoesNotMergeFields(t *testing.T) {
	ctx := LiveStrategyProgramContext{CurrentMode: "knowledge", Facts: []LiveStrategyFact{{Category: "trade", Key: "售后政策", Value: "原始事实"}}}
	out := ProgramInterpretLiveStrategy("添加事实 trade-新售后：支持退货；不能这么说：永远包退；可以这么说：按实际规则处理。", ctx)
	if out.Kind != KindCommand || out.Intent != "fact.add" || out.Changes["fact_value"] != "支持退货" || out.Changes["forbidden_wording"] != "永远包退" || out.Changes["safe_rewrite"] != "按实际规则处理。" {
		t.Fatal(out)
	}
	out = ProgramInterpretLiveStrategy("修改事实售后政策；不能这么说：无条件退货；可以这么说：按售后政策办理。", ctx)
	if out.Kind != KindCommand || out.Intent != "fact.update" || out.Target["fact_key"] != "售后政策" || out.Changes["forbidden_wording"] != "无条件退货" {
		t.Fatal(out)
	}
	if _, exists := out.Changes["fact_value"]; exists {
		t.Fatal("wording-only edit overwrote fact", out)
	}
	for _, text := range []string{"添加事实 trade-新售后：退货，别说永远包退，改成按规则办理。", "修改事实售后政策；不能这么说：甲；不能这么说：乙"} {
		out = ProgramInterpretLiveStrategy(text, ctx)
		if out.Kind != KindClarify {
			t.Fatal("ambiguous prose guessed", out)
		}
	}
	ctx.Facts = append(ctx.Facts, LiveStrategyFact{Category: "other", Key: "售后政策"})
	if out = ProgramInterpretLiveStrategy("修改事实售后政策；可以这么说：按规则办理。", ctx); out.Kind != KindClarify {
		t.Fatal("ambiguous target adopted", out)
	}
}

func TestFactCategoryAddAndImageRemainConfirmationOnly(t *testing.T) {
	out := ProgramInterpretLiveStrategy("新建一类「算账口径」", LiveStrategyProgramContext{CurrentMode: "knowledge"})
	if out.Kind != KindCommand || out.Intent != "fact_category.add" {
		t.Fatal(out)
	}
	category := out.Changes["new_fact_category"].(map[string]any)
	if category["label"] != "算账口径" {
		t.Fatal(category)
	}
	out = ProgramInterpretLiveStrategy("添加事实分类价格核算，同时添加事实名称商品价格", LiveStrategyProgramContext{CurrentMode: "knowledge"})
	if out.Kind != KindClarify {
		t.Fatal("combined request partially treated as category-only", out)
	}
	out = ProgramInterpretLiveStrategy("@图片1 添加事实", LiveStrategyProgramContext{HasImages: true})
	if out.Kind != KindClarify {
		t.Fatal("program guessed image", out)
	}
}
