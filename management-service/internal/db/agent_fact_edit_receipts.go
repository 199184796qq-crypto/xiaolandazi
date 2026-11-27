package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"livecompanion/management/internal/model"
)

const agentFactEditReceiptsDDL = `CREATE TABLE IF NOT EXISTS live_agent_plan_fact_edit_receipts (
	tenant_id BIGINT UNSIGNED NOT NULL,
	plan_id BIGINT UNSIGNED NOT NULL,
	actor_user_id BIGINT UNSIGNED NOT NULL,
	conversation_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
	preview_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
	result_json JSON NOT NULL,
	committed_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
	PRIMARY KEY (tenant_id,plan_id,actor_user_id,conversation_id,preview_id),
	KEY idx_plan_fact_edit_plan (plan_id),
	CONSTRAINT fk_plan_fact_edit_plan FOREIGN KEY (plan_id) REFERENCES live_agent_plans(id) ON DELETE CASCADE
) ENGINE=InnoDB`

type AgentFactEditScope struct {
	TenantID, PlanID, ActorID int64
	ConversationID            string
}

type agentFactEditReceipt struct {
	Scope     AgentFactEditScope
	PreviewID string
	Result    json.RawMessage
}

func (s *Store) AgentFactEditReceipt(ctx context.Context, scope AgentFactEditScope, previewID string) (json.RawMessage, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT result_json FROM live_agent_plan_fact_edit_receipts
		WHERE tenant_id=? AND plan_id=? AND actor_user_id=? AND conversation_id=? AND preview_id=?`,
		scope.TenantID, scope.PlanID, scope.ActorID, scope.ConversationID, previewID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return json.RawMessage(raw), err
}

func (s *Store) AgentFactEditReceipts(ctx context.Context, scope AgentFactEditScope) ([]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT result_json FROM live_agent_plan_fact_edit_receipts
		WHERE tenant_id=? AND plan_id=? AND actor_user_id=? AND conversation_id=?
		ORDER BY committed_at DESC, preview_id DESC LIMIT 80`,
		scope.TenantID, scope.PlanID, scope.ActorID, scope.ConversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		results = append(results, json.RawMessage(raw))
	}
	return results, rows.Err()
}

func (s *Store) CommitAgentFactEdit(ctx context.Context, scope AgentFactEditScope, previewID string,
	result json.RawMessage, claimID int64, input model.PlanMaterialClaimInput, embeddingModel string,
	embedding []byte, libraryRevision, objectRevision string, newSource bool) error {
	if len(libraryRevision) != 64 || len(objectRevision) != 64 ||
		(newSource && input.SourceRef == nil) || !json.Valid(result) {
		return ErrMaterialClaimConflict
	}
	receipt := &agentFactEditReceipt{Scope: scope, PreviewID: previewID, Result: result}
	return s.updatePlanMaterialClaim(ctx, scope.TenantID, scope.PlanID, claimID, input, embeddingModel,
		embedding, libraryRevision, objectRevision, "", newSource, receipt)
}

func (r *agentFactEditReceipt) save(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO live_agent_plan_fact_edit_receipts
		(tenant_id,plan_id,actor_user_id,conversation_id,preview_id,result_json) VALUES (?,?,?,?,?,?)`,
		r.Scope.TenantID, r.Scope.PlanID, r.Scope.ActorID, r.Scope.ConversationID, r.PreviewID, string(r.Result))
	return err
}
