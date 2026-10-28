package db

import (
	"context"
	"encoding/json"

	"livecompanion/management/internal/purposelib"
)

// BuildPlanPurposes regenerates the plan's purpose list from facts, links and
// benefits. It is cheap: pure grouping, no model call.
func (s *Store) BuildPlanPurposes(ctx context.Context, tenantID, planID int64) ([]purposelib.Spec, error) {
	facts, err := s.ListLiveAgentPlanFacts(ctx, tenantID, planID)
	if err != nil {
		return nil, err
	}
	links, _ := s.ListLiveAgentPlanProductLinks(ctx, tenantID, planID)
	benefits, _ := s.ListLiveAgentPlanBenefits(ctx, tenantID, planID)
	categories := make([]string, 0, len(facts))
	keys := make([]string, 0, len(facts))
	for _, fact := range facts {
		categories = append(categories, fact.Category)
		keys = append(keys, fact.Key)
	}
	specs := purposelib.Build(categories, keys, len(links) > 0, len(benefits) > 0)
	if err := s.ReplacePlanPurposes(ctx, tenantID, planID, specs); err != nil {
		return nil, err
	}
	return specs, nil
}

// ReplacePlanPurposes overwrites the stored purpose list for a plan.
func (s *Store) ReplacePlanPurposes(ctx context.Context, tenantID, planID int64, specs []purposelib.Spec) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM live_agent_plan_purposes WHERE tenant_id = ? AND plan_id = ?`, tenantID, planID); err != nil {
		return err
	}
	for _, spec := range specs {
		keywords, _ := json.Marshal(spec.Keywords)
		sources, _ := json.Marshal(spec.Sources)
		inOut := 0
		if spec.InOut {
			inOut = 1
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO live_agent_plan_purposes (tenant_id, plan_id, purpose_key, name, keywords_json, sources_json, in_out)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
			tenantID, planID, spec.Key, spec.Name, string(keywords), string(sources), inOut); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListPlanPurposes reads the stored purpose list.
func (s *Store) ListPlanPurposes(ctx context.Context, tenantID, planID int64) ([]purposelib.Spec, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT purpose_key, name, keywords_json, sources_json, in_out
FROM live_agent_plan_purposes WHERE tenant_id = ? AND plan_id = ? ORDER BY purpose_key`, tenantID, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	specs := make([]purposelib.Spec, 0, 16)
	for rows.Next() {
		var spec purposelib.Spec
		var keywords, sources string
		var inOut int
		if err := rows.Scan(&spec.Key, &spec.Name, &keywords, &sources, &inOut); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(keywords), &spec.Keywords)
		_ = json.Unmarshal([]byte(sources), &spec.Sources)
		spec.InOut = inOut == 1
		specs = append(specs, spec)
	}
	return specs, rows.Err()
}
