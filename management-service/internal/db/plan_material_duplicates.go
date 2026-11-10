package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"livecompanion/management/internal/model"
)

type MaterialClaimExpectation struct {
	ID       int64  `json:"id"`
	Revision string `json:"revision"`
}

// Revision is computed from the stored row before any legacy display projection.
func MaterialClaimRevision(it model.PlanMaterialClaim) string {
	raw, _ := json.Marshal(it)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func CheckMaterialClaimExpectations(items []model.PlanMaterialClaim, expected []MaterialClaimExpectation, keepID int64) error {
	if len(expected) == 0 || len(expected) > 100 {
		return ErrMaterialClaimConflict
	}
	seen := map[int64]bool{}
	keeper := false
	for _, e := range expected {
		if e.ID <= 0 || len(e.Revision) != 64 || seen[e.ID] {
			return ErrMaterialClaimConflict
		}
		seen[e.ID] = true
		found := false
		for _, it := range items {
			if it.ID == e.ID && MaterialClaimRevision(it) == e.Revision {
				found = true
				break
			}
		}
		if !found {
			return ErrMaterialClaimConflict
		}
		keeper = keeper || e.ID == keepID
	}
	if !keeper {
		return ErrMaterialClaimConflict
	}
	return nil
}

type lockedMaterialQuery struct{ *sql.Tx }

func (q lockedMaterialQuery) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return q.Tx.QueryContext(ctx, query+" FOR UPDATE", args...)
}

// The retained body/vector and deletion of the other records commit together.
// A nil vector preserves the existing embedding for an unchanged document.
func (s *Store) ResolvePlanMaterialDuplicates(ctx context.Context, tenantID, planID, keepID int64, expected []MaterialClaimExpectation, it model.PlanMaterialClaimInput, embeddingModel string, vector []float32) error {
	if vector != nil {
		if err := ValidateMaterialVectors([][]float32{vector}, 1); err != nil {
			return err
		}
	}
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
	if err = CheckMaterialClaimExpectations(rows, expected, keepID); err != nil {
		return err
	}
	details, err := json.Marshal(it.MaterialClaimDetails)
	if err != nil {
		return err
	}
	conditions := it.Conditions
	if len(conditions) == 0 {
		conditions = json.RawMessage(`[]`)
	}
	args := []any{it.ClaimKey, it.Topic, it.Attribute, it.Value, it.Content, it.Polarity, it.FactType, string(conditions), string(details), it.SourceQuote, it.SourceSentence, it.ScopeKind, it.StoreName, it.ProductLinkID}
	query := `UPDATE live_agent_plan_material_claims SET claim_key=?,topic=?,attribute=?,value=?,content=?,polarity=?,fact_type=?,conditions_json=?,details_json=?,source_quote=?,source_sentence=?,scope_kind=?,store_name=?,product_link_id=?`
	if vector != nil {
		raw, _ := json.Marshal(vector)
		query += `,embedding_model=?,embedding_json=?`
		args = append(args, embeddingModel, string(raw))
	}
	query += `,updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND plan_id=? AND id=? AND status='active'`
	args = append(args, tenantID, planID, keepID)
	if _, err = tx.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	for _, e := range expected {
		if e.ID == keepID {
			continue
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM live_agent_plan_material_claims WHERE tenant_id=? AND plan_id=? AND id=? AND status='active'`, tenantID, planID, e.ID)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return ErrMaterialClaimConflict
		}
	}
	return tx.Commit()
}
