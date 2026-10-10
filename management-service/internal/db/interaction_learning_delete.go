package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Only pending simulation records are removable here. Adopted assets and other
// review sources are independent records and remain intact.
func (s *Store) DeleteSimulationReviews(ctx context.Context, tenant, room, plan, id int64) (int64, error) {
	if tenant <= 0 || room <= 0 || plan <= 0 || id < 0 {
		return 0, fmt.Errorf("invalid simulation deletion scope")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	where := `tenant_id=? AND room_id=? AND source_type='initial_simulation' AND status='editing' AND review_metadata_json IS NOT NULL AND JSON_EXTRACT(review_metadata_json,'$.plan_id')=? AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(review_metadata_json,'$.history_deleted')),'false')<>'true'`
	args := []any{tenant, room, plan}
	if id > 0 {
		where += ` AND id=?`
		args = append(args, id)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM agent_learning_sessions WHERE `+where+` ORDER BY id FOR UPDATE`, args...)
	if err != nil {
		return 0, err
	}
	ids := []int64{}
	for rows.Next() {
		var value int64
		if err = rows.Scan(&value); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if id > 0 && len(ids) == 0 {
		return 0, sql.ErrNoRows
	}
	var deleted int64
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		values := make([]any, len(chunk))
		for i, value := range chunk {
			values[i] = value
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		for _, table := range []string{"agent_learning_results", "agent_learning_evidence"} {
			if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE session_id IN (`+placeholders+`)`, values...); err != nil {
				return 0, err
			}
		}
		result, e := tx.ExecContext(ctx, `DELETE FROM agent_learning_sessions WHERE id IN (`+placeholders+`)`, values...)
		if e != nil {
			return 0, e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return 0, e
		}
		deleted += n
	}
	if deleted != int64(len(ids)) {
		return 0, fmt.Errorf("simulation records changed during deletion")
	}
	return deleted, tx.Commit()
}
