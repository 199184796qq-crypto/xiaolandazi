package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrLiveAnchorStyleNotFound = errors.New("live anchor style not found")

func emptyLiveAnchorStyleProfile() model.LiveAgentPlanAnchorStyleProfile {
	return model.LiveAgentPlanAnchorStyleProfile{
		Dimensions:        []model.LiveAgentPlanAnchorStyleDimension{},
		ReusableRules:     []string{},
		CandidatePatterns: []string{},
		ExcludedFromStyle: []string{},
	}
}

func decodeLiveAnchorStyleProfile(raw string) model.LiveAgentPlanAnchorStyleProfile {
	profile := emptyLiveAnchorStyleProfile()
	if strings.TrimSpace(raw) == "" {
		return profile
	}
	if err := json.Unmarshal([]byte(raw), &profile); err != nil {
		return emptyLiveAnchorStyleProfile()
	}
	if profile.Dimensions == nil {
		profile.Dimensions = []model.LiveAgentPlanAnchorStyleDimension{}
	}
	if profile.ReusableRules == nil {
		profile.ReusableRules = []string{}
	}
	if profile.CandidatePatterns == nil {
		profile.CandidatePatterns = []string{}
	}
	if profile.ExcludedFromStyle == nil {
		profile.ExcludedFromStyle = []string{}
	}
	return profile
}

func encodeJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func scanLiveAnchorStyle(row interface{ Scan(...any) error }) (model.LiveAnchorStyle, error) {
	var item model.LiveAnchorStyle
	var profileRaw string
	var createdBy, updatedBy sql.NullInt64
	if err := row.Scan(&item.ID, &item.TenantID, &item.Name, &item.Description, &item.Status, &profileRaw, &item.SampleCount, &item.TrainingCount, &item.BoundPlanCount, &createdBy, &updatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return item, err
	}
	item.Profile = decodeLiveAnchorStyleProfile(profileRaw)
	if createdBy.Valid {
		v := createdBy.Int64
		item.CreatedByUserID = &v
	}
	if updatedBy.Valid {
		v := updatedBy.Int64
		item.UpdatedByUserID = &v
	}
	return item, nil
}

const liveAnchorStyleSelect = `
	SELECT s.id, s.tenant_id, s.name, s.description, s.status, s.profile_json,
		(SELECT COUNT(*) FROM live_anchor_style_samples sm WHERE sm.style_id=s.id) AS sample_count,
		(SELECT COUNT(*) FROM live_anchor_style_trainings tr WHERE tr.style_id=s.id) AS training_count,
		(SELECT COUNT(*) FROM live_anchor_style_plan_bindings b WHERE b.style_id=s.id) AS bound_plan_count,
		s.created_by_user_id, s.updated_by_user_id, s.created_at, s.updated_at
	FROM live_anchor_styles s
`

