package anchorcycle

import (
	"strings"
	"testing"
)

func TestLimitCommonFactsKeepsAtMostTwo(t *testing.T) {
	s := &State{Queue: []Task{{Key: "K", SellingPoint: "性价比", Angle: "对比大牌"}}, RunID: "run1", AcceptedOffset: 0}
	facts := []HookFact{
		{ID: "c1", Data: "- [用户指定公共；brand/品牌] 39年老油坊", Shared: true},
		{ID: "c2", Data: "- [用户指定公共；origin/产地] 四川绵阳河清镇", Shared: true},
		{ID: "c3", Data: "- [用户指定公共；location/停车] 免费停车", Shared: true},
		{ID: "c4", Data: "- [用户指定公共；stores/门店] 五十多家门店", Shared: true},
		{ID: "p1", Data: "- [product/规格] 5L装", Products: []string{"1"}},
	}
	out := s.limitCommonFacts(facts)
	common := 0
	for _, f := range out {
		if f.Shared || isCommonFactText(f.Data) {
			common++
		}
	}
	if common != 2 {
		t.Fatalf("公共事实应限量到2条，got=%d (%v)", common, out)
	}
	// 非公共事实必须原样保留
	kept := false
	for _, f := range out {
		if f.ID == "p1" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("商品专属事实不应被限量丢掉")
	}
}

func TestLimitCommonFactsPrefersRelevant(t *testing.T) {
	s := &State{Queue: []Task{{Key: "K", SellingPoint: "性价比", Angle: ""}}, RunID: "run1"}
	facts := []HookFact{
		{ID: "c1", Data: "- [用户指定公共；brand/品牌] 39年老油坊", Shared: true},
		{ID: "c2", Data: "- [用户指定公共；price/价格] 性价比高，一斤合十四块", Shared: true},
		{ID: "c3", Data: "- [用户指定公共；location/停车] 免费停车", Shared: true},
		{ID: "c4", Data: "- [用户指定公共；stores/门店] 五十多家门店", Shared: true},
	}
	out := s.limitCommonFacts(facts)
	// 含「性价比」的 c2 必须被优先保留
	found := false
	for _, f := range out {
		if f.ID == "c2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("含卖点关键词的公共事实应优先保留，got=%v", out)
	}
}

func TestLimitCommonFactsVariesAcrossSegments(t *testing.T) {
	base := []HookFact{
		{ID: "c1", Data: "- [用户指定公共；brand/品牌] 39年老油坊", Shared: true},
		{ID: "c2", Data: "- [用户指定公共；origin/产地] 四川绵阳河清镇", Shared: true},
		{ID: "c3", Data: "- [用户指定公共；location/停车] 免费停车", Shared: true},
		{ID: "c4", Data: "- [用户指定公共；stores/门店] 五十多家门店", Shared: true},
		{ID: "c5", Data: "- [用户指定公共；service/售后] 支持七天退换", Shared: true},
	}
	// 同一个任务，不同已采纳段数，选出的公共事实应不同
	seen := map[string]bool{}
	for n := 0; n < 8; n++ {
		s := &State{Queue: []Task{{Key: "K", SellingPoint: "性价比", Angle: ""}}, RunID: "run1", AcceptedOffset: n}
		out := s.limitCommonFacts(append([]HookFact{}, base...))
		var keys []string
		for _, f := range out {
			if f.Shared {
				keys = append(keys, f.ID)
			}
		}
		seen[strings.Join(keys, ",")] = true
	}
	if len(seen) < 2 {
		t.Fatalf("不同段应选出不同的公共事实组合，实际只出现 %d 种", len(seen))
	}
}

func TestLimitCommonFactsNoOpWhenFew(t *testing.T) {
	s := &State{Queue: []Task{{Key: "K", SellingPoint: "性价比", Angle: ""}}}
	facts := []HookFact{
		{ID: "c1", Data: "- [用户指定公共；brand/品牌] 39年老油坊", Shared: true},
		{ID: "c2", Data: "- [用户指定公共；origin/产地] 四川绵阳河清镇", Shared: true},
		{ID: "p1", Data: "- [product/规格] 5L装", Products: []string{"1"}},
	}
	out := s.limitCommonFacts(facts)
	if len(out) != len(facts) {
		t.Fatalf("公共事实≤2条时不应改变，got=%d want=%d", len(out), len(facts))
	}
}
