package db

import "testing"

func TestNormalizeLiveRiskConfirmationBySeverity(t *testing.T) {
	if _, err := normalizeLiveRiskConfirmation("P0", "block", "用户确认"); err == nil {
		t.Fatal("semantic P0 block must never be confirmable")
	}
	if got, err := normalizeLiveRiskConfirmation("P0", "pass", ""); err != nil || got == "" {
		t.Fatal("semantic pass must record explicit customer confirmation", got, err)
	}
	if got, err := normalizeLiveRiskConfirmation("P0", "evidence_required", ""); err != nil || got == "" {
		t.Fatal("evidence-routed P0 hit must accept explicit user acknowledgement", got, err)
	}
	if got, err := normalizeLiveRiskConfirmation("P0", "evidence_required", "  老字号资质证明  "); err != nil || got != "老字号资质证明" {
		t.Fatal("semantic evidence was not retained", got, err)
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
