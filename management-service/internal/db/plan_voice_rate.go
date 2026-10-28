package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"livecompanion/management/internal/model"
)

var ErrInvalidPlanVoiceRate = errors.New("语速须为 0.80～1.20，步长为 0.01")

func (s *Store) migratePlanVoiceRate(ctx context.Context) error {
	exists, err := s.columnExists(ctx, "live_agent_plans", "voice_rate")
	if err != nil || exists {
		return err
	}
	// NULL preserves the distinction between a legacy version's saved rate
	// and an explicitly saved plan preference. A new plan resolves to 1.
	_, err = s.db.ExecContext(ctx, `ALTER TABLE live_agent_plans ADD COLUMN voice_rate DECIMAL(3,2) NULL DEFAULT NULL`)
	return err
}

func (s *Store) GetPlanVoiceRate(ctx context.Context, tenant, plan int64) (model.PlanVoiceRate, error) {
	result := model.PlanVoiceRate{TenantID: tenant, PlanID: plan, Rate: 1}
	var rate sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `SELECT voice_rate FROM live_agent_plans WHERE tenant_id=? AND id=? AND status='active'`, tenant, plan).Scan(&rate)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrLiveAgentPlanNotFound
	}
	if err != nil {
		return result, err
	}
	if rate.Valid {
		if normalized, ok := model.NormalizePlanVoiceRate(rate.Float64); ok {
			result.Rate = normalized
		}
		return result, nil
	}
	var raw string
	err = s.db.QueryRowContext(ctx, `SELECT CAST(voice_identity_json AS CHAR) FROM live_agent_plan_versions WHERE tenant_id=? AND plan_id=? AND lifecycle_status NOT IN ('prepared','abandoned') ORDER BY id DESC LIMIT 1`, tenant, plan).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Rate = planVoiceRateFromJSON(raw)
	return result, nil
}

func planVoiceRateFromJSON(raw string) float64 {
	var identity model.LiveAgentVoiceIdentity
	if json.Unmarshal([]byte(raw), &identity) == nil {
		if rate, valid := model.NormalizePlanVoiceRate(identity.Rate); valid {
			return rate
		}
	}
	return 1
}

func (s *Store) SetPlanVoiceRate(ctx context.Context, tenant, plan int64, rate float64) (model.PlanVoiceRate, error) {
	normalized, valid := model.NormalizePlanVoiceRate(rate)
	if !valid {
		return model.PlanVoiceRate{}, ErrInvalidPlanVoiceRate
	}
	_, err := s.db.ExecContext(ctx, `UPDATE live_agent_plans SET voice_rate=?,updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND id=? AND status='active'`, normalized, tenant, plan)
	if err != nil {
		return model.PlanVoiceRate{}, err
	}
	return s.GetPlanVoiceRate(ctx, tenant, plan)
}
