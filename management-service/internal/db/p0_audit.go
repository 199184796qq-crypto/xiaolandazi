package db

import (
	"context"
	"encoding/json"
	"errors"

	"livecompanion/management/internal/model"
)

func (s *Store) SaveP0PolicyVersion(ctx context.Context, sha, version, source, md string) error {
	if len(sha) != 64 || version == "" || md == "" {
		return errors.New("P0规则快照无效")
	}
	_, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO live_p0_policy_versions (rule_sha256, policy_version, prompt_source, policy_md) VALUES (?, ?, ?, ?)`, sha, version, source, md)
	return err
}

func (s *Store) SaveP0AuditEvent(ctx context.Context, e model.P0AuditEvent) error {
	if e.TenantID <= 0 || e.PlanID <= 0 || e.RunID == "" || e.CandidateID == "" || len(e.RuleSHA256) != 64 || !json.Valid(e.Report) {
		return errors.New("P0审核记录参数无效")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO live_p0_audit_events (tenant_id, plan_id, room_id, run_id, candidate_id, rule_sha256, phase, task_key, rewrite_number, decision, candidate_text, raw_response, request_prompt, report_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, e.TenantID, e.PlanID, e.RoomID, e.RunID, e.CandidateID, e.RuleSHA256, e.Phase, e.TaskKey, e.RewriteNumber, e.Decision, e.Text, e.RawResponse, e.Prompt, string(e.Report))
	return err
}

func (s *Store) ListP0AuditEvents(ctx context.Context, tenant, plan int64, run string, limit int) ([]model.P0AuditEvent, error) {
	if tenant <= 0 || plan <= 0 || limit < 1 || limit > 200 {
		return nil, errors.New("P0记录查询范围无效")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, tenant_id, plan_id, room_id, run_id, candidate_id, rule_sha256, phase, task_key, rewrite_number, decision, candidate_text, raw_response, request_prompt, report_json, created_at FROM live_p0_audit_events WHERE tenant_id=? AND plan_id=? AND (?='' OR run_id=?) ORDER BY id DESC LIMIT ?`, tenant, plan, run, run, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.P0AuditEvent{}
	for rows.Next() {
		var e model.P0AuditEvent
		var report []byte
		if err := rows.Scan(&e.ID, &e.TenantID, &e.PlanID, &e.RoomID, &e.RunID, &e.CandidateID, &e.RuleSHA256, &e.Phase, &e.TaskKey, &e.RewriteNumber, &e.Decision, &e.Text, &e.RawResponse, &e.Prompt, &report, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Report = json.RawMessage(report)
		items = append(items, e)
	}
	return items, rows.Err()
}
