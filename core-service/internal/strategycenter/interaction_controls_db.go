package strategycenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/go-sql-driver/mysql"
)

func (s *Store) loadInteractionControls(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "SELECT tenant_id,room_id,active,switches,cooldowns,disabled_revisions,revision FROM live_room_interaction_controls")
	if err != nil {
		var missing *mysql.MySQLError
		if errors.As(err, &missing) && missing.Number == 1146 {
			return nil
		} // additive rolling upgrade, no row existed before this feature
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tenant, room int64
		var active bool
		var switches, cooldowns, disabled []byte
		var revision uint64
		if err = rows.Scan(&tenant, &room, &active, &switches, &cooldowns, &disabled, &revision); err != nil {
			return err
		}
		p := s.RoomInteractionPreferences(room)
		p.TenantID = tenant
		p.InteractionActive = &active
		p.ControlsRevision = revision
		if err = json.Unmarshal(disabled, &p.DisabledAt); err != nil {
			return err
		}
		if err = json.Unmarshal(switches, &p.Switches); err != nil {
			return err
		}
		if err = json.Unmarshal(cooldowns, &p.Cooldowns); err != nil {
			return err
		}
		if err = ValidateInteractionControls(p); err != nil {
			return err
		}
		s.PutRoomInteractionPreferences(room, p)
	}
	return rows.Err()
}
