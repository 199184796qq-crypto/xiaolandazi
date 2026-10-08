package productionbuffer

import "testing"

func TestStockDoesNotTreatUnfinishedTextAsPlayable(t *testing.T) {
	c := New(120_000, false)
	if c.ShouldGenerate(Stock{PendingMS: 150_000, PendingCount: 2}, 0) {
		t.Fatal("unbounded text buildup")
	}
	if !c.ShouldGenerate(Stock{ReadyMS: 30_000}, 0) {
		t.Fatal("failed to refill ready audio")
	}
	if c.ShouldGenerate(Stock{ReadyMS: 30_000, ReservedMS: 50_000}, 0) {
		t.Fatal("parallel reservation ignored")
	}
}
func TestHysteresisWaitsUntilLowWatermark(t *testing.T) {
	c := New(120_000, false)
	if c.ShouldGenerate(Stock{ReadyMS: 120_000}, 0) {
		t.Fatal("high watermark ignored")
	}
	if c.ShouldGenerate(Stock{ReadyMS: 100_000}, 0) {
		t.Fatal("restart above low watermark")
	}
	if !c.ShouldGenerate(Stock{ReadyMS: 50_000}, 0) {
		t.Fatal("did not refill at low watermark")
	}
}
func TestPreviewInventoryShrinksWithoutClippingGeneratedUnits(t *testing.T) {
	c := New(120_000, true)
	before := c.Thresholds(120_000)
	tail := c.Thresholds(8633)
	if before.HighMS >= 120_000 || tail.HighMS != 8633 {
		t.Fatal(before, tail)
	}
	if !c.ShouldGenerate(Stock{}, 8633) {
		t.Fatal("remaining time prevented full final unit generation")
	}
	if c.ShouldGenerate(Stock{ReadyMS: 40_000}, 8633) {
		t.Fatal("unnecessary next unit near preview end")
	}
}
