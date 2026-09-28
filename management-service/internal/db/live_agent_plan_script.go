package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

var ErrLiveAgentPlanScriptNotFound = errors.New("live agent plan script not found")

func emptyLiveAgentPlanScriptAnalysis() model.LiveAgentPlanScriptAnalysis {
	return model.LiveAgentPlanScriptAnalysis{
		ProductLinks: []model.LiveAgentPlanProductLinkCandidate{},
		Facts:        []model.LiveAgentPlanFactCandidate{},
		RhythmNodes:  []model.LiveAgentPlanRhythmNode{},
		AnchorStyle: model.LiveAgentPlanAnchorStyleProfile{
			Dimensions:        []model.LiveAgentPlanAnchorStyleDimension{},
			ReusableRules:     []string{},
			CandidatePatterns: []string{},
			ExcludedFromStyle: []string{},
		},
	}
}

func decodeLiveAgentPlanScriptAnalysis(raw string) model.LiveAgentPlanScriptAnalysis {
	analysis := emptyLiveAgentPlanScriptAnalysis()
	if strings.TrimSpace(raw) == "" {
		return analysis
	}
	if err := json.Unmarshal([]byte(raw), &analysis); err != nil {
		return emptyLiveAgentPlanScriptAnalysis()
	}
	if analysis.Facts == nil {
		analysis.Facts = []model.LiveAgentPlanFactCandidate{}
	}
	if analysis.ProductLinks == nil {
		analysis.ProductLinks = []model.LiveAgentPlanProductLinkCandidate{}
	}
	if analysis.RhythmNodes == nil {
		analysis.RhythmNodes = []model.LiveAgentPlanRhythmNode{}
	}
	if analysis.AnchorStyle.Dimensions == nil {
		analysis.AnchorStyle.Dimensions = []model.LiveAgentPlanAnchorStyleDimension{}
	}
	if analysis.AnchorStyle.ReusableRules == nil {
		analysis.AnchorStyle.ReusableRules = []string{}
	}
	if analysis.AnchorStyle.CandidatePatterns == nil {
		analysis.AnchorStyle.CandidatePatterns = []string{}
	}
	if analysis.AnchorStyle.ExcludedFromStyle == nil {
		analysis.AnchorStyle.ExcludedFromStyle = []string{}
	}
	return analysis
}

func scanLiveAgentPlanScript(row interface {
	Scan(dest ...any) error
}) (model.LiveAgentPlanScript, error) {
	var (
		item        model.LiveAgentPlanScript
		sourceAsset sql.NullInt64
		analysisRaw string
		analyzedAt  sql.NullTime
		createdBy   sql.NullInt64
		updatedBy   sql.NullInt64
	)
	err := row.Scan(
		&item.ID, &item.TenantID, &item.PlanID, &item.Title, &item.SourceType,
		&sourceAsset, &item.OriginalName, &item.RawText, &item.ReadableText,
		&item.AnalysisStatus, &analysisRaw, &item.ModelProvider, &item.ModelName,
		&item.LatencyMS, &analyzedAt, &item.Status, &createdBy, &updatedBy,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.LiveAgentPlanScript{}, err
	}
	if sourceAsset.Valid {
		value := sourceAsset.Int64
		item.SourceAssetID = &value
	}
	if analyzedAt.Valid {
		value := analyzedAt.Time
		item.AnalyzedAt = &value
	}
	if createdBy.Valid {
		value := createdBy.Int64
		item.CreatedByUserID = &value
	}
	if updatedBy.Valid {
		value := updatedBy.Int64
		item.UpdatedByUserID = &value
	}
	item.Analysis = decodeLiveAgentPlanScriptAnalysis(analysisRaw)
	return item, nil
}

const liveAgentPlanScriptSelect = `
	SELECT id, tenant_id, plan_id, title, source_type, source_asset_id,
		original_name, raw_text, readable_text, analysis_status, analysis_json,
		model_provider, model_name, latency_ms, analyzed_at, status,
		created_by_user_id, updated_by_user_id, created_at, updated_at
	FROM live_agent_plan_scripts
`

