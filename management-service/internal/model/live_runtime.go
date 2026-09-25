package model

import "time"

type LiveDevice struct {
	ID               int64      `json:"id"`
	SN               string     `json:"sn"`
	SKUCode          string     `json:"sku_code"`
	LifecycleStatus  string     `json:"lifecycle_status"`
	TenantID         int64      `json:"tenant_id"`
	RoomID           *int64     `json:"room_id,omitempty"`
	BindingRole      string     `json:"binding_role"`
	ConnectionStatus string     `json:"connection_status"`
	WorkStatus       string     `json:"work_status"`
	StopReason       string     `json:"stop_reason"`
	LastHeartbeatAt  *time.Time `json:"last_heartbeat_at,omitempty"`
}

type LiveRuntimeSession struct {
	ID                 int64      `json:"id"`
	ExternalID         string     `json:"external_id"`
	TenantID           int64      `json:"tenant_id"`
	RoomID             int64      `json:"room_id"`
	DeviceID           *int64     `json:"device_id,omitempty"`
	DeviceSN           string     `json:"device_sn,omitempty"`
	Status             string     `json:"status"`
	StopReason         string     `json:"stop_reason"`
	StartedByUserID    *int64     `json:"started_by_user_id,omitempty"`
	StoppedByUserID    *int64     `json:"stopped_by_user_id,omitempty"`
	StartedAt          time.Time  `json:"started_at"`
	LastBilledAt       time.Time  `json:"last_billed_at"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
	TotalBilledSeconds uint64     `json:"total_billed_seconds"`
	Version            uint64     `json:"version"`
}

type LiveRuntimeEvent struct {
	ID          int64          `json:"id"`
	TenantID    int64          `json:"tenant_id"`
	RoomID      *int64         `json:"room_id,omitempty"`
	DeviceID    *int64         `json:"device_id,omitempty"`
	SessionID   *int64         `json:"session_id,omitempty"`
	ActorType   string         `json:"actor_type"`
	ActorUserID *int64         `json:"actor_user_id,omitempty"`
	EventCode   string         `json:"event_code"`
	Title       string         `json:"title"`
	Detail      map[string]any `json:"detail,omitempty"`
	OccurredAt  time.Time      `json:"occurred_at"`
}

type LiveQuotaSourceSummary struct {
	SourceType       string     `json:"source_type"`
	SourceLabel      string     `json:"source_label"`
	AssetNo          string     `json:"asset_no,omitempty"`
	RemainingSeconds uint64     `json:"remaining_seconds"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
}

type LiveTimeCardSummary struct {
	AssetNo              string     `json:"asset_no"`
	ProductName          string     `json:"product_name"`
	Status               string     `json:"status"`
	OriginalSeconds      uint64     `json:"original_seconds"`
	RemainingSeconds     uint64     `json:"remaining_seconds"`
	ActivationDeadlineAt *time.Time `json:"activation_deadline_at,omitempty"`
	ActivatedAt          *time.Time `json:"activated_at,omitempty"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
}

type LiveQuotaSummary struct {
	ActiveSeconds          uint64                  `json:"active_seconds"`
	ReserveTimeCardSeconds uint64                  `json:"reserve_time_card_seconds"`
	ReserveTimeCardCount   uint32                  `json:"reserve_time_card_count"`
	Current                *LiveQuotaSourceSummary `json:"current,omitempty"`
	TimeCards              []LiveTimeCardSummary   `json:"time_cards"`
}

type LiveRuntimeSnapshot struct {
	Session                *LiveRuntimeSession     `json:"session,omitempty"`
	AgentState             string                  `json:"agent_state"`
	AgentMode              string                  `json:"agent_mode"`
	AgentWorkingSeconds    uint64                  `json:"agent_working_seconds"`
	QuotaRemainingSeconds  uint64                  `json:"quota_remaining_seconds"`
	ReserveTimeCardSeconds uint64                  `json:"reserve_time_card_seconds"`
	ReserveTimeCardCount   uint32                  `json:"reserve_time_card_count"`
	CurrentQuota           *LiveQuotaSourceSummary `json:"current_quota,omitempty"`
	TimeCards              []LiveTimeCardSummary   `json:"time_cards"`
	RoomLive               bool                    `json:"room_live"`
	Device                 *LiveDevice             `json:"device,omitempty"`
}

type LiveAgentSettings struct {
	TenantID         int64     `json:"tenant_id"`
	DisplayName      string    `json:"display_name"`
	RoleName         string    `json:"role_name"`
	SelfIntroduction string    `json:"self_introduction"`
	Mission          string    `json:"mission"`
	Greeting         string    `json:"greeting"`
	UpdatedByUserID  *int64    `json:"updated_by_user_id,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type LiveAgentSettingsInput struct {
	DisplayName      string `json:"display_name"`
	RoleName         string `json:"role_name"`
	SelfIntroduction string `json:"self_introduction"`
	Mission          string `json:"mission"`
	Greeting         string `json:"greeting"`
}

type RecordLiveRuntimeEventInput struct {
	EventCode string         `json:"event_code"`
	Title     string         `json:"title,omitempty"`
	Detail    map[string]any `json:"detail,omitempty"`
}

type BindLiveDeviceInput struct {
	RoomID      int64  `json:"room_id"`
	BindingRole string `json:"binding_role"`
}

type DeviceHeartbeatInput struct {
	RoomID   *int64         `json:"room_id,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type DeviceControlInput struct {
	RoomID int64  `json:"room_id"`
	Action string `json:"action"`
}

type StartLiveRuntimeInput struct {
	DeviceID *int64 `json:"device_id,omitempty"`
}

type StopLiveRuntimeInput struct {
	Reason string `json:"reason,omitempty"`
}
