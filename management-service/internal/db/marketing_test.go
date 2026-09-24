package db

import "testing"

func TestCalculateMarketingPayableCentsFloorsToWholeYuan(t *testing.T) {
	tests := []struct {
		name        string
		baseCents   uint64
		discountBPS uint32
		wantCents   uint64
	}{
		{name: "899 yuan at 9 zhe", baseCents: 89900, discountBPS: 9000, wantCents: 80900},
		{name: "2697 yuan at 8 zhe", baseCents: 269700, discountBPS: 8000, wantCents: 215700},
		{name: "5394 yuan at 7 zhe", baseCents: 539400, discountBPS: 7000, wantCents: 377500},
		{name: "10788 yuan at 6 zhe", baseCents: 1078800, discountBPS: 6000, wantCents: 647200},
		{name: "598 yuan at 9 zhe", baseCents: 59800, discountBPS: 9000, wantCents: 53800},
		{name: "88 yuan at 9 zhe", baseCents: 8800, discountBPS: 9000, wantCents: 7900},
		{name: "base cents are discarded before discount", baseCents: 199, discountBPS: 6000, wantCents: 0},
		{name: "zero discount is gift", baseCents: 89900, discountBPS: 0, wantCents: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := calculateMarketingPayableCents(test.baseCents, test.discountBPS)
			if got != test.wantCents {
				t.Fatalf("calculateMarketingPayableCents(%d, %d) = %d, want %d",
					test.baseCents, test.discountBPS, got, test.wantCents)
			}
			if got%100 != 0 {
				t.Fatalf("marketing payable amount must be whole yuan, got %d cents", got)
			}
		})
	}
}

func TestNormalizeMarketingDisplayLocations(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "empty is backoffice", in: nil, want: []string{"backoffice"}},
		{name: "shop only", in: []string{"shop"}, want: []string{"shop"}},
		{name: "membership only", in: []string{"membership"}, want: []string{"membership"}},
		{name: "multiple client placements", in: []string{"shop", "membership"}, want: []string{"shop", "membership"}},
		{name: "backoffice is exclusive", in: []string{"backoffice", "shop"}, want: []string{"shop"}},
		{name: "dedupe and normalize", in: []string{" SHOP ", "shop", "membership"}, want: []string{"shop", "membership"}},
		{name: "future placement code is allowed", in: []string{"live_room"}, want: []string{"live_room"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := normalizeMarketingDisplayLocations(test.in)
			if len(got) != len(test.want) {
				t.Fatalf("normalizeMarketingDisplayLocations(%v) = %v, want %v", test.in, got, test.want)
			}
			for i := range test.want {
				if got[i] != test.want[i] {
					t.Fatalf("normalizeMarketingDisplayLocations(%v) = %v, want %v", test.in, got, test.want)
				}
			}
		})
	}
}

func TestNormalizeCustomerMarketingPlacement(t *testing.T) {
	if got := normalizeCustomerMarketingPlacement("membership"); got != "membership" {
		t.Fatalf("membership placement = %q, want membership", got)
	}
	for _, value := range []string{"", "shop", "backoffice", "unknown"} {
		if got := normalizeCustomerMarketingPlacement(value); got != "shop" {
			t.Fatalf("normalizeCustomerMarketingPlacement(%q) = %q, want shop", value, got)
		}
	}
}
