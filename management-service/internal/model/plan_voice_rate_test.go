package model

import (
	"math"
	"testing"
)

func TestPlanVoiceRateAcceptedRange(t *testing.T) {
	for _, rate := range []float64{0.8, 0.9, 1, 1.1, 1.2} {
		if actual, ok := NormalizePlanVoiceRate(rate); !ok || actual != rate {
			t.Fatal(rate, actual, ok)
		}
	}
	for _, rate := range []float64{0, -1, 0.5, 1.15, 2, math.NaN(), math.Inf(1)} {
		if _, ok := NormalizePlanVoiceRate(rate); ok {
			t.Fatal("invalid rate accepted", rate)
		}
	}
}
