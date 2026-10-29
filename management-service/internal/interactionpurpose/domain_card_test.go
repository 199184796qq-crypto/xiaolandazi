package interactionpurpose

import (
	"context"
	"strings"
	"testing"

	"livecompanion/management/internal/domaincard"
	"livecompanion/management/internal/semantic"
)

func TestResolveCarriesDomainCardAndMatchedFacts(t *testing.T) {
	repo := &memoryRepo{documents: map[string]semantic.StoredDocument{}}
	var payload string
	resolver := Resolver{Repo: repo, Complete: func(_ context.Context, value string) (string, error) {
		payload = value
		return `{"question":true,"relevant":true,"confidence":0.96,"purpose_id":"","purpose":"确认上海是否抽真空","scope":"上海","task_angles":[]}`, nil
	}}
	card := &domaincard.Card{
		SourceSHA256: "card-sha",
		Dimensions: []domaincard.Dimension{{
			Name: "包装方式", Phrases: []string{"抽真空吗"}, Conditions: []string{"本地", "外地"}, FactIDs: []string{"F001"},
		}},
	}
	input := Input{
		Tenant: 14, Room: 15, Plan: 2, Text: "上海抽真空吗",
		Facts: "- [包装] 本地发货不抽真空，外地发货采用真空包装。", Prompt: "v1",
		DomainCard: card,
		CardFacts:  []domaincard.Fact{{ID: "F001", Text: "本地发货不抽真空，外地发货采用真空包装。"}},
		Semantics:  []SemanticEntry{{Keywords: []string{"砍", "切块"}, Sense: "规格与切块", Exclude: []string{"价", "便宜"}}},
	}
	result, err := resolver.Resolve(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload, "领域卡") || !strings.Contains(payload, "本问题命中事实") {
		t.Fatalf("intent payload missing card context: %s", payload)
	}
	if !strings.Contains(payload, "全量事实") {
		t.Fatalf("intent payload missing full facts: %s", payload)
	}
	if !strings.Contains(payload, "语义库") || !strings.Contains(payload, "规格与切块") {
		t.Fatalf("intent payload missing semantics: %s", payload)
	}
	if len(result.Angles) == 0 {
		t.Fatal("card facts should produce an angle")
	}
	got := result.Angles[0].FactIDs
	if len(got) != 1 || got[0] != "F001" {
		t.Fatalf("card facts not used: %v", got)
	}
}
