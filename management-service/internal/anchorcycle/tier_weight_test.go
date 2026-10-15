package anchorcycle

import "testing"

func TestTierWeight(t *testing.T) {
	cases := map[string]int{"core": 3, "secondary": 2, "trust": 1, "": 1, "unknown": 1}
	for tier, want := range cases {
		if got := tierWeight(tier); got != want {
			t.Fatalf("tierWeight(%q)=%d want %d", tier, got, want)
		}
	}
}

func TestPointWeightFallsBackToTier(t *testing.T) {
	s := &State{} // 无 ProductFocus，也无 PointWeights 配置
	if got := s.pointWeight(Task{SellingPoint: "核心工艺", Tier: "core"}); got != 3 {
		t.Fatalf("core 卖点应权重 3，got %d", got)
	}
	if got := s.pointWeight(Task{SellingPoint: "原料", Tier: "secondary"}); got != 2 {
		t.Fatalf("secondary 卖点应权重 2，got %d", got)
	}
	if got := s.pointWeight(Task{SellingPoint: "物流", Tier: "trust"}); got != 1 {
		t.Fatalf("trust 卖点应权重 1，got %d", got)
	}
}

func TestPointWeightConfiguredOverridesTier(t *testing.T) {
	s := &State{ProductFocus: &ProductFocus{Policy: ProductPolicy{PointWeights: map[string]int{"核心工艺": 5}}}}
	if got := s.pointWeight(Task{SellingPoint: "核心工艺", Tier: "core"}); got != 5 {
		t.Fatalf("配置的 PointWeights 应覆盖 tier，got %d", got)
	}
	// 未配置的卖点仍走 tier 默认。
	if got := s.pointWeight(Task{SellingPoint: "物流", Tier: "trust"}); got != 1 {
		t.Fatalf("未配置卖点应走 tier 权重，got %d", got)
	}
}
