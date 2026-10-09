package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"livecompanion/management/internal/model"
	"strings"
)

// The runtime session, rather than the platform capture session, owns generation traces.
func (s *Store) LoadProductionReview(ctx context.Context, tenant, room int64) (model.ProductionReview, error) {
	result := model.ProductionReview{Items: []model.ProductionReviewItem{}}
	var ended sql.NullTime
	if err := s.db.QueryRowContext(ctx, `SELECT id,started_at,ended_at FROM live_runtime_sessions WHERE tenant_id=? AND room_id=? ORDER BY id DESC LIMIT 1`, tenant, room).Scan(&result.RuntimeSessionID, &result.StartedAt, &ended); err != nil {
		return result, err
	}
	if ended.Valid {
		result.EndedAt = &ended.Time
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,task_key,decision,candidate_text,report_json,created_at FROM live_p0_audit_events WHERE tenant_id=? AND room_id=? AND (CAST(JSON_UNQUOTE(JSON_EXTRACT(report_json,'$.runtime_session_id')) AS UNSIGNED)=? OR (JSON_EXTRACT(report_json,'$.runtime_session_id') IS NULL AND created_at>=? AND (? IS NULL OR created_at<=?))) AND (decision<>'passed' OR JSON_LENGTH(JSON_EXTRACT(report_json,'$.warnings'))>0) ORDER BY id DESC LIMIT 200`, tenant, room, result.RuntimeSessionID, result.StartedAt, result.EndedAt, result.EndedAt)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item model.ProductionReviewItem
		var raw []byte
		if err = rows.Scan(&item.ID, &item.TaskKey, &item.Decision, &item.Text, &raw, &item.CreatedAt); err != nil {
			return result, err
		}
		var report struct {
			Kind     string   `json:"kind"`
			Stage    string   `json:"stage"`
			Reason   string   `json:"reason"`
			Error    string   `json:"error"`
			Warnings []string `json:"warnings"`
			Hits     []struct {
				RuleID string `json:"rule_id"`
				Text   string `json:"matched_text"`
			} `json:"hits"`
		}
		_ = json.Unmarshal(raw, &report)
		item.Kind, item.Stage, item.Reason = report.Kind, report.Stage, report.Reason
		if item.Kind == "" {
			item.Kind = "text_review"
			item.Stage = "文字审核"
		}
		if item.Reason == "" {
			item.Reason = report.Error
		}
		for _, hit := range report.Hits {
			if item.Reason != "" {
				item.Reason += "；"
			}
			item.Reason += hit.RuleID + "：" + hit.Text
		}
		if len(report.Warnings) > 0 {
			if item.Reason != "" {
				item.Reason += "；"
			}
			item.Reason += strings.Join(report.Warnings, "；")
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}
