package db

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"livecompanion/management/internal/model"
)

var ErrMaterialFactOrderInvalid = errors.New("invalid material fact order")

type MaterialFactOrderChange struct {
	ObjectKey       string  `json:"object_key"`
	LibraryRevision string  `json:"library_revision"`
	Previous        []int64 `json:"previous"`
	Order           []int64 `json:"order"`
}

// ListPlanMaterialClaimsForDisplay applies an editorial order only for the
// library view. Recall, analysis and rewriting keep their existing ordering.
func (s *Store) ListPlanMaterialClaimsForDisplay(ctx context.Context, tenantID, planID int64) ([]model.PlanMaterialClaim, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	claims, err := listMaterialClaims(ctx, tx, tenantID, planID)
	if err != nil || len(claims) < 2 {
		return claims, err
	}
	catalog, err := readMaterialObjects(ctx, tx, tenantID, planID, false)
	if err != nil {
		return nil, err
	}
	return materialClaimsInDisplayOrder(claims, catalog.Directory.FactDisplayOrder), nil
}

func materialClaimsInDisplayOrder(claims []model.PlanMaterialClaim, order []int64) []model.PlanMaterialClaim {
	if len(order) == 0 {
		return claims
	}
	remaining := make(map[int64]model.PlanMaterialClaim, len(claims))
	for _, claim := range claims {
		remaining[claim.ID] = claim
	}
	result := make([]model.PlanMaterialClaim, 0, len(claims))
	for _, id := range order {
		if claim, ok := remaining[id]; ok {
			result = append(result, claim)
			delete(remaining, id)
		}
	}
	tail := len(result)
	for _, claim := range remaining {
		result = append(result, claim)
	}
	// Facts created after the last sort append in creation order.
	slices.SortFunc(result[tail:], func(a, b model.PlanMaterialClaim) int { return cmp.Compare(a.ID, b.ID) })
	return result
}

// The selector uses the same resolved ownership as the fact library view, with
// the object directory locked alongside the facts.
func (s *Store) ReorderPlanMaterialFacts(ctx context.Context, tenantID, planID int64, change MaterialFactOrderChange,
	selectObject func([]model.PlanMaterialClaim, model.MaterialObjectDirectory) []model.PlanMaterialClaim,
) (model.MaterialObjectCatalog, error) {
	var empty model.MaterialObjectCatalog
	if change.ObjectKey == "" || change.ObjectKey == "all" || change.ObjectKey == "unresolved" || len(change.Order) < 2 || len(change.Previous) != len(change.Order) {
		return empty, ErrMaterialFactOrderInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	claims, err := s.lockMaterialLibrary(ctx, tx, tenantID, planID, change.LibraryRevision)
	if err != nil {
		return empty, err
	}
	catalog, err := readMaterialObjects(ctx, tx, tenantID, planID, true)
	if err != nil {
		return empty, err
	}
	claims = selectObject(claims, catalog.Directory)
	ordered := materialClaimsInDisplayOrder(claims, catalog.Directory.FactDisplayOrder)
	ordered = materialClaimsInDisplayOrder(ordered, catalog.Directory.FactDisplayOrders[change.ObjectKey])
	current := make([]int64, len(ordered))
	for i, claim := range ordered {
		current[i] = claim.ID
	}
	expected, desired := slices.Clone(current), slices.Clone(change.Order)
	slices.Sort(expected)
	slices.Sort(desired)
	if !slices.Equal(expected, desired) {
		return empty, ErrMaterialFactOrderInvalid
	}
	// A lost success response can be retried without writing a second time.
	if slices.Equal(current, change.Order) {
		return catalog, nil
	}
	if !slices.Equal(current, change.Previous) {
		return empty, ErrMaterialClaimConflict
	}
	if catalog.Directory.FactDisplayOrders == nil {
		catalog.Directory.FactDisplayOrders = make(map[string][]int64)
	}
	catalog.Directory.FactDisplayOrders[change.ObjectKey] = change.Order
	orders, err := json.Marshal(catalog.Directory.FactDisplayOrders)
	if err != nil {
		return empty, err
	}
	if catalog.Revision == 0 {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO live_agent_plan_material_objects (tenant_id, plan_id, revision, stores_json, directory_json)
			VALUES (?, ?, 1, JSON_ARRAY(), JSON_OBJECT('fact_display_orders', CAST(? AS JSON)))
		`, tenantID, planID, string(orders))
		catalog.Revision = 1
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE live_agent_plan_material_objects
			SET directory_json=JSON_SET(COALESCE(directory_json, JSON_OBJECT()), '$.fact_display_orders', CAST(? AS JSON))
			WHERE tenant_id=? AND plan_id=?
		`, string(orders), tenantID, planID)
	}
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return catalog, nil
}
