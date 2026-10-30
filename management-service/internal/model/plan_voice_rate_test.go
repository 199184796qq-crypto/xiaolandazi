package model

import (
	"math"
	"testing"
)

func TestPlanVoiceRateAcceptedRange(t *testing.T) {
	for cents := 80; cents <= 140; cents++ {
		rate := float64(cents) / 100
		if actual, ok := NormalizePlanVoiceRate(rate); !ok || actual != rate {
			t.Fatal(rate, actual, ok)
		}
	}
	for _, rate := range []float64{0, -1, 0.5, 0.79, 1.41, 1.401, 1.405, 1.001, 1.015, 1.01001, 2, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, ok := NormalizePlanVoiceRate(rate); ok {
			t.Fatal("invalid rate accepted", rate)
		}
	}
	if rate, ok := NormalizePlanVoiceRate(1.0 + 0.01 + 0.01); !ok || rate != 1.02 {
		t.Fatal("floating point noise changed a valid rate", rate, ok)
	}
}
