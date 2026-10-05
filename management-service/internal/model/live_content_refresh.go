package model

import "time"

type LiveContentRefreshCandidate struct {
	TenantID         int64
	RoomID           int64
	PlanID           int64
	BaseVersionID    int64
	RuntimeSessionID int64
	ExecutionRealm   string
	StartedAt        time.Time
	PublishedAt      time.Time
}

type LiveContentRefreshJob struct {
	ID                int64                            `json:"id"`
	TenantID          int64                            `json:"tenant_id"`
	RoomID            int64                            `json:"room_id"`
	PlanID            int64                            `json:"plan_id"`
	BaseVersionID     int64                            `json:"base_version_id"`
	RuntimeSessionID  int64                            `json:"runtime_session_id"`
	ExecutionRealm    string                           `json:"execution_realm"`
	Status            string                           `json:"status"`
	Policy            LiveContentPolicy                `json:"policy"`
	PolicyFingerprint string                           `json:"policy_fingerprint"`
	SourceFingerprint string                           `json:"source_fingerprint"`
	ScheduledAt       time.Time                        `json:"scheduled_at"`
	PublishAfter      time.Time                        `json:"publish_after"`
	LeaseToken        string                           `json:"-"`
	LeaseUntil        *time.Time                       `json:"lease_until,omitempty"`
	Attempts          int                              `json:"attempts"`
	ResultVersionID   int64                            `json:"result_version_id,omitempty"`
	Draft             *CreateLiveAgentPlanVersionInput `json:"draft,omitempty"`
	GeneratedAssets   []int64                          `json:"generated_assets,omitempty"`
}
