package anchorcycle

import (
	"math/rand"
	"strings"
	"testing"
)

func priceProducts() []FocusProduct {
	return []FocusProduct{
		{LinkKey: "1", DailyPrice: "87.6", Spec: "900g(一整只)"},
		{LinkKey: "2", DailyPrice: "92.6", Spec: "960g(一整只)"},
		{LinkKey: "3", DailyPrice: "48.8", Spec: "450g(半只)"},
	}
}

func TestCheckPriceDriftCatchesCrossLink(t *testing.T) {
	d := CheckPriceDrift("1号链接日常价92.6元，整只鸭子。", priceProducts())
	if len(d) != 1 {
		t.Fatalf("want 1 drift, got %d: %+v", len(d), d)
	}
	if d[0].Correct != "87.6" || d[0].Spoken != "92.6元" {
		t.Fatalf("wrong drift: %+v", d[0])
	}
}

func TestCheckPriceDriftPassesCorrectPrice(t *testing.T) {
	d := CheckPriceDrift("1号链接日常价87.6元，整只鸭子。", priceProducts())
	if len(d) != 0 {
		t.Fatalf("want 0 drift, got %+v", d)
	}
}

func TestCheckPriceDriftIgnoresUnknownPrice(t *testing.T) {
	// 88 不是任何链接的日常价，可能是活动价/到手价，不误报
	d := CheckPriceDrift("今天1号链接到手88元。", priceProducts())
	if len(d) != 0 {
		t.Fatalf("unknown price should not drift, got %+v", d)
	}
}

func TestApplyPriceCorrectionHardReplace(t *testing.T) {
	text := "1号链接日常价92.6元，整只鸭子。"
	drifts := CheckPriceDrift(text, priceProducts())
	if len(drifts) != 1 {
		t.Fatal("no drift")
	}
	// 固定用硬改路径（rng 恒返回 0）
	got := ApplyPriceCorrection(text, drifts[0], rand.New(rand.NewSource(0)))
	if !strings.Contains(got, "87.6元") || strings.Contains(got, "92.6元") {
		t.Fatalf("hard replace failed: %s", got)
	}
}

func TestApplyPriceCorrectionAppend(t *testing.T) {
	text := "1号链接日常价92.6元，整只鸭子。"
	drifts := CheckPriceDrift(text, priceProducts())
	if len(drifts) != 1 {
		t.Fatal("no drift")
	}
	// 固定走补纠正路径（rng 恒返回 1）
	got := ApplyPriceCorrection(text, drifts[0], rand.New(rand.NewSource(1)))
	if !strings.Contains(got, "92.6元") || !strings.Contains(got, "说岔了") || !strings.Contains(got, "87.6") {
		t.Fatalf("append correction failed: %s", got)
	}
}
