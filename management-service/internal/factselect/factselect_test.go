package factselect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "configs", "prompts", "anchor-style", "fact-bound.json"))
	if err == nil {
		os.Setenv("FACT_BOUND_CONFIG", path)
	}
	os.Exit(m.Run())
}

func yangDuckFacts() []Fact {
	return []Fact{
		{ID: "F001", Text: "店铺有全场包邮活动，活动长期有效"},
		{ID: "F002", Text: "发货统一走京东物流，京东配送到家"},
		{ID: "F003", Text: "川渝地区隔天送达"},
		{ID: "F004", Text: "本地发货不抽真空，外地发货用真空包装"},
		{ID: "F005", Text: "下单后第二天上午发货"},
	}
}

func TestTibetQuestionDropsOtherRegionFact(t *testing.T) {
	picked := Select("西藏包邮吗", nil, nil, yangDuckFacts(), 3)
	if len(picked) == 0 {
		t.Fatal("no facts selected")
	}
	for _, fact := range picked {
		if strings.Contains(fact.Text, "川渝") {
			t.Fatalf("fact about another region kept: %s", fact.Text)
		}
	}
	if !strings.Contains(picked[0].Text, "包邮") {
		t.Fatalf("top fact should be the shipping benefit: %+v", picked)
	}
}

func TestAngleFactsWinAndLimitApplies(t *testing.T) {
	angleIDs := map[string]bool{"F002": true}
	picked := Select("发什么快递", []string{"物流 配送方式"}, angleIDs, yangDuckFacts(), 2)
	if len(picked) == 0 || picked[0].ID != "F002" {
		t.Fatalf("angle fact should rank first: %+v", picked)
	}
	if len(picked) > 2 {
		t.Fatalf("limit not applied: %d", len(picked))
	}
}

func TestNoOverlapKeepsSmallOriginalSelection(t *testing.T) {
	picked := Select("多少钱", nil, nil, yangDuckFacts(), 3)
	if len(picked) == 0 || len(picked) > 2 {
		t.Fatalf("fallback should keep at most two facts: %+v", picked)
	}
}
