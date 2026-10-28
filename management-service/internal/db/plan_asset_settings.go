package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

// PlanAssetSettings belongs to the account's plan. No room-specific override is
// read or written here; rooms only reference the plan selected for playback.
type PlanAssetSettings struct {
	TenantID     int64  `json:"tenant_id"`
	PlanID       int64  `json:"plan_id"`
	ContentMode  string `json:"content_mode"`
	ReadingRules string `json:"reading_rules"`
}

func (s *Store) GetPlanAssetSettings(ctx context.Context, tenant, plan int64) (PlanAssetSettings, error) {
	out := PlanAssetSettings{TenantID: tenant, PlanID: plan, ContentMode: model.LiveContentAIDynamic}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_agent_plans WHERE tenant_id=? AND id=? AND status='active'`, tenant, plan).Scan(&exists); err != nil {
		return out, err
	}
	if exists == 0 {
		return out, ErrLiveAgentPlanNotFound
	}
	err := s.db.QueryRowContext(ctx, `SELECT content_mode,reading_rules FROM live_agent_plan_asset_settings WHERE tenant_id=? AND plan_id=?`, tenant, plan).Scan(&out.ContentMode, &out.ReadingRules)
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	// Legacy versions are already plan assets. Preserve the latest saved mode,
	// without guessing a room from historical bindings or importing room rules.
	var mode string
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(generation_context_json,'$.content_mode')),''),'ai_pregenerated') FROM live_agent_plan_versions WHERE tenant_id=? AND plan_id=? AND lifecycle_status IN ('draft','published','superseded') ORDER BY id DESC LIMIT 1`, tenant, plan).Scan(&mode)
	if err == nil {
		out.ContentMode = model.ResolveLiveContentMode(mode, true, "").ContentMode
		return out, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	return out, err
}

func (s *Store) SetPlanAssetMode(ctx context.Context, tenant, plan, user int64, mode string) (PlanAssetSettings, error) {
	current, err := s.GetPlanAssetSettings(ctx, tenant, plan)
	if err != nil {
		return current, err
	}
	if mode != model.LiveContentUserAudio && mode != model.LiveContentAIDynamic && mode != model.LiveContentAIPregenerated {
		return current, errors.New("无效的方案内容模式")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO live_agent_plan_asset_settings(tenant_id,plan_id,content_mode,reading_rules,updated_by_user_id) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE content_mode=VALUES(content_mode),updated_by_user_id=VALUES(updated_by_user_id)`, tenant, plan, mode, current.ReadingRules, user)
	if err != nil {
		return current, err
	}
	return s.GetPlanAssetSettings(ctx, tenant, plan)
}

func (s *Store) GetPlanReadingRules(ctx context.Context, tenant, plan int64) (model.RoomReadingRules, error) {
	item, err := s.GetPlanAssetSettings(ctx, tenant, plan)
	return model.RoomReadingRules{TenantID: tenant, PlanID: plan, RulesText: item.ReadingRules}, err
}

func (s *Store) SetPlanReadingRules(ctx context.Context, tenant, plan, user int64, text string) (model.RoomReadingRules, error) {
	current, err := s.GetPlanAssetSettings(ctx, tenant, plan)
	if err != nil {
		return model.RoomReadingRules{}, err
	}
	text = strings.TrimSpace(text)
	if len([]rune(text)) > 4000 || strings.Count(text, "\n") >= 200 {
		return model.RoomReadingRules{}, errors.New("读写规则最多4000字、200行")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO live_agent_plan_asset_settings(tenant_id,plan_id,content_mode,reading_rules,updated_by_user_id) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE reading_rules=VALUES(reading_rules),updated_by_user_id=VALUES(updated_by_user_id)`, tenant, plan, current.ContentMode, text, user)
	if err != nil {
		return model.RoomReadingRules{}, err
	}
	return s.GetPlanReadingRules(ctx, tenant, plan)
}
