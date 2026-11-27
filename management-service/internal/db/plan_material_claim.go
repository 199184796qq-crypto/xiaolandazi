package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrMaterialClaimNotFound = errors.New("material claim not found")
var ErrMaterialClaimConflict = errors.New("material claim changed; refresh before replacing")

const materialClaimSelect = `SELECT id, tenant_id, plan_id, claim_key, topic, attribute, value, content,
 polarity, fact_type, COALESCE(conditions_json, JSON_ARRAY()), COALESCE(source_quote,''), COALESCE(source_sentence,''),
 scope_kind, store_name, product_link_id, embedding_model, status, COALESCE(created_by_user_id,0), created_at, updated_at,
 COALESCE(details_json, JSON_OBJECT()) FROM live_agent_plan_material_claims
 WHERE tenant_id=? AND plan_id=? AND status='active'`

type materialQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func listMaterialClaims(ctx context.Context, q materialQuery, tenantID, planID int64) ([]model.PlanMaterialClaim, error) {
	rows, err := q.QueryContext(ctx, materialClaimSelect+` ORDER BY updated_at DESC, id DESC`, tenantID, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PlanMaterialClaim{}
	for rows.Next() {
		var it model.PlanMaterialClaim
		var conditions, details []byte
		if err := rows.Scan(&it.ID, &it.TenantID, &it.PlanID, &it.ClaimKey, &it.Topic, &it.Attribute, &it.Value, &it.Content,
			&it.Polarity, &it.FactType, &conditions, &it.SourceQuote, &it.SourceSentence, &it.ScopeKind, &it.StoreName, &it.ProductLinkID,
			&it.EmbeddingModel, &it.Status, &it.CreatedByUserID, &it.CreatedAt, &it.UpdatedAt, &details); err != nil {
			return nil, err
		}
		if len(conditions) == 0 {
			conditions = []byte("[]")
		}
		it.Conditions = conditions
		if len(details) > 0 {
			if err := json.Unmarshal(details, &it.MaterialClaimDetails); err != nil {
				return nil, err
			}
		}
		if it.Destination == "" {
			it.Destination = "fact"
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	projectSharedMaterialSentences(out)
	return out, nil
}
func (s *Store) ListPlanMaterialClaims(ctx context.Context, tenantID, planID int64) ([]model.PlanMaterialClaim, error) {
	return listMaterialClaims(ctx, s.db, tenantID, planID)
}

// PlanRewriteFactsSnapshot reads claims and their object directory at one revision.
func (s *Store) PlanRewriteFactsSnapshot(ctx context.Context, tenant, plan int64) ([]model.PlanMaterialClaim, model.MaterialObjectCatalog, []QALink, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, model.MaterialObjectCatalog{}, nil, err
	}
	defer tx.Rollback()
	claims, err := listMaterialClaims(ctx, tx, tenant, plan)
	if err != nil || len(claims) == 0 {
		return claims, model.MaterialObjectCatalog{}, nil, err
	}
	catalog, links, _, err := qaObjects(ctx, tx, tenant, plan)
	return claims, catalog, links, err
}

// MaterialClaimSame compares meaning-bearing data, including scope and rule
// predicates. Different conditions are independent claims, never duplicates.
func MaterialClaimSame(a, b model.PlanMaterialClaimInput) bool {
	identity := func(it model.PlanMaterialClaimInput) string {
		dest := it.Destination
		if dest == "" {
			dest = "fact"
		}
		var conditions, exceptions any
		_ = json.Unmarshal(it.Conditions, &conditions)
		_ = json.Unmarshal(it.Exceptions, &exceptions)
		if conditions == nil {
			conditions = []any{}
		}
		if exceptions == nil {
			exceptions = []any{}
		}
		raw, _ := json.Marshal([]any{dest, it.ScopeKind, it.ProductLinkID, it.StoreName, it.ScopeName, it.ClaimKey, it.Topic, it.Content, it.Value, it.Polarity, it.Question, it.Answer, it.Aliases, conditions, exceptions, it.ObjectTargets})
		return string(raw)
	}
	return identity(a) == identity(b)
}
func MaterialClaimInput(it model.PlanMaterialClaim) model.PlanMaterialClaimInput {
	return model.PlanMaterialClaimInput{MaterialClaimDetails: it.MaterialClaimDetails, ClaimKey: it.ClaimKey, Topic: it.Topic, Attribute: it.Attribute, Value: it.Value, Content: it.Content, Polarity: it.Polarity, FactType: it.FactType, Conditions: it.Conditions, SourceQuote: it.SourceQuote, SourceSentence: it.SourceSentence, ScopeKind: it.ScopeKind, StoreName: it.StoreName, ProductLinkID: it.ProductLinkID}
}
func ValidateMaterialVectors(vectors [][]float32, count int) error {
	if len(vectors) != count {
		return errors.New("embedding count mismatch")
	}
	dimension := 0
	for _, v := range vectors {
		if len(v) == 0 {
			return errors.New("empty embedding")
		}
		if dimension == 0 {
			dimension = len(v)
		}
		if dimension != len(v) {
			return errors.New("embedding dimension mismatch")
		}
		norm := float64(0)
		for _, x := range v {
			if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
				return errors.New("non-finite embedding")
			}
			norm += float64(x) * float64(x)
		}
		if norm == 0 {
			return errors.New("zero embedding")
		}
	}
	return nil
}

// Serializing on the owning plan makes duplicate confirms idempotent. All
// inserts and explicit replacements commit together. Existing callers may
// supply vectors; a MySQL-only write stores NULL in the vector column.
func (s *Store) ReplacePlanMaterialClaims(ctx context.Context, tenantID, planID, actorID int64, items []model.PlanMaterialClaimInput, embeddingModel string, vectors [][]float32) (int, error) {
	if err := validateMaterialWriteVectors(embeddingModel, vectors, len(items)); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var owner int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE tenant_id=? AND id=? FOR UPDATE`, tenantID, planID).Scan(&owner); err != nil {
		return 0, err
	}
	existing, err := listMaterialClaims(ctx, tx, tenantID, planID)
	if err != nil {
		return 0, err
	}
	added := 0
	for i, it := range items {
		if err := validateMaterialDirectoryTargets(ctx, tx, tenantID, planID, it); err != nil {
			return 0, err
		}
		if strings.TrimSpace(it.Content) == "" {
			return 0, errors.New("claim content is empty")
		}
		duplicate := false
		for _, old := range existing {
			if MaterialClaimSame(it, MaterialClaimInput(old)) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		if it.ReplacesID > 0 {
			found := false
			for _, old := range existing {
				if old.ID == it.ReplacesID {
					found = true
					break
				}
			}
			if !found {
				return 0, ErrMaterialClaimConflict
			}
		}
		conditions := it.Conditions
		if len(conditions) == 0 {
			conditions = []byte("[]")
		}
		details, err := json.Marshal(it.MaterialClaimDetails)
		if err != nil {
			return 0, err
		}
		vector := materialWriteVector(vectors, i)
		result, err := tx.ExecContext(ctx, `INSERT INTO live_agent_plan_material_claims
   (tenant_id,plan_id,claim_key,topic,attribute,value,content,polarity,fact_type,conditions_json,details_json,
    source_quote,source_sentence,scope_kind,store_name,product_link_id,embedding_model,embedding_json,status,created_by_user_id)
   VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'active',?)`,
			tenantID, planID, it.ClaimKey, it.Topic, it.Attribute, it.Value, it.Content, it.Polarity, it.FactType, string(conditions), string(details), it.SourceQuote, it.SourceSentence, it.ScopeKind, it.StoreName, it.ProductLinkID, embeddingModel, vector, actorID)
		if err != nil {
			return 0, err
		}
		if it.ReplacesID > 0 {
			result, err := tx.ExecContext(ctx, `DELETE FROM live_agent_plan_material_claims WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'`, it.ReplacesID, tenantID, planID)
			if err != nil {
				return 0, err
			}
			n, _ := result.RowsAffected()
			if n != 1 {
				return 0, ErrMaterialClaimConflict
			}
			filtered := existing[:0]
			for _, old := range existing {
				if old.ID != it.ReplacesID {
					filtered = append(filtered, old)
				}
			}
			existing = filtered
		}
		id, _ := result.LastInsertId()
		existing = append(existing, model.PlanMaterialClaim{ID: id, MaterialClaimDetails: it.MaterialClaimDetails, ClaimKey: it.ClaimKey, Topic: it.Topic, Value: it.Value, Content: it.Content, Polarity: it.Polarity, Conditions: it.Conditions, ScopeKind: it.ScopeKind, StoreName: it.StoreName, ProductLinkID: it.ProductLinkID})
		added++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return added, nil
}

