package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"livecompanion/management/internal/model"
)

// Keep adopted versions and their source evidence intact when removing a record
// from the user's history. Hidden pending records release their deduplication key.
func (s *Store) DeleteInteractionHistory(ctx context.Context, tenant, room, plan, id int64) error {
	if tenant <= 0 || room <= 0 || plan <= 0 || id <= 0 {
		return fmt.Errorf("invalid history deletion scope")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM agent_learning_sessions WHERE id=? AND tenant_id=? AND room_id=?
	 AND JSON_EXTRACT(review_metadata_json,'$.plan_id')=?
	 AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(review_metadata_json,'$.history_deleted')),'false')<>'true' FOR UPDATE`, id, tenant, room, plan).Scan(&current); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE agent_learning_sessions
	 SET review_metadata_json=JSON_SET(review_metadata_json,'$.history_deleted',CAST('true' AS JSON)),pending_review_key=NULL,updated_at=CURRENT_TIMESTAMP(3)
	 WHERE id=? AND tenant_id=? AND room_id=? AND review_metadata_json IS NOT NULL
	 AND JSON_EXTRACT(review_metadata_json,'$.plan_id')=?
	 AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(review_metadata_json,'$.history_deleted')),'false')<>'true'`, id, tenant, room, plan)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	// Cancel an unfinished correction of this record as part of the same action.
	// Completed follow-up records and their adopted assets remain independent.
	if _, err := tx.ExecContext(ctx, `UPDATE agent_learning_sessions
	 SET review_metadata_json=JSON_SET(review_metadata_json,'$.history_deleted',CAST('true' AS JSON)),pending_review_key=NULL,updated_at=CURRENT_TIMESTAMP(3)
	 WHERE tenant_id=? AND room_id=? AND status='editing' AND JSON_EXTRACT(review_metadata_json,'$.plan_id')=?
	 AND JSON_EXTRACT(review_metadata_json,'$.history_origin_id')=?
	 AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(review_metadata_json,'$.history_deleted')),'false')<>'true'`, tenant, room, plan, id); err != nil {
		return err
	}
	return tx.Commit()
}

// A fresh correction draft preserves the processed history and adopted assets.
// Lock the original record so retries reuse one pending draft and cannot race a
// history deletion. No previous result can be accidentally adopted as a new turn.
func (s *Store) CreateInteractionHistoryCorrection(ctx context.Context, actor, tenant, room, plan, id int64) (model.AgentLearningSession, error) {
	if actor <= 0 || tenant <= 0 || room <= 0 || plan <= 0 || id <= 0 {
		return model.AgentLearningSession{}, fmt.Errorf("invalid history correction scope")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AgentLearningSession{}, err
	}
	defer tx.Rollback()
	original, err := scanAgentLearningSession(tx.QueryRowContext(ctx, `SELECT `+agentLearningSessionColumns+`
	 FROM agent_learning_sessions WHERE id=? AND tenant_id=? AND room_id=?
	 AND JSON_EXTRACT(review_metadata_json,'$.plan_id')=?
	 AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(review_metadata_json,'$.history_deleted')),'false')<>'true' FOR UPDATE`, id, tenant, room, plan))
	if err != nil {
		return model.AgentLearningSession{}, err
	}
	if original.Status == model.AgentLearningStatusEditing {
		return original, tx.Commit()
	}
	if original.Status != model.AgentLearningStatusClosed && original.Status != model.AgentLearningStatusAdopted {
		return model.AgentLearningSession{}, fmt.Errorf("history record is not correctable")
	}
	var draftID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM agent_learning_sessions WHERE tenant_id=? AND room_id=? AND status='editing'
	 AND JSON_EXTRACT(review_metadata_json,'$.plan_id')=? AND JSON_EXTRACT(review_metadata_json,'$.history_origin_id')=?
	 AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(review_metadata_json,'$.history_deleted')),'false')<>'true' ORDER BY id DESC LIMIT 1 FOR UPDATE`, tenant, room, plan, id).Scan(&draftID)
	if err != nil && err != sql.ErrNoRows {
		return model.AgentLearningSession{}, err
	}
	if err == sql.ErrNoRows {
		review := *original.Review
		review.HistoryOriginID = original.ID
		review.HistoryDeleted = false
		raw, e := json.Marshal(review)
		if e != nil {
			return model.AgentLearningSession{}, e
		}
		ref := fmt.Sprintf("interaction_history:%d", original.ID)
		if original.AdoptedMemoryItemID != nil {
			ref = fmt.Sprintf("agent_memory:%d", *original.AdoptedMemoryItemID)
		}
		result, e := tx.ExecContext(ctx, `INSERT INTO agent_learning_sessions
		 (tenant_id,room_id,source_type,source_ref,question,original_reply,target,status,memory_type,created_by_user_id,review_metadata_json)
		 VALUES(?,?,?,?,?,?,?,'editing',?,?,?)`, tenant, room, original.SourceType, ref, original.Question, original.OriginalReply, original.Target, original.MemoryType, actor, string(raw))
		if e != nil {
			return model.AgentLearningSession{}, e
		}
		draftID, err = result.LastInsertId()
		if err != nil {
			return model.AgentLearningSession{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.AgentLearningSession{}, err
	}
	return s.GetAgentLearningSession(ctx, tenant, room, draftID)
}
