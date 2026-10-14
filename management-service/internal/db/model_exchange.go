package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"strings"
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
	entry, lease, account := "", "", ""
	if s.modelPoolReady && scope.TenantID > 0 && scope.RoomID > 0 && !strings.Contains(stage, "tts") {
		var runtime int64
		err = s.db.QueryRowContext(ctx, `SELECT entry_id,lease_id,account_name,runtime_id FROM mgmt_model_pool_leases WHERE tenant_id=? AND room_id=? AND status='active'`, scope.TenantID, scope.RoomID).Scan(&entry, &lease, &account, &runtime)
		room := agentgateway.ContextRoomModelScope(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if room.RequireLease && (err != nil || (room.RuntimeID > 0 && runtime != room.RuntimeID)) {
			return 0, ErrModelPoolLeaseMissing
		}
		if entry != "" {
			profile = entry
		}
	}
	var result sql.Result
	if s.modelPoolReady {
		result, err = s.db.ExecContext(ctx, `INSERT INTO live_model_exchanges(tenant_id,plan_id,room_id,style_id,run_id,candidate_id,stage,profile_id,request_json,input_bytes,status,pool_entry_id,pool_lease_id,pool_account_name) VALUES(?,?,?,?,?,?,?,?,?,?,'sending',?,?,?)`, scope.TenantID, scope.PlanID, scope.RoomID, scope.StyleID, scope.RunID, scope.CandidateID, stage, profile, string(raw), len(raw), entry, lease, account)
	} else {
		result, err = s.db.ExecContext(ctx, `INSERT INTO live_model_exchanges(tenant_id,plan_id,room_id,style_id,run_id,candidate_id,stage,profile_id,request_json,input_bytes,status) VALUES(?,?,?,?,?,?,?,?,?,?,'sending')`, scope.TenantID, scope.PlanID, scope.RoomID, scope.StyleID, scope.RunID, scope.CandidateID, stage, profile, string(raw), len(raw))
	}
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
func (s *Store) CompleteModelExchange(ctx context.Context, id int64, response any, text string, latencyMS int64, inputTokens, outputTokens, cachedInputTokens *int64, callErr error) error {
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	status, errorText := "completed", ""
	if callErr != nil {
		status, errorText = "failed", callErr.Error()
	}
	_, err = s.db.ExecContext(ctx, `UPDATE live_model_exchanges SET response_json=?,output_bytes=?,raw_text=?,latency_ms=?,input_tokens=?,output_tokens=?,cached_input_tokens=?,status=?,error_text=?,finished_at=? WHERE id=? AND status='sending'`, string(raw), len(raw), text, latencyMS, inputTokens, outputTokens, cachedInputTokens, status, errorText, time.Now().UTC(), id)
	return err
}
