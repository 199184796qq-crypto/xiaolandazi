package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrMaterialObjectsConflict = errors.New("material object catalog changed")
var ErrMaterialObjectsInUse = errors.New("material store still referenced")
var ErrMaterialObjectMissing = errors.New("material object missing")
var ErrMaterialObjectName = errors.New("invalid or duplicate material object name")

func (s *Store) PlanMaterialObjects(ctx context.Context, tenantID, planID int64) (model.MaterialObjectCatalog, error) {
	return readMaterialObjects(ctx, s.db, tenantID, planID, false)
}

func readMaterialObjects(ctx context.Context, q qaQueryer, tenantID, planID int64, lock bool) (model.MaterialObjectCatalog, error) {
	out := model.MaterialObjectCatalog{Stores: []model.MaterialStoreObject{}, Directory: model.MaterialObjectDirectory{Objects: []model.MaterialNamedObject{}, LinkDisplayNames: map[int64]string{}}}
	var raw, directory []byte
	query := `SELECT revision,stores_json,directory_json FROM live_agent_plan_material_objects WHERE tenant_id=? AND plan_id=?`
	if lock {
		query += ` FOR UPDATE`
	}
	err := q.QueryRowContext(ctx, query, tenantID, planID).Scan(&out.Revision, &raw, &directory)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out.Stores); err != nil {
		return out, err
	}
	if len(directory) > 0 {
		err = json.Unmarshal(directory, &out.Directory)
	}
	return out, err
}

// Check directory targets under the same plan lock used by object deletion.
func validateMaterialDirectoryTargets(ctx context.Context, tx *sql.Tx, tenant, plan int64, it model.PlanMaterialClaimInput) error {
	if !slices.ContainsFunc(it.ObjectTargets, func(target model.MaterialObjectTarget) bool {
		return target.Kind == "object" || target.Kind == "product" && target.LinkID != 0
	}) {
		return nil
	}
	catalog, err := readMaterialObjects(ctx, tx, tenant, plan, true)
	if err != nil {
		return err
	}
	for _, target := range it.ObjectTargets {
		if target.Kind == "product" {
			if slices.Contains(catalog.Directory.RemovedLinkIDs, target.LinkID) {
				return ErrMaterialClaimConflict
			}
		} else if target.Kind == "object" && !slices.ContainsFunc(catalog.Directory.Objects, func(object model.MaterialNamedObject) bool { return object.ID == target.ObjectID }) {
			return ErrMaterialClaimConflict
		}
	}
	return nil
}

type MaterialObjectChange struct {
	Revision     int64    `json:"revision"`
	Action       string   `json:"action"`
	Kind         string   `json:"kind"`
	ID           int64    `json:"object_id"`
	Name         string   `json:"display_name"`
	DisplayOrder []string `json:"display_order"`
}

func materialObjectOrderKey(kind string, id int64) string {
	return kind + ":" + strconv.FormatInt(id, 10)
}

func validMaterialObjectOrder(dir model.MaterialObjectDirectory, order []string, links map[int64]string) bool {
	remaining := make(map[string]struct{}, len(dir.Objects)+len(links))
	for _, object := range dir.Objects {
		remaining[materialObjectOrderKey("object", object.ID)] = struct{}{}
	}
	for id := range links {
		if !slices.Contains(dir.RemovedLinkIDs, id) {
			remaining[materialObjectOrderKey("product", id)] = struct{}{}
		}
	}
	if len(order) != len(remaining) {
		return false
	}
	for _, key := range order {
		if _, ok := remaining[key]; !ok {
			return false
		}
		delete(remaining, key)
	}
	return true
}

