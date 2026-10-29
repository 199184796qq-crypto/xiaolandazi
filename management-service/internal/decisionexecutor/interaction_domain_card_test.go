package decisionexecutor

import (
	"strings"
	"testing"

	"livecompanion/management/internal/domaincard"
)

func TestSingleAnswerFactsUsesDomainCardConditionRules(t *testing.T) {
	full := `- [公共；包装] 本地发货不抽真空，外地发货采用真空包装。
- [公共；包装] 抽真空的收到后微波炉叮1分钟。
- [公共；时效] 川渝地区隔天送达。
- [公共；运费] 新疆、西藏只能发空运，需要补空运费。`
	card := &domaincard.Card{Dimensions: []domaincard.Dimension{
		{Name: "包装方式", Phrases: []string{"抽真空吗", "真空包装"}, Conditions: []string{"本地", "外地"}, FactIDs: []string{"F001", "F002"}},
		{Name: "配送时效", Phrases: []string{"什么时候到", "几天能到"}, Conditions: []string{"川渝"}, FactIDs: []string{"F003"}},
		{Name: "运费与包邮", Phrases: []string{"包邮吗", "空运补运费"}, Conditions: []string{"偏远地区"}, FactIDs: []string{"F004"}},
	}}
	item := decisionItem{
		FullFacts:       full,
		DomainCard:      card,
		SampleQuestions: []string{"上海抽真空吗"},
	}
	got := (&Worker{}).singleAnswerFacts(item)
	if !strings.Contains(got, "外地发货采用真空包装") {
		t.Fatalf("full facts missing packaging rule: %s", got)
	}
}

func TestSingleAnswerFactsFallsBackWithoutCard(t *testing.T) {
	item := decisionItem{
		FullFacts:       "- [公共；包装] 外地发货采用真空包装。",
		SampleQuestions: []string{"上海抽真空吗"},
	}
	if got := (&Worker{}).singleAnswerFacts(item); !strings.Contains(got, "外地发货采用真空包装") {
		t.Fatalf("full facts should be returned without a card, got %s", got)
	}
}
