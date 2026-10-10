package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"livecompanion/management/internal/interactionlearning"
	"livecompanion/management/internal/model"
)

func (s *Store) MigrateInteractionLearning(ctx context.Context) error {
	for _, c := range []struct{ name, sql string }{{"review_metadata_json", "ALTER TABLE agent_learning_sessions ADD COLUMN review_metadata_json JSON NULL"}, {"pending_review_key", "ALTER TABLE agent_learning_sessions ADD COLUMN pending_review_key CHAR(64) NULL"}} {
		exists, err := s.columnExists(ctx, "agent_learning_sessions", c.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.db.ExecContext(ctx, c.sql); err != nil {
				return err
			}
		}
	}
	cols, err := s.indexColumns(ctx, "agent_learning_sessions", "uk_agent_learning_pending_review")
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		_, err = s.db.ExecContext(ctx, `ALTER TABLE agent_learning_sessions ADD UNIQUE KEY uk_agent_learning_pending_review(pending_review_key)`)
		return err
	}
	if len(cols) != 1 || cols[0] != "pending_review_key" {
		return fmt.Errorf("unexpected interaction review index")
	}
	return nil
}

func (s *Store) RecordInteractionLearningReview(ctx context.Context, o model.InteractionLearningObservation) error {
	if o.TenantID <= 0 || o.RoomID <= 0 || o.Review.PlanID <= 0 || strings.TrimSpace(o.Question) == "" || strings.TrimSpace(o.SourceRef) == "" {
		return fmt.Errorf("invalid interaction review scope")
	}
	if o.SourceType != "initial_simulation" && o.SourceType != "live_uncertain" {
		return fmt.Errorf("invalid review source")
	}
	if err := interactionlearning.ValidateReview(o.Review); err != nil {
		return err
	}
	o.Review.Occurrences = 1
	o.Review.LastSourceRef = o.SourceRef
	if o.Review.NeededFacts == nil {
		o.Review.NeededFacts = []string{}
	}
	raw, err := json.Marshal(o.Review)
	if err != nil {
		return err
	}
	target := []rune(o.Review.Meaning)
	if len(target) > 255 {
		target = target[:255]
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_learning_sessions
	(tenant_id,room_id,source_type,source_ref,question,original_reply,target,status,memory_type,created_by_user_id,review_metadata_json,pending_review_key)
	VALUES(?,?,?,?,?,?,?,'editing','',?,?,?)
	ON DUPLICATE KEY UPDATE
	 review_metadata_json=JSON_SET(VALUES(review_metadata_json),'$.occurrences',
	  CAST(COALESCE(JSON_UNQUOTE(JSON_EXTRACT(review_metadata_json,'$.occurrences')),'1') AS UNSIGNED)+IF(JSON_UNQUOTE(JSON_EXTRACT(review_metadata_json,'$.last_source_ref'))=VALUES(source_ref),0,1)),
	 original_reply=VALUES(original_reply), source_ref=VALUES(source_ref),source_type=IF(VALUES(source_type)='live_uncertain','live_uncertain',source_type),updated_at=CURRENT_TIMESTAMP(3)`,
		o.TenantID, o.RoomID, o.SourceType, o.SourceRef, strings.TrimSpace(o.Question), o.Reply, string(target), o.ActorUserID, string(raw), interactionlearning.PendingKey(o))
	return err
}

func (s *Store) ListInteractionLearningReviews(ctx context.Context, tenantID, roomID, planID int64) ([]model.AgentLearningSession, int, error) {
	where := `tenant_id=? AND room_id=? AND status='editing' AND review_metadata_json IS NOT NULL AND JSON_EXTRACT(review_metadata_json,'$.plan_id')=? AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(review_metadata_json,'$.history_deleted')),'false')<>'true'`
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_learning_sessions WHERE `+where, tenantID, roomID, planID).Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+agentLearningSessionColumns+` FROM agent_learning_sessions WHERE `+where+` ORDER BY updated_at DESC,id DESC LIMIT 100`, tenantID, roomID, planID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []model.AgentLearningSession{}
	for rows.Next() {
		item, e := scanAgentLearningSession(rows)
		if e != nil {
			return nil, 0, e
		}
		items = append(items, item)
	}
	return items, count, rows.Err()
}

func (s *Store) CloseInteractionLearningReview(ctx context.Context, tenantID, roomID, sessionID int64) error {
	r, err := s.db.ExecContext(ctx, `UPDATE agent_learning_sessions SET status='closed',pending_review_key=NULL,updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND room_id=? AND status='editing' AND review_metadata_json IS NOT NULL AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(review_metadata_json,'$.history_deleted')),'false')<>'true'`, sessionID, tenantID, roomID)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return fmt.Errorf("review no longer pending")
	}
	return err
}
