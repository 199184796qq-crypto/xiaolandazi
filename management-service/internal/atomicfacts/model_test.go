package atomicfacts

import (
	"context"
	"strings"
	"testing"
)

func TestBuildWithModelSplitsAndQuotesSource(t *testing.T) {
	raw := `- [公共] 川渝地区隔天送达；本地发货不抽真空，外地发货采用真空包装。
- [商品] 链接=1；规格=900g。`
	complete := func(context.Context, string) (string, error) {
		return `{"facts":[
			{"source_fact_id":"F001","text":"川渝地区隔天送达","attribute":"时效"},
			{"source_fact_id":"F001","text":"本地发货不抽真空，外地发货采用真空包装","attribute":"包装"},
			{"source_fact_id":"F002","text":"链接=1","attribute":"链接"},
			{"source_fact_id":"F002","text":"规格=900g","attribute":"规格"}
		]}`, nil
	}
	rendered, facts, err := BuildWithModel(context.Background(), raw, complete)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 4 {
		t.Fatalf("facts=%d rendered=%s", len(facts), rendered)
	}
	var shipping, packaging bool
	for _, fact := range facts {
		if strings.Contains(fact.Text, "川渝地区隔天送达") && !strings.Contains(fact.Text, "真空") {
			shipping = true
		}
		if strings.Contains(fact.Text, "本地发货不抽真空，外地发货采用真空包装") {
			packaging = true
		}
	}
	if !shipping || !packaging {
		t.Fatalf("shipping=%v packaging=%v", shipping, packaging)
	}
}

func TestBuildWithModelRejectsParaphrase(t *testing.T) {
	raw := `- [公共] 川渝地区隔天送达；本地发货不抽真空。`
	complete := func(context.Context, string) (string, error) {
		return `{"facts":[{"source_fact_id":"F001","text":"川渝明天就到","attribute":"时效"}]}`, nil
	}
	rendered, _, err := BuildWithModel(context.Background(), raw, complete)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, "明天就到") || !strings.Contains(rendered, "川渝地区隔天送达") {
		t.Fatalf("paraphrase was not rejected: %s", rendered)
	}
}