func (s *Store) UpdatePlanMaterialClaim(ctx context.Context, tenantID, planID, id int64, it model.PlanMaterialClaimInput, embeddingModel string, embedding []byte) error {
	return s.UpdatePlanMaterialClaimAtRevision(ctx, tenantID, planID, id, it, embeddingModel, embedding, "", false)
}

func (s *Store) UpdatePlanMaterialClaimAtRevision(ctx context.Context, tenantID, planID, id int64, it model.PlanMaterialClaimInput, embeddingModel string, embedding []byte, revision string, newSource bool) error {
	return s.updatePlanMaterialClaim(ctx, tenantID, planID, id, it, embeddingModel, embedding, "", "", revision, newSource, nil)
}

// CommitEditedPlanMaterialClaim saves the corrected evidence and its claim in
// one transaction, after verifying both catalogs still match the preview.
func (s *Store) CommitEditedPlanMaterialClaim(ctx context.Context, tenantID, planID, id int64, it model.PlanMaterialClaimInput, embeddingModel string, embedding []byte, libraryRevision, objectRevision string) error {
	if len(libraryRevision) != 64 || len(objectRevision) != 64 || it.SourceRef == nil {
		return ErrMaterialClaimConflict
	}
	return s.updatePlanMaterialClaim(ctx, tenantID, planID, id, it, embeddingModel, embedding, libraryRevision, objectRevision, "", true, nil)
}

