package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	"livecompanion/management/internal/model"
)

var ErrInteractionControlsConflict = errors.New("互动开关已被更新，请刷新后再保存")

type controlsReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readRoomInteractionControls(ctx context.Context, q controlsReader, tenant, room int64) (model.RoomInteractionControls, error) {
	var c model.RoomInteractionControls
	var active bool
	var switches, cooldowns, disabled []byte
	err := q.QueryRowContext(ctx, `SELECT active,switches,cooldowns,disabled_revisions,revision,updated_at,updated_by FROM live_room_interaction_controls WHERE tenant_id=? AND room_id=?`, tenant, room).Scan(&active, &switches, &cooldowns, &disabled, &c.Revision, &c.UpdatedAt, &c.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(disabled, &c.DisabledAt); err != nil {
		return c, err
	}
	c.Active = &active
	if err = json.Unmarshal(switches, &c.Switches); err != nil {
		return c, err
	}
	if err = json.Unmarshal(cooldowns, &c.Cooldowns); err != nil {
		return c, err
	}
	return c, model.ValidateInteractionControls(c.Switches, c.Cooldowns)
}
func (s *Store) GetRoomInteractionControls(ctx context.Context, tenant, room int64) (model.RoomInteractionControls, error) {
	return readRoomInteractionControls(ctx, s.db, tenant, room)
}
func (s *Store) withRoomInteractionControls(ctx context.Context, item model.RoomInteractionPreferences, err error) (model.RoomInteractionPreferences, error) {
	if err != nil {
		return item, err
	}
	c, err := s.GetRoomInteractionControls(ctx, item.TenantID, item.RoomID)
	if err != nil {
		return item, err
	}
	item.ControlsDisabledAt = c.DisabledAt
	item.InteractionActive = c.Active
	item.Switches = c.Switches
	item.Cooldowns = c.Cooldowns
	item.ControlsRevision = c.Revision
	item.ControlsUpdatedAt = c.UpdatedAt
	if c.UpdatedBy > 0 {
		item.ControlsUpdatedByUserID = &c.UpdatedBy
	}
	return item, nil
}

// The caller locks the tenant-owned core_rooms row in this same transaction.
// Old clients omit all three controls fields and cannot reset saved controls.
func (s *Store) saveRoomInteractionControlsTx(ctx context.Context, tx *sql.Tx, tenant, room, user int64, in model.RoomInteractionPreferencesInput) error {
	if in.InteractionActive == nil && in.Switches == nil && in.Cooldowns == nil {
		return nil
	}
	before, err := readRoomInteractionControls(ctx, tx, tenant, room)
	if err != nil {
		return err
	}
	if in.ControlsRevision == nil || *in.ControlsRevision != before.Revision {
		return ErrInteractionControlsConflict
	}
	next := before
	if next.Active == nil {
		on := true
		next.Active = &on
	}
	if in.InteractionActive != nil {
		next.Active = in.InteractionActive
	}
	if in.Switches != nil {
		next.Switches = in.Switches
	}
	if in.Cooldowns != nil {
		next.Cooldowns = in.Cooldowns
	}
	if next.Switches == nil {
		next.Switches = map[string]bool{}
	}
	if next.Cooldowns == nil {
		next.Cooldowns = map[string]int{}
	}
	if before.Active != nil && *before.Active == *next.Active && reflect.DeepEqual(before.Switches, next.Switches) && reflect.DeepEqual(before.Cooldowns, next.Cooldowns) {
		return nil
	}
	next.Revision = before.Revision + 1
	next.UpdatedBy = user
	next.DisabledAt = map[string]uint64{}
	for k, v := range before.DisabledAt {
		next.DisabledAt[k] = v
	}
	for _, k := range model.InteractionControlKinds {
		if before.Enabled(k) && !next.Enabled(k) {
			next.DisabledAt[k] = next.Revision
		}
	}
	switches, _ := json.Marshal(next.Switches)
	cooldowns, _ := json.Marshal(next.Cooldowns)
	disabled, _ := json.Marshal(next.DisabledAt)
	_, err = tx.ExecContext(ctx, `INSERT INTO live_room_interaction_controls(tenant_id,room_id,active,switches,cooldowns,disabled_revisions,revision,updated_at,updated_by) VALUES(?,?,?,?,?,?,?,CURRENT_TIMESTAMP(6),?) ON DUPLICATE KEY UPDATE active=VALUES(active),switches=VALUES(switches),cooldowns=VALUES(cooldowns),disabled_revisions=VALUES(disabled_revisions),revision=VALUES(revision),updated_at=VALUES(updated_at),updated_by=VALUES(updated_by)`, tenant, room, *next.Active, string(switches), string(cooldowns), string(disabled), next.Revision, user)
	if err != nil {
		return err
	}
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(next)
	_, err = tx.ExecContext(ctx, `INSERT INTO live_room_interaction_control_history(tenant_id,room_id,revision,before_json,after_json,updated_by) VALUES(?,?,?,?,?,?)`, tenant, room, next.Revision, string(a), string(b), user)
	return err
}