func (s *Store) CreateLiveAnchorStyle(ctx context.Context, tenantID, actorID int64, input model.CreateLiveAnchorStyleInput) (model.LiveAnchorStyle, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "未命名主播"
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO live_anchor_styles (tenant_id, name, description, profile_json, created_by_user_id, updated_by_user_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, tenantID, name, strings.TrimSpace(input.Description), encodeJSON(emptyLiveAnchorStyleProfile()), actorID, actorID)
	if err != nil {
		return model.LiveAnchorStyle{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.LiveAnchorStyle{}, err
	}
	return s.GetLiveAnchorStyle(ctx, tenantID, id)
}

func (s *Store) ListLiveAnchorStyles(ctx context.Context, tenantID int64) ([]model.LiveAnchorStyle, error) {
	rows, err := s.db.QueryContext(ctx, liveAnchorStyleSelect+` WHERE s.tenant_id=? AND s.status='active' ORDER BY s.updated_at DESC, s.id DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveAnchorStyle, 0)
	for rows.Next() {
		item, err := scanLiveAnchorStyle(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetLiveAnchorStyle(ctx context.Context, tenantID, styleID int64) (model.LiveAnchorStyle, error) {
	item, err := scanLiveAnchorStyle(s.db.QueryRowContext(ctx, liveAnchorStyleSelect+` WHERE s.id=? AND s.tenant_id=? AND s.status='active'`, styleID, tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAnchorStyle{}, ErrLiveAnchorStyleNotFound
	}
	if err != nil {
		return model.LiveAnchorStyle{}, err
	}
	plugins, err := s.ListLiveAnchorStylePlugins(ctx, tenantID, styleID)
	if err != nil {
		return model.LiveAnchorStyle{}, err
	}
	item.PluginSettings = plugins
	return item, nil
}

func (s *Store) UpdateLiveAnchorStyle(ctx context.Context, tenantID, styleID, actorID int64, input model.UpdateLiveAnchorStyleInput) (model.LiveAnchorStyle, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return model.LiveAnchorStyle{}, errors.New("主播名称不能为空")
	}
	args := []any{name, strings.TrimSpace(input.Description), actorID, styleID, tenantID}
	query := `UPDATE live_anchor_styles SET name=?, description=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)`
	if input.Profile != nil {
		query += `, profile_json=?`
		args = []any{name, strings.TrimSpace(input.Description), actorID, encodeJSON(*input.Profile), styleID, tenantID}
	}
	query += ` WHERE id=? AND tenant_id=? AND status='active'`
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return model.LiveAnchorStyle{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return model.LiveAnchorStyle{}, ErrLiveAnchorStyleNotFound
	}
	return s.GetLiveAnchorStyle(ctx, tenantID, styleID)
}

func (s *Store) DeleteLiveAnchorStyle(ctx context.Context, tenantID, styleID int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE live_anchor_styles SET status='deleted', updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND status='active'`, styleID, tenantID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrLiveAnchorStyleNotFound
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM live_anchor_style_plan_bindings WHERE tenant_id=? AND style_id=?`, tenantID, styleID)
	return nil
}

func scanLiveAnchorStyleSample(row interface{ Scan(...any) error }) (model.LiveAnchorStyleSample, error) {
	var item model.LiveAnchorStyleSample
	var analysisRaw string
	var createdBy sql.NullInt64
	var analyzedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.TenantID, &item.StyleID, &item.Title, &item.SourceType, &item.OriginalName, &item.RawText, &item.ReadableText, &item.AnalysisStatus, &analysisRaw, &item.Provider, &item.Model, &item.LatencyMS, &item.Progress, &item.Stage, &item.ErrorMessage, &analyzedAt, &createdBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return item, err
	}
	item.Analysis = decodeLiveAnchorStyleProfile(analysisRaw)
	if analyzedAt.Valid {
		v := analyzedAt.Time
		item.AnalyzedAt = &v
	}
	if createdBy.Valid {
		v := createdBy.Int64
		item.CreatedByUserID = &v
	}
	return item, nil
}

const liveAnchorStyleSampleSelect = `
	SELECT id, tenant_id, style_id, title, source_type, original_name, raw_text, readable_text,
		analysis_status, analysis_json, provider, model, latency_ms, progress, stage, error_message,
		analyzed_at, created_by_user_id, created_at, updated_at
	FROM live_anchor_style_samples
`

func (s *Store) CreateLiveAnchorStyleSample(ctx context.Context, tenantID, styleID, actorID int64, input model.CreateLiveAnchorStyleSampleInput) (model.LiveAnchorStyleSample, error) {
	readable := strings.TrimSpace(input.ReadableText)
	if readable == "" {
		readable = strings.TrimSpace(input.RawText)
	}
	if readable == "" {
		return model.LiveAnchorStyleSample{}, errors.New("主播样本不能为空")
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = "主播样本"
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO live_anchor_style_samples (tenant_id, style_id, title, source_type, original_name, raw_text, readable_text, analysis_json, created_by_user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, tenantID, styleID, title, strings.TrimSpace(input.SourceType), strings.TrimSpace(input.OriginalName), input.RawText, readable, encodeJSON(emptyLiveAnchorStyleProfile()), actorID)
	if err != nil {
		return model.LiveAnchorStyleSample{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.LiveAnchorStyleSample{}, err
	}
	return s.GetLiveAnchorStyleSample(ctx, tenantID, styleID, id)
}

func (s *Store) ListLiveAnchorStyleSamples(ctx context.Context, tenantID, styleID int64) ([]model.LiveAnchorStyleSample, error) {
	rows, err := s.db.QueryContext(ctx, liveAnchorStyleSampleSelect+` WHERE tenant_id=? AND style_id=? ORDER BY created_at DESC, id DESC`, tenantID, styleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveAnchorStyleSample, 0)
	for rows.Next() {
		item, err := scanLiveAnchorStyleSample(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetLiveAnchorStyleSample(ctx context.Context, tenantID, styleID, sampleID int64) (model.LiveAnchorStyleSample, error) {
	item, err := scanLiveAnchorStyleSample(s.db.QueryRowContext(ctx, liveAnchorStyleSampleSelect+` WHERE id=? AND tenant_id=? AND style_id=?`, sampleID, tenantID, styleID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAnchorStyleSample{}, errors.New("主播样本不存在")
	}
	return item, err
}

func (s *Store) UpdateLiveAnchorStyleSampleAnalysis(ctx context.Context, tenantID, styleID, sampleID int64, profile model.LiveAgentPlanAnchorStyleProfile, provider, modelName string, latency int64, status string, progress int, stage, errorMessage string) (model.LiveAnchorStyleSample, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE live_anchor_style_samples
		SET analysis_status=?, analysis_json=?, provider=?, model=?, latency_ms=?, progress=?, stage=?, error_message=?, analyzed_at=CASE WHEN ?='analyzed' THEN CURRENT_TIMESTAMP(3) ELSE analyzed_at END, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND style_id=?
	`, status, encodeJSON(profile), provider, modelName, latency, progress, stage, errorMessage, status, sampleID, tenantID, styleID)
	if err != nil {
		return model.LiveAnchorStyleSample{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return model.LiveAnchorStyleSample{}, errors.New("主播样本不存在")
	}
	return s.GetLiveAnchorStyleSample(ctx, tenantID, styleID, sampleID)
}

func (s *Store) DeleteLiveAnchorStyleSample(ctx context.Context, tenantID, styleID, sampleID int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM live_anchor_style_samples WHERE id=? AND tenant_id=? AND style_id=?`, sampleID, tenantID, styleID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return errors.New("主播样本不存在")
	}
	return nil
}

func (s *Store) SaveLiveAnchorStyleProfile(ctx context.Context, tenantID, styleID, actorID int64, profile model.LiveAgentPlanAnchorStyleProfile) (model.LiveAnchorStyle, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE live_anchor_styles SET profile_json=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND status='active'`, encodeJSON(profile), actorID, styleID, tenantID)
	if err != nil {
		return model.LiveAnchorStyle{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return model.LiveAnchorStyle{}, ErrLiveAnchorStyleNotFound
	}
	return s.GetLiveAnchorStyle(ctx, tenantID, styleID)
}

func (s *Store) ListLiveAnchorStyleTrainings(ctx context.Context, tenantID, styleID int64) ([]model.LiveAnchorStyleTraining, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, style_id, request_text, target_chars, heat, expansion_freedom, selected_facts_json, generated_text, score_json, status, created_by_user_id, created_at, updated_at FROM live_anchor_style_trainings WHERE tenant_id=? AND style_id=? ORDER BY created_at DESC, id DESC`, tenantID, styleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveAnchorStyleTraining, 0)
	for rows.Next() {
		var item model.LiveAnchorStyleTraining
		var selectedRaw, scoreRaw string
		var createdBy sql.NullInt64
		if err := rows.Scan(&item.ID, &item.StyleID, &item.RequestText, &item.TargetChars, &item.Heat, &item.ExpansionFreedom, &selectedRaw, &item.GeneratedText, &scoreRaw, &item.Status, &createdBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(selectedRaw), &item.SelectedFacts)
		if item.SelectedFacts == nil {
			item.SelectedFacts = []string{}
		}
		if strings.TrimSpace(scoreRaw) != "" {
			_ = json.Unmarshal([]byte(scoreRaw), &item.Score)
		}
		if createdBy.Valid {
			v := createdBy.Int64
			item.CreatedByUserID = &v
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SaveLiveAnchorStyleTraining(ctx context.Context, tenantID, styleID, actorID int64, input model.LiveAnchorStyleTrainingInput) (model.LiveAnchorStyleTraining, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO live_anchor_style_trainings (tenant_id, style_id, request_text, target_chars, heat, expansion_freedom, selected_facts_json, generated_text, score_json, status, created_by_user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'draft', ?)`, tenantID, styleID, strings.TrimSpace(input.RequestText), input.TargetChars, input.Heat, input.ExpansionFreedom, encodeJSON(input.SelectedFacts), input.GeneratedText, encodeJSON(input.Score), actorID)
	if err != nil {
		return model.LiveAnchorStyleTraining{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.LiveAnchorStyleTraining{}, err
	}
	items, err := s.ListLiveAnchorStyleTrainings(ctx, tenantID, styleID)
	if err != nil {
		return model.LiveAnchorStyleTraining{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return model.LiveAnchorStyleTraining{}, errors.New("训练记录保存失败")
}

func (s *Store) UpdateLiveAnchorStyleTraining(ctx context.Context, tenantID, styleID, trainingID int64, input model.LiveAnchorStyleTrainingInput) (model.LiveAnchorStyleTraining, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE live_anchor_style_trainings SET request_text=?, target_chars=?, heat=?, expansion_freedom=?, selected_facts_json=?, generated_text=?, score_json=?, updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND style_id=?`, strings.TrimSpace(input.RequestText), input.TargetChars, input.Heat, input.ExpansionFreedom, encodeJSON(input.SelectedFacts), input.GeneratedText, encodeJSON(input.Score), trainingID, tenantID, styleID)
	if err != nil {
		return model.LiveAnchorStyleTraining{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return model.LiveAnchorStyleTraining{}, errors.New("训练记录不存在")
	}
	items, err := s.ListLiveAnchorStyleTrainings(ctx, tenantID, styleID)
	if err != nil {
		return model.LiveAnchorStyleTraining{}, err
	}
	for _, item := range items {
		if item.ID == trainingID {
			return item, nil
		}
	}
	return model.LiveAnchorStyleTraining{}, errors.New("训练记录保存失败")
}

func (s *Store) DeleteLiveAnchorStyleTraining(ctx context.Context, tenantID, styleID, trainingID int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM live_anchor_style_trainings WHERE id=? AND tenant_id=? AND style_id=?`, trainingID, tenantID, styleID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return errors.New("训练记录不存在")
	}
	return nil
}

func (s *Store) ListLiveAnchorStylePlugins(ctx context.Context, tenantID, styleID int64) ([]model.LiveAnchorStylePluginSetting, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, plugin_id, plugin_version, enabled, parameters_json, updated_at FROM live_anchor_style_plugins WHERE tenant_id=? AND style_id=? ORDER BY id`, tenantID, styleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveAnchorStylePluginSetting, 0)
	for rows.Next() {
		var item model.LiveAnchorStylePluginSetting
		var enabled int
		var raw string
		if err := rows.Scan(&item.ID, &item.PluginID, &item.PluginVersion, &enabled, &raw, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.StyleID = styleID
		item.Enabled = enabled != 0
		_ = json.Unmarshal([]byte(raw), &item.Parameters)
		if item.Parameters == nil {
			item.Parameters = map[string]any{}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpsertLiveAnchorStylePlugin(ctx context.Context, tenantID, styleID, actorID int64, input model.LiveAnchorStylePluginSetting) ([]model.LiveAnchorStylePluginSetting, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO live_anchor_style_plugins (tenant_id, style_id, plugin_id, plugin_version, enabled, parameters_json, updated_by_user_id) VALUES (?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE enabled=VALUES(enabled), parameters_json=VALUES(parameters_json), updated_by_user_id=VALUES(updated_by_user_id), updated_at=CURRENT_TIMESTAMP(3)`, tenantID, styleID, strings.TrimSpace(input.PluginID), strings.TrimSpace(input.PluginVersion), boolInt(input.Enabled), encodeJSON(input.Parameters), actorID)
	if err != nil {
		return nil, err
	}
	return s.ListLiveAnchorStylePlugins(ctx, tenantID, styleID)
}

func (s *Store) DeleteLiveAnchorStylePlugin(ctx context.Context, tenantID, styleID int64, pluginID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM live_anchor_style_plugins WHERE tenant_id=? AND style_id=? AND plugin_id=?`, tenantID, styleID, strings.TrimSpace(pluginID))
	return err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Store) BindLiveAnchorStyleToPlan(ctx context.Context, tenantID, styleID, planID, actorID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO live_anchor_style_plan_bindings (tenant_id, style_id, plan_id, bound_by_user_id) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE style_id=VALUES(style_id), bound_by_user_id=VALUES(bound_by_user_id), bound_at=CURRENT_TIMESTAMP(3)`, tenantID, styleID, planID, actorID)
	return err
}

func (s *Store) GetLiveAnchorStyleForPlan(ctx context.Context, tenantID, planID int64) (model.LiveAnchorStyle, error) {
	var styleID int64
	err := s.db.QueryRowContext(ctx, `SELECT style_id FROM live_anchor_style_plan_bindings WHERE tenant_id=? AND plan_id=? LIMIT 1`, tenantID, planID).Scan(&styleID)
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAnchorStyle{}, ErrLiveAnchorStyleNotFound
	}
	if err != nil {
		return model.LiveAnchorStyle{}, err
	}
	return s.GetLiveAnchorStyle(ctx, tenantID, styleID)
}
