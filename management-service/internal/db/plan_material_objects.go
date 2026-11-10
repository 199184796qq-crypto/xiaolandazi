package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/model"
)

var ErrMaterialObjectsConflict = errors.New("material object catalog changed")
var ErrMaterialObjectsInUse = errors.New("material store still referenced")

func (s *Store) PlanMaterialObjects(ctx context.Context, tenantID, planID int64) (model.MaterialObjectCatalog, error) {
	out := model.MaterialObjectCatalog{Stores: []model.MaterialStoreObject{}}
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT revision,stores_json FROM live_agent_plan_material_objects WHERE tenant_id=? AND plan_id=?`, tenantID, planID).Scan(&out.Revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out.Stores); err != nil {
		return out, err
	}
	return out, nil
}
func (s *Store) SavePlanMaterialObjects(ctx context.Context, tenantID, planID, revision int64, stores []model.MaterialStoreObject) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var owner int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE tenant_id=? AND id=? FOR UPDATE`, tenantID, planID).Scan(&owner); err != nil {
		return 0, err
	}
	var current int64
	var previousJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT revision,stores_json FROM live_agent_plan_material_objects WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenantID, planID).Scan(&current, &previousJSON)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if current != revision {
		return 0, ErrMaterialObjectsConflict
	}
	// Claim confirmation and full edits take the same owning-plan lock. Check
	// references in this transaction so concurrent saves cannot orphan a store.
	var previous []model.MaterialStoreObject
	if len(previousJSON) > 0 {
		if err = json.Unmarshal(previousJSON, &previous); err != nil {
			return 0, err
		}
	}
	retained := map[string]bool{}
	for _, store := range stores {
		retained[store.Name] = true
	}
	removed := map[string]bool{}
	for _, store := range previous {
		if !retained[store.Name] {
			removed[store.Name] = true
		}
	}
	if len(removed) > 0 {
		claims, e := listMaterialClaims(ctx, tx, tenantID, planID)
		if e != nil {
			return 0, e
		}
		for _, claim := range claims {
			if claim.ObjectTargets == nil && claim.ScopeKind == "store" && removed[claim.StoreName] {
				return 0, ErrMaterialObjectsInUse
			}
			for _, target := range claim.ObjectTargets {
				if target.Kind == "store" && removed[target.StoreName] {
					return 0, ErrMaterialObjectsInUse
				}
			}
		}
	}
	raw, err := json.Marshal(stores)
	if err != nil {
		return 0, err
	}
	next := current + 1
	if current == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_material_objects(tenant_id,plan_id,revision,stores_json) VALUES(?,?,?,?)`, tenantID, planID, next, string(raw))
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_material_objects SET revision=?,stores_json=? WHERE tenant_id=? AND plan_id=?`, next, string(raw), tenantID, planID)
	}
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return next, nil
}
