package strategycenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func CloneInteractionSchedule(s InteractionSchedule) InteractionSchedule {
	raw, _ := json.Marshal(s)
	var out InteractionSchedule
	_ = json.Unmarshal(raw, &out)
	return out
}
func (s *Store) InteractionScheduleForPlan(roomID, tenantID, planID int64) (InteractionSchedule, error) {
	prefs := s.RoomInteractionPreferences(roomID)
	if prefs.Schedule != nil && prefs.TenantID == tenantID && prefs.Schedule.PlanID == planID {
		return CloneInteractionSchedule(*prefs.Schedule), nil
	}
	if s == nil || s.scheduleDB == nil {
		return LoadInteractionMother()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var existing int64
	if err := s.scheduleDB.QueryRowContext(ctx, `SELECT id FROM core_rooms WHERE tenant_id=? AND id=?`, tenantID, roomID).Scan(&existing); err != nil {
		return InteractionSchedule{}, err
	}
	var raw []byte
	var version int64
	err := s.scheduleDB.QueryRowContext(ctx, `SELECT version_no,config_json FROM live_room_interaction_config_versions WHERE tenant_id=? AND room_id=? AND plan_id=? ORDER BY version_no DESC LIMIT 1`, tenantID, roomID, planID).Scan(&version, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		cfg, e := LoadInteractionMother()
		if e != nil {
			return cfg, e
		}
		cfg.Version = 1
		cfg.PlanID = planID
		raw, e = json.Marshal(cfg)
		if e != nil {
			return cfg, e
		}
		_, e = s.scheduleDB.ExecContext(ctx, `INSERT IGNORE INTO live_room_interaction_config_versions (tenant_id,room_id,plan_id,version_no,config_json) VALUES (?,?,?,?,?)`, tenantID, roomID, planID, 1, raw)
		if e != nil {
			return cfg, e
		}
		err = s.scheduleDB.QueryRowContext(ctx, `SELECT version_no,config_json FROM live_room_interaction_config_versions WHERE tenant_id=? AND room_id=? AND plan_id=? ORDER BY version_no DESC LIMIT 1`, tenantID, roomID, planID).Scan(&version, &raw)
	}
	var cfg InteractionSchedule
	if err != nil {
		return cfg, err
	}
	if cfg, err = Decode(raw); err != nil {
		return cfg, err
	}
	if err = ValidateInteractionSchedule(cfg); err != nil {
		return cfg, err
	}
	cfg.Version = version
	cfg.PlanID = planID
	return cfg, nil
}
