package domaincard

import (
	"strings"
	"testing"
)

func testFacts() []Fact {
	return []Fact{
		{ID: "F016", Text: "川渝地区隔天送达。"},
		{ID: "F026", Text: "海南、新疆、西藏地区只能发空运，需要补空运费。"},
		{ID: "F030", Text: "本地发货不抽真空，外地发货采用真空包装。"},
		{ID: "F033", Text: "抽真空的收到后微波炉叮1分钟。"},
	}
}

func TestValidateAndMatch(t *testing.T) {
	card, err := Parse(`{
		"domain":"卤味熟食",
		"product":"绸都杨鸭子",
		"dimensions":[
			{"name":"包装方式","phrases":["抽真空吗","真空包装"],"conditions":["本地","外地"],"fact_ids":["F030","F033"]},
			{"name":"配送时效","phrases":["什么时候到","几天能到","隔天送达"],"conditions":["川渝"],"fact_ids":["F016"]},
			{"name":"运费与包邮","phrases":["包邮吗","空运补运费"],"conditions":["偏远地区"],"fact_ids":["F026"]}
		]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := NormalizeAndValidate(card, testFacts()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		question string
		want     []string
	}{
		{"上海抽真空吗", []string{"F030", "F033"}},
		{"上海什么时候到", []string{"F016"}},
		{"西藏包邮吗", []string{"F026"}},
	} {
		got := Match(tc.question, card, testFacts())
		ids := make([]string, 0, len(got))
		for _, fact := range got {
			ids = append(ids, fact.ID)
		}
		if strings.Join(ids, ",") != strings.Join(tc.want, ",") {
			t.Fatalf("%s: got %v want %v", tc.question, ids, tc.want)
		}
	}
}

func TestValidateAddsMissingFactsToOther(t *testing.T) {
	card := &Card{Dimensions: []Dimension{{Name: "包装", Phrases: []string{"抽真空"}, FactIDs: []string{"F030"}}}}
	if err := NormalizeAndValidate(card, testFacts()); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, dimension := range card.Dimensions {
		for _, id := range dimension.FactIDs {
			seen[id] = true
		}
	}
	for _, fact := range testFacts() {
		if !seen[fact.ID] {
			t.Fatalf("fact %s is not covered", fact.ID)
		}
	}
}

func TestMatchReturnsNilWhenNoDimensionMatches(t *testing.T) {
	card := &Card{Dimensions: []Dimension{{Name: "包装", Phrases: []string{"抽真空"}, FactIDs: []string{"F030"}}}}
	if got := Match("今天天气怎么样", card, testFacts()); len(got) != 0 {
		t.Fatalf("unexpected match: %+v", got)
	}
}

func TestFromGenerationReadsStoredCard(t *testing.T) {
	card := &Card{Dimensions: []Dimension{{Name: "包装", Phrases: []string{"抽真空"}, FactIDs: []string{"F030"}}}}
	if got := FromGeneration(map[string]any{"domain_card": card}); got == nil || len(got.Dimensions) != 1 {
		t.Fatalf("stored card not read: %+v", got)
	}
	if got := FromGeneration(map[string]any{}); got != nil {
		t.Fatalf("absent card should be nil: %+v", got)
	}
}

func TestExpandSharedFactsAddsConditionRuleToSecondDimension(t *testing.T) {
	facts := []Fact{
		{ID: "F008", Text: "川渝地区隔天送达；本地发货不抽真空，外地发货采用真空包装。"},
		{ID: "F033", Text: "商品卡：包装=麻辣抽真空，麻辣不抽真空。"},
	}
	card := &Card{Dimensions: []Dimension{
		{Name: "发货与快递", Phrases: []string{"什么时候到"}, FactIDs: []string{"F008"}},
		{Name: "包装方式", Phrases: []string{"抽真空吗"}, FactIDs: []string{"F033"}},
	}}
	if err := NormalizeAndValidate(card, facts); err != nil {
		t.Fatal(err)
	}
	got := Match("上海抽真空吗", card, facts)
	ids := make([]string, 0, len(got))
	for _, fact := range got {
		ids = append(ids, fact.ID)
	}
	if !containsString(ids, "F008") || !containsString(ids, "F033") {
		t.Fatalf("shared packaging rule missing: %v", ids)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
