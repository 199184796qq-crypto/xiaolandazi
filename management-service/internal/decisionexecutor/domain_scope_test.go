package decisionexecutor

import (
	"strings"
	"testing"

	"livecompanion/management/internal/domaincard"
)

func TestApplyDomainAnswerScopeForChengdu(t *testing.T) {
	item := &decisionItem{
		FullFacts: `- [发货] 川渝地区隔天送达；本地发货不抽真空，外地发货采用真空包装。
- [发货] 快递选择只有京东物流。
- [空运] 新疆、西藏只能发空运，需要补空运费。
- [价格] 线上售价85.6元。`,
		SampleQuestions: []string{"成都发吗"},
		Title:           "确认发货地能否发往成都",
		DomainCard: &domaincard.Card{Dimensions: []domaincard.Dimension{
			{Name: "发货与快递", Phrases: []string{"发什么快递", "多久到"}, FactIDs: []string{"F001", "F002"}},
			{Name: "偏远地区空运", Phrases: []string{"新疆能发吗"}, FactIDs: []string{"F003"}},
			{Name: "价格与优惠", Phrases: []string{"多少钱"}, FactIDs: []string{"F004"}},
		}},
	}
	applyDomainAnswerScope(item)
	if !strings.Contains(item.ScopedFacts, "F001") || !strings.Contains(item.ScopedFacts, "F002") {
		t.Fatalf("shipping facts missing: %q", item.ScopedFacts)
	}
	if strings.Contains(item.ScopedFacts, "F003") || strings.Contains(item.ScopedFacts, "F004") {
		t.Fatalf("forbidden facts leaked: %q", item.ScopedFacts)
	}
	if !strings.Contains(item.AnswerScope, "发货与快递") || !strings.Contains(item.AnswerScope, "价格与优惠") {
		t.Fatalf("answer scope missing: %q", item.AnswerScope)
	}
}
