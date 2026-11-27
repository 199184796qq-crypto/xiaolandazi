package db

import (
	"context"
	"database/sql"
	"encoding/json"
)

const planFactCardsDDL = `
CREATE TABLE IF NOT EXISTS live_agent_plan_fact_cards (
  tenant_id BIGINT UNSIGNED NOT NULL,
  plan_id BIGINT UNSIGNED NOT NULL,
  fact_revision VARCHAR(64) NOT NULL,
  payload_json JSON NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (tenant_id, plan_id, fact_revision)
) ENGINE=InnoDB`

func (s *Store) ensurePlanFactCards(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, planFactCardsDDL)
	return err
}

func (s *Store) GetPlanFactCard(ctx context.Context, tenant, plan int64, revision string) (map[string]any, bool, error) {
	if err := s.ensurePlanFactCards(ctx); err != nil {
		return nil, false, err
	}
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM live_agent_plan_fact_cards WHERE tenant_id=? AND plan_id=? AND fact_revision=?`, tenant, plan, revision).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var card map[string]any
	if err := json.Unmarshal(raw, &card); err != nil {
		return nil, false, err
	}
	return card, true, nil
}

func (s *Store) PutPlanFactCard(ctx context.Context, tenant, plan int64, revision string, card map[string]any) error {
	if err := s.ensurePlanFactCards(ctx); err != nil {
		return err
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO live_agent_plan_fact_cards (tenant_id, plan_id, fact_revision, payload_json) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE payload_json=VALUES(payload_json), updated_at=CURRENT_TIMESTAMP(3)`, tenant, plan, revision, string(raw))
	return err
}
