package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
)

var ErrInteractionScheduleConflict = errors.New("互动配置已更新或方案已切换，请刷新后再保存")

func (s *Store) interactionPlanID(ctx context.Context, tenantID, roomID int64) (int64, error) {
	plan, err := s.GetLiveAgentPlanForRoom(ctx, tenantID, roomID)
	if errors.Is(err, ErrLiveAgentPlanNotFound) {
		return 0, nil
	}
	return plan.ID, err
}
func (s *Store) GetRoomInteractionSchedule(ctx context.Context, tenantID, roomID int64) (model.InteractionSchedule, error) {
	planID, err := s.interactionPlanID(ctx, tenantID, roomID)
	if err != nil {
		return model.InteractionSchedule{}, err
	}
	var raw []byte
	var version int64
	err = s.db.QueryRowContext(ctx, `SELECT version_no,config_json FROM live_room_interaction_config_versions WHERE tenant_id=? AND room_id=? AND plan_id=? ORDER BY version_no DESC LIMIT 1`, tenantID, roomID, planID).Scan(&version, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		cfg, e := interactionpolicy.Load()
		if e != nil {
			return cfg, e
		}
		cfg.Version = 1
		cfg.PlanID = planID
		raw, e = json.Marshal(cfg)
		if e != nil {
			return cfg, e
		}
		_, e = s.db.ExecContext(ctx, `INSERT IGNORE INTO live_room_interaction_config_versions (tenant_id,room_id,plan_id,version_no,config_json) VALUES (?,?,?,?,?)`, tenantID, roomID, planID, 1, raw)
		if e != nil {
			return cfg, e
		}
		// Another request may have initialized/edited the room while we loaded.
		return s.GetRoomInteractionSchedule(ctx, tenantID, roomID)
	}
	if err != nil {
		return model.InteractionSchedule{}, err
	}
	var cfg model.InteractionSchedule
	if cfg, err = interactionpolicy.Decode(raw); err != nil {
		return cfg, err
	}
	if err = interactionpolicy.Validate(cfg); err != nil {
		return cfg, err
	}
	cfg.Version = version
	cfg.PlanID = planID
	return cfg, nil
}
func (s *Store) saveRoomInteractionScheduleTx(ctx context.Context, tx *sql.Tx, tenantID, roomID, userID int64, cfg, current model.InteractionSchedule) (model.InteractionSchedule, error) {
	if err := interactionpolicy.Validate(cfg); err != nil {
		return cfg, err
	}
	var planID int64
	err := tx.QueryRowContext(ctx, `SELECT plan_id FROM live_agent_room_plan_selections WHERE tenant_id=? AND room_id=? FOR UPDATE`, tenantID, roomID).Scan(&planID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return cfg, err
	}
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT MAX(version_no) FROM live_room_interaction_config_versions WHERE tenant_id=? AND room_id=? AND plan_id=?`, tenantID, roomID, current.PlanID).Scan(&version); err != nil {
		return cfg, err
	}
	if cfg.PlanID != planID || cfg.PlanID != current.PlanID || cfg.Version != version {
		return cfg, ErrInteractionScheduleConflict
	}
	// An unchanged schedule does not create a new version merely because the
	// user adjusts an existing frequency slider.
	cfg.TemplateVersion = current.TemplateVersion
	cfg.TemplateSHA256 = current.TemplateSHA256
	raw, err := json.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	previous, err := json.Marshal(current)
	if err != nil {
		return cfg, err
	}
	if string(raw) == string(previous) {
		return current, nil
	}
	cfg.Version = version + 1
	raw, err = json.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO live_room_interaction_config_versions (tenant_id,room_id,plan_id,version_no,config_json,created_by_user_id) VALUES (?,?,?,?,?,?)`, tenantID, roomID, cfg.PlanID, cfg.Version, raw, userID)
	return cfg, err
}
