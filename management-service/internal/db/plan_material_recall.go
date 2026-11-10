package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"livecompanion/management/internal/model"
)

type MaterialRecallRecord struct {
	Claim  model.PlanMaterialClaim
	Vector []float32
}

// A single read-only snapshot prevents pairing a new body with an old vector.
// Vectors remain server-side and never enter the public material-list response.
func (s *Store) PlanMaterialRecallSnapshot(ctx context.Context, tenant, plan int64) ([]MaterialRecallRecord, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	claims, err := listMaterialClaims(ctx, tx, tenant, plan)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,embedding_json FROM live_agent_plan_material_claims WHERE tenant_id=? AND plan_id=? AND status='active'`, tenant, plan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	vectors := map[int64][]float32{}
	for rows.Next() {
		var id int64
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var v []float32
		// Invalid old embeddings are reported as unavailable by the caller, never
		// repaired by purchasing vectors during a read/test request.
		if json.Unmarshal(raw, &v) == nil && ValidateMaterialVectors([][]float32{v}, 1) == nil {
			vectors[id] = v
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := make([]MaterialRecallRecord, 0, len(claims))
	for _, c := range claims {
		out = append(out, MaterialRecallRecord{c, vectors[c.ID]})
	}
	return out, nil
}
