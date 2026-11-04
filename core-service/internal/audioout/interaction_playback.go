package audioout

import "time"

type InteractionPreflightRequest struct {
	TenantID   int64     `json:"tenant_id"`
	RoomID     int64     `json:"room_id"`
	PlanID     int64     `json:"plan_id"`
	Epoch      string    `json:"epoch"`
	DecisionID string    `json:"decision_id"`
	Stage      string    `json:"stage"`
	DurationMS int       `json:"duration_ms"`
	Until      time.Time `json:"until"`
}
type InteractionPreflightResult struct {
	Allowed      bool      `json:"allowed"`
	Reason       string    `json:"reason"`
	CheckedAt    time.Time `json:"checked_at"`
	RetryAfterMS int       `json:"retry_after_ms"`
	ProgramID    string    `json:"program_id,omitempty"`
	TrackID      string    `json:"track_id,omitempty"`
	CutMS        int       `json:"cut_ms,omitempty"`
	RemainingMS  int       `json:"remaining_ms,omitempty"`
}

// InteractionPlaybackReceipt contains only PCM producer evidence. Task creation
// and elapsed wall time cannot set StartedAt or a COMPLETED status.
type InteractionPlaybackReceipt struct {
	RoomID        int64         `json:"room_id"`
	ProgramID     string        `json:"program_id"`
	DecisionID    string        `json:"decision_id"`
	TaskID        string        `json:"task_id"`
	SessionID     string        `json:"session_id"`
	DurationMS    int           `json:"duration_ms"`
	PreparedUntil time.Time     `json:"prepared_until"`
	StartedAt     time.Time     `json:"started_at"`
	Event         PlaybackEvent `json:"event"`
}
