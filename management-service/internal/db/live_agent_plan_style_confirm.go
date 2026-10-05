package db

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

func (s *Store) ConfirmLiveAgentPlanScriptAnalysis(ctx context.Context, tenantID, planID, scriptID int64, source string, analysis model.LiveAgentPlanScriptAnalysis) (model.LiveAgentPlanScript, error) {
	raw, err := json.Marshal(analysis)
	if err != nil {
		return model.LiveAgentPlanScript{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE live_agent_plan_scripts
		SET analysis_status='analyzed', analysis_json=?, analyzed_at=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'
		AND BINARY TRIM(readable_text)=BINARY ?`, string(raw), time.Now().UTC(), scriptID, tenantID, planID, strings.TrimSpace(source))
	if err != nil {
		return model.LiveAgentPlanScript{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return model.LiveAgentPlanScript{}, err
	}
	if count == 0 {
		return model.LiveAgentPlanScript{}, ErrLiveAgentPlanScriptNotFound
	}
	return s.GetLiveAgentPlanScript(ctx, tenantID, planID, scriptID)
}
