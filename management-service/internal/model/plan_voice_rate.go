package model

import "math"

type PlanVoiceRate struct {
	TenantID int64   `json:"tenant_id"`
	PlanID   int64   `json:"plan_id"`
	Rate     float64 `json:"rate"`
}

func NormalizePlanVoiceRate(rate float64) (float64, bool) {
	if math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, false
	}
	normalized := math.Round(rate*100) / 100
	if normalized < 0.8 || normalized > 1.4 || math.Abs(rate-normalized) > 1e-9 {
		return 0, false
	}
	return normalized, true
}
