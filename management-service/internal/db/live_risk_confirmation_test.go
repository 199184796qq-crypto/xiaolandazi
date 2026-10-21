package db

import "testing"

func TestNormalizeLiveRiskConfirmationBySeverity(t *testing.T) {
	// 语义复核已下线：semantic 类 P0 与 P1/P2 一样，用户确认/举证即放行。
	if got, err := normalizeLiveRiskConfirmation("P0", "not_required", ""); err != nil || got == "" {
		t.Fatal("semantic P0 must accept explicit user acknowledgement", got, err)
	}
	if got, err := normalizeLiveRiskConfirmation("P0", "not_required", "  老字号资质证明  "); err != nil || got != "老字号资质证明" {
		t.Fatal("P0 evidence was not retained", got, err)
	}
	// hard 类 P0 仍不能由用户直接确认放行，只能后台复核。
	if _, err := normalizeLiveRiskConfirmation("P0", "hard", "用户确认"); err == nil {
		t.Fatal("hard P0 must never be confirmable by the user")
	}
	if got, err := normalizeLiveRiskConfirmation("P1", "not_applicable", ""); err != nil || got == "" {
		t.Fatal("P1 must accept explicit user acknowledgement", got, err)
	}
	if got, err := normalizeLiveRiskConfirmation("P1", "not_applicable", "  批次检测报告  "); err != nil || got != "批次检测报告" {
		t.Fatal("P1 evidence was not retained", got, err)
	}
	if got, err := normalizeLiveRiskConfirmation("P2", "not_applicable", ""); err != nil || got != "用户已确认该项P2表达可以使用" {
		t.Fatal("P2 explicit confirmation was not recorded", got, err)
	}
	if _, err := normalizeLiveRiskConfirmation("P3", "not_applicable", "确认"); err == nil {
		t.Fatal("unknown severity must fail closed")
	}
}
