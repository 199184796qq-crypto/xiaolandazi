package numericrepair

import (
	"strings"
	"testing"
)

func TestBuildAtomicUnit(t *testing.T) {
	unit, err := BuildAtomicUnit(Calculation{
		Operation: "multiply", LeftMinor: 699, Right: 2,
		CorrectMinor: 1398, Scale: 10, Unit: "元",
	}, "等一下，刚才算岔了")
	if err != nil {
		t.Fatal(err)
	}
	if unit.Interruptible || !unit.AtomicAudio || !unit.AtomicSubtitle {
		t.Fatalf("unit is not atomic: %+v", unit)
	}
	if unit.WrongMinor == unit.CorrectMinor || !strings.Contains(unit.Text, "139.8元") {
		t.Fatalf("unexpected repair unit: %+v", unit)
	}
}

func TestBuildAtomicUnitRejectsUnverifiedCorrectValue(t *testing.T) {
	_, err := BuildAtomicUnit(Calculation{
		Operation: "multiply", LeftMinor: 699, Right: 2,
		CorrectMinor: 1408, Scale: 10, Unit: "元",
	}, "")
	if err == nil {
		t.Fatal("expected mismatched correct value to fail")
	}
}
