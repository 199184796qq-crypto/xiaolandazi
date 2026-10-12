package db

import (
	"context"
	"database/sql"
	"strings"
)

// RecordInteractionReplay 记录一次互动（观众问题 → 意图挖掘 → 最终回答），
// 供后续回测使用：改 prompt / 逻辑后，取历史 question 重跑对比新旧 reply 与 intent。
// intentJSON 为 answer_outline 的 JSON（可为空，例如 verbatim 模式没有意图挖掘）。
func (s *Store) RecordInteractionReplay(ctx context.Context, tenantID, roomID, planID int64, origin, question, reply, executionMode string, userLayerVersion uint64, intentJSON []byte, outlineError string) error {
	var intent sql.NullString
	if len(intentJSON) > 0 && string(intentJSON) != "null" {
		intent.Valid = true
		intent.String = string(intentJSON)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO live_interaction_replays (
			tenant_id, room_id, plan_id, origin, question, intent_json,
			reply, execution_mode, user_layer_version, outline_error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tenantID, roomID, planID, strings.TrimSpace(origin), strings.TrimSpace(question),
		intent, strings.TrimSpace(reply), strings.TrimSpace(executionMode), userLayerVersion,
		nullString(outlineError))
	return err
}

func nullString(v string) sql.NullString {
	if strings.TrimSpace(v) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}
