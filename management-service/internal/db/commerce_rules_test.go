package db

import (
	"livecompanion/management/internal/model"
	"testing"
	"time"
)

func TestCommerceMoneyAndSchedule(t *testing.T) {
	r := model.CommerceRewardRule{Mode: "percent", RateBPS: 1000}
	for _, c := range []struct{ paid, want uint64 }{{100, 10}, {101, 10}, {0, 0}, {9999, 999}} {
		if got := CommerceRewardAmount(r, c.paid); got != c.want {
			t.Fatalf("paid %d: got %d", c.paid, got)
		}
	}
	r.Mode = "fixed"
	r.AmountCents = 200
	if CommerceRewardAmount(r, 100) != 100 {
		t.Fatal("fixed reward exceeded actual paid")
	}
	r.CapCents = 30
	if CommerceRewardAmount(r, 100) != 30 {
		t.Fatal("cap")
	}
	r = model.CommerceRewardRule{Mode: "tier", Tiers: []model.CommerceRewardTier{{MinimumPaidCents: 0, RateBPS: 500}, {MinimumPaidCents: 10000, RateBPS: 1000}}}
	if CommerceRewardAmount(r, 12000) != 1200 {
		t.Fatal("tier")
	}
	r.ScheduleMode = "monthly"
	r.ReleaseDay = 15
	r.MonthLag = 1
	paid := time.Date(2026, 10, 31, 23, 30, 0, 0, commerceLocation)
	want := time.Date(2026, 11, 15, 0, 0, 0, 0, commerceLocation)
	if !CommerceRewardAvailableAt(r, paid).Equal(want) {
		t.Fatal("month-end accounting must release next month, Beijing time")
	}
	r.PendingDays = 30
	if !CommerceRewardAvailableAt(r, paid).Equal(time.Date(2026, 12, 15, 0, 0, 0, 0, commerceLocation)) {
		t.Fatal("freeze must defer monthly release")
	}
}
func TestCommercePrecedenceAndOff(t *testing.T) {
	rules := []model.CommerceRewardRule{{Channel: "sales", ScopeType: "default", Mode: "percent"}, {Channel: "sales", ScopeType: "category", ProductType: "time_card", Mode: "fixed"}, {Channel: "sales", ScopeType: "product", ProductType: "time_card", TargetID: 1, Mode: "percent"}, {Channel: "sales", ScopeType: "campaign", TargetID: 2, Mode: "off"}}
	if got := selectCommerceRule(rules, "sales", "time_card", 1, 2); got == nil || got.Mode != "off" {
		t.Fatal("campaign opt-out not honored")
	}
	if got := selectCommerceRule(rules, "sales", "time_card", 1, 0); got == nil || got.ScopeType != "product" {
		t.Fatal("product priority")
	}
}
func TestNewAccountControls(t *testing.T) {
	c, e := normalizeCampaignControls(model.MarketingCampaignControls{Audience: "new_within_days", NewAccountDays: 7, MaxClaims: 99}, "")
	if e != nil || c.MaxClaims != 1 || !c.RequirePhone || c.BenefitKey != "new-account-welcome" {
		t.Fatal("new account one-time safeguard", c, e)
	}
	_, e = normalizeCampaignControls(model.MarketingCampaignControls{Audience: "new_since_start"}, "")
	if e == nil {
		t.Fatal("new-since-start needs start time")
	}
	p := uint64(101)
	if marketingItemPayable(10000, model.MarketingCampaignItem{PricingMode: "fixed", FixedPriceCents: &p, Quantity: 3}) != 101 {
		t.Fatal("fixed price must preserve cents and be per bundle")
	}
	if marketingItemPayable(10000, model.MarketingCampaignItem{PricingMode: "free"}) != 0 {
		t.Fatal("free")
	}
}
