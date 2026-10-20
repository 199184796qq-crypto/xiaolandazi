package model

import "math"

type PlanVoiceRate struct {
	TenantID int64   `json:"tenant_id"`
	PlanID   int64   `json:"plan_id"`
	Rate     float64 `json:"rate"`
}

func NormalizePlanVoiceRate(rate float64) (float64, bool) {
	for _, allowed := range []float64{0.8, 0.9, 1, 1.1, 1.2} {
		if math.Abs(rate-allowed) < 0.0001 {
			return allowed, true
		}
	}
	return 0, false
}
