package model

import (
	"encoding/json"
	"time"
)

type P0AuditEvent struct {
	ID            int64           `json:"id"`
	TenantID      int64           `json:"tenant_id"`
	PlanID        int64           `json:"plan_id"`
	RoomID        int64           `json:"room_id"`
	RunID         string          `json:"run_id"`
	CandidateID   string          `json:"candidate_id"`
	RuleSHA256    string          `json:"rule_sha256"`
	Phase         string          `json:"phase"`
	TaskKey       string          `json:"task_key"`
	RewriteNumber int             `json:"rewrite_number"`
	Decision      string          `json:"decision"`
	Text          string          `json:"text"`
	RawResponse   string          `json:"raw_response"`
	Prompt        string          `json:"prompt"`
	Report        json.RawMessage `json:"report"`
	CreatedAt     time.Time       `json:"created_at"`
}
