package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"golang.org/x/text/unicode/norm"
	"livecompanion/management/internal/model"
	"sort"
	"strconv"
	"strings"
)

func MaterialLibraryRevision(items []model.PlanMaterialClaim) string {
	rows := append([]model.PlanMaterialClaim(nil), items...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	versions := []string{}
	for _, it := range rows {
		versions = append(versions, MaterialClaimRevision(it))
	}
	raw, _ := json.Marshal(versions)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func materialNormalized(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(s)), " "))
}
func materialCanonical(raw json.RawMessage) string {
	var value any
	if len(raw) == 0 || string(raw) == "null" {
		return "[]"
	}
	if json.Unmarshal(raw, &value) != nil {
		return string(raw)
	}
	b, _ := json.Marshal(value)
	return string(b)
}
func materialDestination(it model.PlanMaterialClaimInput) string {
	if it.Destination == "" {
		return "fact"
	}
	return it.Destination
}
func materialTopic(it model.PlanMaterialClaimInput) string {
	if it.Topic != "" {
		return it.Topic
	}
	return it.Attribute
}
func materialBody(it model.PlanMaterialClaimInput) string {
	polarity := it.Polarity
	if polarity == "" {
		polarity = "positive"
	}
	raw, _ := json.Marshal([]string{materialNormalized(it.Content), materialNormalized(it.Value), polarity, materialNormalized(it.Question), materialNormalized(it.Answer)})
	return string(raw)
}

// Explicit object identities only; inheritance is a retrieval concern.
func MaterialOwnerKey(t model.MaterialObjectTarget) string {
	switch t.Kind {
	case "plan":
		return "plan"
	case "store":
		if strings.TrimSpace(t.StoreName) != "" {
			return "store:" + strings.TrimSpace(t.StoreName)
		}
	case "product":
		if t.LinkID > 0 {
			return "link:" + strconv.FormatInt(t.LinkID, 10)
		}
	}
	return ""
}
func MaterialClaimCollision(a, b model.PlanMaterialClaimInput, _ []model.MaterialStoreObject) bool {
	if materialDestination(a) != materialDestination(b) || materialCanonical(a.Conditions) != materialCanonical(b.Conditions) || materialCanonical(a.Exceptions) != materialCanonical(b.Exceptions) {
		return false
	}
	sameAttribute := false
	if materialDestination(a) == "qa" {
		sameAttribute = materialNormalized(a.Question) != "" && materialNormalized(a.Question) == materialNormalized(b.Question)
	} else {
		key := materialNormalized(a.ClaimKey)
		sameAttribute = (key != "" && key != "claim" && key == materialNormalized(b.ClaimKey)) || (materialNormalized(materialTopic(a)) != "" && materialNormalized(materialTopic(a)) == materialNormalized(materialTopic(b)))
	}
	if !sameAttribute {
		return false
	}
	for _, at := range a.ObjectTargets {
		owner := MaterialOwnerKey(at)
		if owner == "" {
			continue
		}
		for _, bt := range b.ObjectTargets {
			if owner == MaterialOwnerKey(bt) {
				return true
			}
		}
	}
	return false
}

// A duplicate alert requires exact value and displayed source text. Existing
// attribute replacement review continues to use MaterialClaimCollision.
func MaterialClaimDuplicate(a, b model.PlanMaterialClaimInput) bool {
	original := func(it model.PlanMaterialClaimInput) string {
		if it.SourceSentence != "" {
			return it.SourceSentence
		}
		return it.SourceQuote
	}
	source := original(a)
	return MaterialClaimCollision(a, b, nil) && strings.TrimSpace(a.Value) != "" && strings.TrimSpace(source) != "" && a.Value == b.Value && source == original(b)
}

func (s *Store) lockMaterialLibrary(ctx context.Context, tx *sql.Tx, tenantID, planID int64, revision string) ([]model.PlanMaterialClaim, error) {
	if len(revision) != 64 {
		return nil, ErrMaterialClaimConflict
	}
	var owner int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE tenant_id=? AND id=? FOR UPDATE`, tenantID, planID).Scan(&owner); err != nil {
		return nil, err
	}
	rows, err := listMaterialClaims(ctx, lockedMaterialQuery{tx}, tenantID, planID)
	if err != nil {
		return nil, err
	}
	if MaterialLibraryRevision(rows) != revision {
		return nil, ErrMaterialClaimConflict
	}
	return rows, nil
}

// Clear only the active material library of the explicitly reviewed plan.
// A new or edited row after the confirmation snapshot invalidates the request.
func (s *Store) ClearPlanMaterialLibrary(ctx context.Context, tenantID, planID int64, revision string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = s.lockMaterialLibrary(ctx, tx, tenantID, planID, revision); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM live_agent_plan_material_claims WHERE tenant_id=? AND plan_id=? AND status='active'`, tenantID, planID)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// Appends and explicit replacements share a single atomic adoption. A stable
// full-library snapshot prevents new duplicates and stale keep/replace choices.
func (s *Store) ImportPlanMaterialLibrary(ctx context.Context, tenantID, planID, actorID int64, revision string, objectRevision int64, items []model.PlanMaterialClaimInput, removeIDs []int64, embeddingModel string, vectors [][]float32) (int, error) {
	if len(items) > 0 {
		if err := ValidateMaterialVectors(vectors, len(items)); err != nil {
			return 0, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := s.lockMaterialLibrary(ctx, tx, tenantID, planID, revision)
	if err != nil {
		return 0, err
	}
	var currentObjects int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM live_agent_plan_material_objects WHERE tenant_id=? AND plan_id=?`, tenantID, planID).Scan(&currentObjects)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	if currentObjects != objectRevision {
		return 0, ErrMaterialClaimConflict
	}
	seen := map[int64]bool{}
	for _, id := range removeIDs {
		found := false
		for _, row := range rows {
			found = found || row.ID == id
		}
		if !found || seen[id] {
			return 0, ErrMaterialClaimConflict
		}
		seen[id] = true
	}
	for i, it := range items {
		if strings.TrimSpace(it.Content) == "" {
			return 0, ErrMaterialClaimConflict
		}
		conditions := it.Conditions
		if len(conditions) == 0 {
			conditions = json.RawMessage(`[]`)
		}
		details, e := json.Marshal(it.MaterialClaimDetails)
		if e != nil {
			return 0, e
		}
		vector, _ := json.Marshal(vectors[i])
		_, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_material_claims (tenant_id,plan_id,claim_key,topic,attribute,value,content,polarity,fact_type,conditions_json,details_json,source_quote,source_sentence,scope_kind,store_name,product_link_id,embedding_model,embedding_json,status,created_by_user_id) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'active',?)`, tenantID, planID, it.ClaimKey, it.Topic, it.Attribute, it.Value, it.Content, it.Polarity, it.FactType, string(conditions), string(details), it.SourceQuote, it.SourceSentence, it.ScopeKind, it.StoreName, it.ProductLinkID, embeddingModel, string(vector), actorID)
		if err != nil {
			return 0, err
		}
	}
	for _, id := range removeIDs {
		if _, err = tx.ExecContext(ctx, `DELETE FROM live_agent_plan_material_claims WHERE tenant_id=? AND plan_id=? AND id=? AND status='active'`, tenantID, planID, id); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(items), nil
}