func (s *Store) updatePlanMaterialClaim(ctx context.Context, tenantID, planID, id int64, it model.PlanMaterialClaimInput, embeddingModel string, embedding []byte, libraryRevision, objectRevision, claimRevision string, newSource bool, receipt *agentFactEditReceipt) error {
	conditions := it.Conditions
	if len(conditions) == 0 {
		conditions = []byte("[]")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var claims []model.PlanMaterialClaim
	if libraryRevision == "" {
		var owner int64
		if err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE tenant_id=? AND id=? FOR UPDATE`, tenantID, planID).Scan(&owner); err != nil {
			return err
		}
		if claimRevision != "" || newSource {
			claims, err = listMaterialClaims(ctx, lockedMaterialQuery{tx}, tenantID, planID)
			if err != nil {
				return err
			}
		}
	} else {
		if claims, err = s.lockMaterialLibrary(ctx, tx, tenantID, planID, libraryRevision); err != nil {
			return err
		}
		_, _, currentRevision, err := readQAObjects(ctx, tx, tenantID, planID, true)
		if err != nil {
			return err
		}
		if currentRevision != objectRevision {
			return ErrMaterialClaimConflict
		}
	}
	if claims != nil {
		var previous *model.PlanMaterialClaim
		for index := range claims {
			if claims[index].ID == id {
				previous = &claims[index]
				break
			}
		}
		if previous == nil {
			return ErrMaterialClaimNotFound
		}
		if claimRevision != "" && MaterialClaimRevision(*previous) != claimRevision {
			return ErrMaterialClaimConflict
		}
		if newSource {
			if err = saveSharedMaterialSentence(ctx, tx, *previous, &it, claims); err != nil {
				return err
			}
		}
	}
	details, err := json.Marshal(it.MaterialClaimDetails)
	if err != nil {
		return err
	}
	if err = validateMaterialDirectoryTargets(ctx, tx, tenantID, planID, it); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE live_agent_plan_material_claims
 SET claim_key=?,topic=?,attribute=?,value=?,content=?,polarity=?,fact_type=?,conditions_json=?,details_json=?,
 source_quote=?,source_sentence=?,scope_kind=?,store_name=?,product_link_id=?,embedding_model=?,embedding_json=?,updated_at=CURRENT_TIMESTAMP(3)
 WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'`,
		it.ClaimKey, it.Topic, it.Attribute, it.Value, it.Content, it.Polarity, it.FactType, string(conditions), string(details), it.SourceQuote, it.SourceSentence, it.ScopeKind, it.StoreName, it.ProductLinkID, embeddingModel, optionalMaterialVector(embedding), id, tenantID, planID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrMaterialClaimNotFound
	}
	if receipt != nil {
		if err = receipt.save(ctx, tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetPlanMaterialClaimBenefit changes only the editorial label. The semantic
// document and embedding stay byte-for-byte the same.
func (s *Store) SetPlanMaterialClaimBenefit(ctx context.Context, tenantID, planID, id int64, revision string, benefit bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE tenant_id=? AND id=? FOR UPDATE`, tenantID, planID).Scan(&owner); err != nil {
		return err
	}
	rows, err := listMaterialClaims(ctx, lockedMaterialQuery{tx}, tenantID, planID)
	if err != nil {
		return err
	}
	for _, claim := range rows {
		if claim.ID != id {
			continue
		}
		if MaterialClaimRevision(claim) != revision {
			return ErrMaterialClaimConflict
		}
		claim.IsBenefit = benefit
		details, err := json.Marshal(claim.MaterialClaimDetails)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_material_claims SET details_json=?,updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND plan_id=? AND id=? AND status='active'`, string(details), tenantID, planID, id); err != nil {
			return err
		}
		return tx.Commit()
	}
	return ErrMaterialClaimNotFound
}
func (s *Store) DeletePlanMaterialClaim(ctx context.Context, tenantID, planID, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM live_agent_plan_material_claims WHERE id=? AND tenant_id=? AND plan_id=?`, id, tenantID, planID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrMaterialClaimNotFound
	}
	return nil
}
func (s *Store) UpdatePlanMaterialClaimScope(ctx context.Context, tenantID, planID, id int64, scopeKind string, productLinkID int64) error {
	// Legacy endpoint retained for older clients. New clients use full update so
	// the scope-aware document is re-embedded, not silently left stale.
	if scopeKind != "product" {
		scopeKind = "plan"
		productLinkID = 0
	}
	result, err := s.db.ExecContext(ctx, `UPDATE live_agent_plan_material_claims SET scope_kind=?,product_link_id=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'`, scopeKind, productLinkID, id, tenantID, planID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrMaterialClaimNotFound
	}
	return nil
}

func validateMaterialWriteVectors(embeddingModel string, vectors [][]float32, count int) error {
	if embeddingModel == "" && len(vectors) == 0 {
		return nil
	}
	return ValidateMaterialVectors(vectors, count)
}

func materialWriteVector(vectors [][]float32, index int) any {
	if len(vectors) == 0 {
		return nil
	}
	raw, _ := json.Marshal(vectors[index])
	return string(raw)
}

func optionalMaterialVector(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