// Legacy link objects remain available until removed from this directory.
// Removing one never deletes the commerce link itself.
func (s *Store) ChangePlanMaterialObject(ctx context.Context, tenant, plan int64, change MaterialObjectChange, linkNames map[int64]string, legacyLinkNo int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE tenant_id=? AND id=? FOR UPDATE`, tenant, plan).Scan(&owner); err != nil {
		return err
	}
	catalog, err := readMaterialObjects(ctx, tx, tenant, plan, true)
	if err != nil {
		return err
	}
	if catalog.Revision != change.Revision {
		return ErrMaterialObjectsConflict
	}
	dir := &catalog.Directory
	index := slices.IndexFunc(dir.Objects, func(object model.MaterialNamedObject) bool { return object.ID == change.ID })
	if (change.Kind == "object" && change.Action != "add" && index < 0) ||
		(change.Kind == "product" && (linkNames[change.ID] == "" || slices.Contains(dir.RemovedLinkIDs, change.ID))) {
		return ErrMaterialObjectMissing
	}
	if change.Action == "add" || change.Action == "rename" {
		if err = validateMaterialObjectName(catalog, change, linkNames); err != nil {
			return err
		}
	}
	switch {
	case change.Action == "reorder" && change.Kind == "":
		if !validMaterialObjectOrder(*dir, change.DisplayOrder, linkNames) {
			return ErrMaterialObjectsConflict
		}
		dir.DisplayOrder = change.DisplayOrder
	case change.Action == "add" && change.Kind == "object":
		var data [8]byte
		if _, err = rand.Read(data[:]); err != nil {
			return err
		}
		id := int64(binary.LittleEndian.Uint64(data[:]) & ((1 << 52) - 1))
		if id == 0 || slices.ContainsFunc(dir.Objects, func(object model.MaterialNamedObject) bool { return object.ID == id }) {
			return ErrMaterialObjectsConflict
		}
		dir.Objects = append(dir.Objects, model.MaterialNamedObject{ID: id, DisplayName: change.Name})
	case change.Action == "rename" && change.Kind == "plan":
		dir.GlobalDisplayName = change.Name
	case change.Action == "rename" && change.Kind == "object":
		dir.Objects[index].DisplayName = change.Name
	case change.Action == "rename" && change.Kind == "product":
		if dir.LinkDisplayNames == nil {
			dir.LinkDisplayNames = map[int64]string{}
		}
		dir.LinkDisplayNames[change.ID] = change.Name
	case change.Action == "delete" && (change.Kind == "object" || change.Kind == "product"):
		if change.Kind == "object" {
			dir.Objects = slices.Delete(dir.Objects, index, index+1)
		} else {
			dir.RemovedLinkIDs = append(dir.RemovedLinkIDs, change.ID)
			delete(dir.LinkDisplayNames, change.ID)
		}
		key := materialObjectOrderKey(change.Kind, change.ID)
		delete(dir.FactDisplayOrders, key)
		dir.DisplayOrder = slices.DeleteFunc(dir.DisplayOrder, func(entry string) bool { return entry == key })
		if err = detachMaterialObject(ctx, tx, tenant, plan, change, legacyLinkNo); err != nil {
			return err
		}
	default:
		return ErrMaterialObjectMissing
	}
	data, err := json.Marshal(dir)
	if err != nil {
		return err
	}
	if catalog.Revision == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_material_objects(tenant_id,plan_id,revision,stores_json,directory_json) VALUES(?,?,1,JSON_ARRAY(),?)`, tenant, plan, string(data))
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_material_objects SET revision=revision+1,directory_json=? WHERE tenant_id=? AND plan_id=?`, string(data), tenant, plan)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func validateMaterialObjectName(catalog model.MaterialObjectCatalog, change MaterialObjectChange, linkNames map[int64]string) error {
	name, dir := change.Name, catalog.Directory
	if name == "" || len([]rune(name)) > 50 || strings.ContainsAny(name, "\r\n") || name == "待归属" || name == "未归档" || name == "全部属性" ||
		(change.Kind != "plan" && (name == "全局" || name == dir.GlobalDisplayName)) {
		return ErrMaterialObjectName
	}
	for id, label := range linkNames {
		if change.Kind == "product" && change.ID == id || slices.Contains(dir.RemovedLinkIDs, id) {
			continue
		}
		if override := dir.LinkDisplayNames[id]; override != "" {
			label = override
		}
		if strings.EqualFold(name, label) {
			return ErrMaterialObjectName
		}
	}
	for _, object := range dir.Objects {
		if !(change.Kind == "object" && change.ID == object.ID) && strings.EqualFold(name, object.DisplayName) {
			return ErrMaterialObjectName
		}
	}
	for _, store := range catalog.Stores {
		if strings.EqualFold(name, store.Name) {
			return ErrMaterialObjectName
		}
	}
	return nil
}

func detachMaterialObject(ctx context.Context, tx *sql.Tx, tenant, plan int64, change MaterialObjectChange, legacyLinkNo int64) error {
	claims, err := listMaterialClaims(ctx, lockedMaterialQuery{tx}, tenant, plan)
	if err != nil {
		return err
	}
	for _, claim := range claims {
		targets := claim.ObjectTargets
		if targets == nil && claim.ScopeKind == "product" && claim.ProductLinkID > 0 {
			targets = []model.MaterialObjectTarget{{Kind: "product", LinkNo: claim.ProductLinkID}}
		}
		kept := make([]model.MaterialObjectTarget, 0, len(targets))
		for _, target := range targets {
			removed := change.Kind == "object" && target.Kind == "object" && target.ObjectID == change.ID
			if change.Kind == "product" && target.Kind == "product" {
				removed = target.LinkID == change.ID || target.LinkID == 0 && legacyLinkNo > 0 && target.LinkNo == legacyLinkNo
			}
			if !removed {
				kept = append(kept, target)
			}
		}
		if len(kept) == len(targets) {
			continue
		}
		claim.ObjectTargets = kept
		claim.ScopeKind, claim.StoreName, claim.ProductLinkID = "unresolved", "", 0
		if len(kept) > 0 {
			first := kept[0]
			claim.ScopeKind, claim.StoreName, claim.ProductLinkID = first.Kind, first.StoreName, max(first.LinkID, first.LinkNo)
		} else {
			claim.NeedsConfirmation = true
		}
		details, err := json.Marshal(claim.MaterialClaimDetails)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_material_claims SET details_json=?,scope_kind=?,store_name=?,product_link_id=?,updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND plan_id=? AND id=? AND status='active'`, string(details), claim.ScopeKind, claim.StoreName, claim.ProductLinkID, tenant, plan, claim.ID); err != nil {
			return err
		}
	}
	return nil
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
		qaRecords, e := listQA(ctx, tx, tenantID, planID)
		if e != nil {
			return 0, e
		}
		for _, r := range qaRecords {
			for _, target := range r.Entry.Targets {
				if target.Kind == "store" && removed[target.StoreName] {
					return 0, ErrMaterialObjectsInUse
				}
			}
		}
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
