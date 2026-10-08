package db

import (
	"context"
	"encoding/json"
	"time"
)

type ModelExchangeScope struct {
	TenantID    int64  `json:"tenant_id"`
	PlanID      int64  `json:"plan_id"`
	RoomID      int64  `json:"room_id"`
	StyleID     int64  `json:"style_id"`
	RunID       string `json:"run_id"`
	CandidateID string `json:"candidate_id"`
}

// Payload bytes are application payload sizes, not HTTP/TLS network traffic.
func (s *Store) BeginModelExchange(ctx context.Context, scope ModelExchangeScope, stage, profile string, request any) (int64, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return 0, err
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO live_model_exchanges(tenant_id,plan_id,room_id,style_id,run_id,candidate_id,stage,profile_id,request_json,input_bytes,status) VALUES(?,?,?,?,?,?,?,?,?,?,'sending')`, scope.TenantID, scope.PlanID, scope.RoomID, scope.StyleID, scope.RunID, scope.CandidateID, stage, profile, string(raw), len(raw))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
func (s *Store) CompleteModelExchange(ctx context.Context, id int64, response any, text string, latencyMS int64, inputTokens, outputTokens *int64, callErr error) error {
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	status, errorText := "completed", ""
	if callErr != nil {
		status, errorText = "failed", callErr.Error()
	}
	_, err = s.db.ExecContext(ctx, `UPDATE live_model_exchanges SET response_json=?,output_bytes=?,raw_text=?,latency_ms=?,input_tokens=?,output_tokens=?,status=?,error_text=?,finished_at=? WHERE id=? AND status='sending'`, string(raw), len(raw), text, latencyMS, inputTokens, outputTokens, status, errorText, time.Now().UTC(), id)
	return err
}