func (s *Store) ListLiveAgentPlanScripts(ctx context.Context, tenantID, planID int64) ([]model.LiveAgentPlanScript, error) {
	rows, err := s.db.QueryContext(ctx, liveAgentPlanScriptSelect+`
		WHERE tenant_id=? AND plan_id=? AND status='active'
		ORDER BY updated_at DESC, id DESC
	`, tenantID, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveAgentPlanScript, 0)
	for rows.Next() {
		item, err := scanLiveAgentPlanScript(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetLiveAgentPlanScript(ctx context.Context, tenantID, planID, scriptID int64) (model.LiveAgentPlanScript, error) {
	item, err := scanLiveAgentPlanScript(s.db.QueryRowContext(ctx, liveAgentPlanScriptSelect+`
		WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'
	`, scriptID, tenantID, planID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanScript{}, ErrLiveAgentPlanScriptNotFound
	}
	return item, err
}

func (s *Store) SaveLiveAgentPlanScript(
	ctx context.Context,
	tenantID, planID, actorUserID int64,
	input model.SaveLiveAgentPlanScriptInput,
) (model.LiveAgentPlanScript, error) {
	var planExists int
	if err := s.db.QueryRowContext(ctx, `
		SELECT 1 FROM live_agent_plans WHERE id=? AND tenant_id=? AND status='active'
	`, planID, tenantID).Scan(&planExists); errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanScript{}, ErrLiveAgentPlanNotFound
	} else if err != nil {
		return model.LiveAgentPlanScript{}, err
	}
	analysisRaw, _ := json.Marshal(emptyLiveAgentPlanScriptAnalysis())
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO live_agent_plan_scripts (
			tenant_id, plan_id, title, source_type, source_asset_id, original_name,
			raw_text, readable_text, analysis_status, analysis_json, status,
			created_by_user_id, updated_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'not_analyzed', ?, 'active', ?, ?)
	`, tenantID, planID, strings.TrimSpace(input.Title), strings.TrimSpace(input.SourceType),
		input.SourceAssetID, strings.TrimSpace(input.OriginalName), input.RawText, input.ReadableText,
		string(analysisRaw), actorUserID, actorUserID)
	if err != nil {
		return model.LiveAgentPlanScript{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.LiveAgentPlanScript{}, err
	}
	return s.GetLiveAgentPlanScript(ctx, tenantID, planID, id)
}

func (s *Store) UpdateLiveAgentPlanScript(
	ctx context.Context,
	tenantID, planID, scriptID, actorUserID int64,
	input model.SaveLiveAgentPlanScriptInput,
) (model.LiveAgentPlanScript, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE live_agent_plan_scripts
		SET title=?, readable_text=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'
	`, strings.TrimSpace(input.Title), input.ReadableText, actorUserID, scriptID, tenantID, planID)
	if err != nil {
		return model.LiveAgentPlanScript{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return model.LiveAgentPlanScript{}, ErrLiveAgentPlanScriptNotFound
	}
	return s.GetLiveAgentPlanScript(ctx, tenantID, planID, scriptID)
}

func (s *Store) SetLiveAgentPlanScriptAnalysis(
	ctx context.Context,
	tenantID, planID, scriptID int64,
	analysis model.LiveAgentPlanScriptAnalysis,
	provider, modelName string,
	latencyMS int64,
) (model.LiveAgentPlanScript, error) {
	raw, err := json.Marshal(analysis)
	if err != nil {
		return model.LiveAgentPlanScript{}, err
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		UPDATE live_agent_plan_scripts
		SET analysis_status='analyzed', analysis_json=?, model_provider=?, model_name=?,
			latency_ms=?, analyzed_at=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'
	`, string(raw), provider, modelName, latencyMS, now, scriptID, tenantID, planID)
	if err != nil {
		return model.LiveAgentPlanScript{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return model.LiveAgentPlanScript{}, ErrLiveAgentPlanScriptNotFound
	}
	return s.GetLiveAgentPlanScript(ctx, tenantID, planID, scriptID)
}
