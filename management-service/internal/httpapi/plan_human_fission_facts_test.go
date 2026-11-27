package httpapi

import (
	"encoding/json"
	"testing"

	"livecompanion/management/internal/humanfission"
)

func TestHumanFissionScansPriceAndCouponDatesInContext(t *testing.T) {
	const price = "9块9三斤，各位老板们，这是我们今天直播间才有的价格。"
	const comparison = "你看门店价格要一斤五块。"
	const deadline = "这个券11月29号才截止，可以用到月底，29号才结束。"
	const term = "2号链接的券购买后十天内可以使用。"
	lines := map[int][]humanfission.Cue{
		1: {{Text: "看看我们直播间今天什么价格？"}, {Text: price}},
		2: {{Text: "后面再想吃这个猕猴桃。"}, {Text: comparison}},
		3: {{Text: "我们的一号链接这个猕猴桃。"}, {Text: deadline}},
		4: {{Text: term}, {Text: "2号链接团购价十九元九角，每份五斤。"}},
	}
	facts := json.RawMessage(`[
		{"对象":"1号链接","属性":"商品信息·品名","值":"徐香猕猴桃"},
		{"对象":"1号链接","属性":"价格","值":"12.9 元 3 斤"},
		{"对象":"1号链接","属性":"竞品价格·门店价格","值":"一斤五块"},
		{"对象":"1号链接","属性":"卡券信息·有效期","值":"至11月27日"},
		{"对象":"2号链接","属性":"价格·售价","值":"19.90 元 5 斤"},
		{"对象":"2号链接","属性":"券·有效期","值":"购买后十二天"}
	]`)
	hits, err := fissionNumericConflicts([]int{1, 2, 3, 4}, lines, facts)
	if err != nil || len(hits) != 3 {
		t.Fatalf("scan = %+v, %v", hits, err)
	}
	for i, before := range []string{price, deadline, term} {
		if hits[i].Before != before || hits[i].Part != []int{1, 3, 4}[i] {
			t.Fatalf("conflict %d = %+v", i, hits[i])
		}
	}
	for i, fix := range []string{
		"12块9三斤，各位老板们，这是我们今天直播间才有的价格。",
		"这个券11月27号截止，可以用到27号。",
		"2号链接的券购买后十二天内可以使用。",
	} {
		if !hits[i].resolved(fix) || hits[i].resolved(hits[i].Before) || hits[i].resolved("") {
			t.Fatalf("unexpected verification for conflict %+v", hits[i])
		}
	}
	for _, stale := range []string{
		"这个券11月27号截止，可以用到月底。",
		"这个券11月27号截止，29号才结束。",
	} {
		if hits[1].resolved(stale) {
			t.Fatalf("accepted stale cutoff in %q", stale)
		}
	}
}

func TestHumanFissionNumericScanLeavesAmbiguousPricesToModel(t *testing.T) {
	const price = "今天直播间售价九十九元。"
	lines := map[int][]humanfission.Cue{
		1: {{Text: "我们的1号链接。"}},
		2: {{Text: price}},
		3: {{Text: "我们的2号链接。"}},
		4: {{Text: "1号链接大约九块多。"}, {Text: "1号链接线下价格十八元。"}, {Text: "1号链接价格大概在5–6元。"}},
		5: {{Text: "3号链接团购价九块九。"}},
	}
	facts := json.RawMessage(`[
		{"对象":"1号链接","属性":"团购价","值":"109 元"},
		{"对象":"2号链接","属性":"价格","值":"99 元"},
		{"对象":"3号链接","属性":"团购价","值":"12.9 元","条件":"周末"}
	]`)
	for _, order := range [][]int{{1, 2, 3, 4, 5}, {1, 2, 1, 3, 2}} {
		hits, err := fissionNumericConflicts(order, lines, facts)
		if err != nil || len(hits) != 0 {
			t.Fatalf("ambiguous order %v produced %+v, %v", order, hits, err)
		}
	}
	hits, err := fissionNumericConflicts([]int{1, 2, 1, 2}, lines, facts)
	if err != nil || len(hits) != 1 || hits[0].Before != price || !hits[0].resolved("今天直播间售价109元。") {
		t.Fatalf("unambiguous repeated clip produced %+v, %v", hits, err)
	}
}

func TestHumanFissionPriceValues(t *testing.T) {
	for _, tt := range []struct {
		text string
		want int
	}{
		{"９．９０ 元 ３ 斤", 990}, {"9块9三斤", 990}, {"九元九角", 990}, {"九点九块", 990},
		{"十九元九角五斤", 1990}, {"12块3盒", 1200}, {"9元3斤", 900}, {"18.26元", 1826}, {"一百二十五元", 12500},
	} {
		got, _, ok := fissionNumericValue(fissionPrice, tt.text)
		if !ok || got != tt.want {
			t.Errorf("%q = %d, %v, want %d", tt.text, got, ok, tt.want)
		}
	}
	for _, text := range []string{"差价6-8元", "九块多", "十七八元", "18.233元", "一千零八十元", "一百五元"} {
		if _, _, ok := fissionNumericValue(fissionPrice, text); ok {
			t.Errorf("%q is not an exact price", text)
		}
	}
}
