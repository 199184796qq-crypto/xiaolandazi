package factbound

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

const duckFacts = "F034 - [有效福利] 赠品=免邮费；活动=包邮\n" +
	"F007 - [fulfillment/快递选择] 快递选择只有京东物流。\n" +
	"F019 - [other/配送时效与包装建议] 川渝地区隔天送达；本地发货不抽真空。"

func TestTibetIsPartial(t *testing.T) {
	result := Evaluate("西藏包邮吗？", duckFacts, true)
	if result.State != Partial {
		t.Fatalf("state=%s want partial (%+v)", result.State, result)
	}
	if len(result.Uncovered) != 1 || result.Uncovered[0].Text != "西藏" {
		t.Fatalf("uncovered=%+v", result.Uncovered)
	}
	if !OverClaims("朋友们，西藏包邮的，免邮费。") {
		t.Fatal("direct postage claim must be caught")
	}
	if OverClaims("朋友们，西藏是否包邮还没核实，目前能确认的是有包邮活动。") {
		t.Fatal("hedged answer must pass")
	}
}

func TestWrittenRegionIsHit(t *testing.T) {
	if result := Evaluate("川渝多久能到？", duckFacts, true); result.State != Hit {
		t.Fatalf("state=%s want hit (%+v)", result.State, result)
	}
}

func TestQuantityGapIsPartial(t *testing.T) {
	facts := "F020 - [价格] 4人餐299元；8人餐499元。"
	if result := Evaluate("10人餐多少钱？", facts, true); result.State != Partial {
		t.Fatalf("state=%s want partial (%+v)", result.State, result)
	}
	if result := Evaluate("4人餐多少钱？", facts, true); result.State != Hit {
		t.Fatalf("state=%s want hit (%+v)", result.State, result)
	}
}

func TestNoFactsIsMiss(t *testing.T) {
	if result := Evaluate("能治高血压吗？", "", false); result.State != Miss {
		t.Fatalf("state=%s want miss", result.State)
	}
}

func TestExclusionCountsAsAnswered(t *testing.T) {
	result := Evaluate("新疆包邮吗？", "全国包邮，偏远地区除外。", true)
	if result.State != Hit {
		t.Fatalf("explicit exclusion answers the question: %+v", result)
	}
	result = Evaluate("北京包邮吗？", "全国包邮，除新疆西藏外。", true)
	if result.State != Hit {
		t.Fatalf("nationwide coverage answers Beijing: %+v", result)
	}
}

func TestValidateIntroUsesConfigRules(t *testing.T) {
	if err := ValidateIntro("四川特色美食绸都杨鸭子，卤味熟食，开袋即食，选用八个月土麻鸭制作。"); err != nil {
		t.Fatalf("good intro rejected: %v", err)
	}
	for _, text := range []string{
		"咱这儿专售八个月土麻鸭。",
		"我们卖活鸭，现杀现发。",
		"就是一些吃的。",
	} {
		if ValidateIntro(text) == nil {
			t.Fatalf("bad intro accepted: %q", text)
		}
	}
}

func TestQuestionWithoutValuesIsHit(t *testing.T) {
	if result := Evaluate("发什么快递？", duckFacts, true); result.State != Hit || len(result.Values) != 0 {
		t.Fatalf("plain question must not be blocked: %+v", result)
	}
}

func TestMatchContextAndFilter(t *testing.T) {
	name, ctx := MatchContext("怎么已抢光了")
	if name != "stock_out" {
		t.Fatalf("context=%q want stock_out", name)
	}
	facts := "F011 这款已经抢光了，目前没有库存可下单\nF006 下单后第二天上午发货\nF008 统一走京东物流配送"
	got := FilterFactsForContext(facts, ctx)
	if strings.Contains(got, "发货") || strings.Contains(got, "物流") {
		t.Fatalf("shipping facts should be dropped for a sold-out question: %s", got)
	}
	if !strings.Contains(got, "抢光") {
		t.Fatalf("stock fact lost: %s", got)
	}
}
