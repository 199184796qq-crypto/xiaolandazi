package db

import "testing"

func TestCalculateBeanCharge(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		perUnit  uint64
		unitSize uint64
		minimum  uint64
		maximum  uint64
		units    uint64
		want     uint64
	}{
		{name: "fixed", mode: "fixed", perUnit: 8, unitSize: 1, minimum: 1, units: 999, want: 8},
		{name: "round usage up", mode: "per_unit", perUnit: 2, unitSize: 60, minimum: 2, units: 61, want: 4},
		{name: "minimum", mode: "per_unit", perUnit: 1, unitSize: 100, minimum: 5, units: 1, want: 5},
		{name: "maximum", mode: "per_unit", perUnit: 10, unitSize: 1, minimum: 1, maximum: 25, units: 4, want: 25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calculateBeanCharge(tt.mode, tt.perUnit, tt.unitSize, tt.minimum, tt.maximum, tt.units)
			if err != nil {
				t.Fatalf("calculateBeanCharge() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("calculateBeanCharge() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCalculateBeanChargeRejectsInvalidPricing(t *testing.T) {
	if _, err := calculateBeanCharge("unknown", 1, 1, 1, 0, 1); err == nil {
		t.Fatal("calculateBeanCharge() accepted an unsupported mode")
	}
	if _, err := calculateBeanCharge("fixed", 0, 1, 1, 0, 1); err == nil {
		t.Fatal("calculateBeanCharge() accepted a zero unit price")
	}
}
