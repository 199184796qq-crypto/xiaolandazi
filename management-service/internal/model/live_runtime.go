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

type GeneratedSpeechHistoryInput struct {
	TenantID          int64
	RoomID            int64
	RuntimeSessionID  int64
	RuntimeExternalID string
	DecisionID        string
	SourceType        string
	QuestionText      string
	GeneratedText     string
}

type GeneratedSpeechHistoryItem struct {
	ID                     int64     `json:"id"`
	TenantID               int64     `json:"tenant_id"`
	RoomID                 int64     `json:"room_id"`
	RuntimeSessionID       int64     `json:"runtime_session_id"`
	RuntimeExternalID      string    `json:"runtime_external_id"`
	DecisionID             string    `json:"decision_id"`
	SourceType             string    `json:"source_type"`
	QuestionText           string    `json:"question_text,omitempty"`
	GeneratedText          string    `json:"generated_text"`
	CorrectionCount        uint64    `json:"correction_count"`
	AdoptedCorrectionCount uint64    `json:"adopted_correction_count"`
	EditingCorrectionCount uint64    `json:"editing_correction_count"`
	CorrectionStatus       string    `json:"correction_status"`
	CreatedAt              time.Time `json:"created_at"`
}

type GeneratedSpeechHistoryPage struct {
	Items            []GeneratedSpeechHistoryItem `json:"items"`
	Page             int                          `json:"page"`
	PageSize         int                          `json:"page_size"`
	Total            uint64                       `json:"total"`
	RuntimeSessionID int64                        `json:"runtime_session_id"`
}

type LiveQuotaSourceSummary struct {
	SourceType       string     `json:"source_type"`
	SourceID         *int64     `json:"source_id,omitempty"`
	SourceLabel      string     `json:"source_label"`
	AssetNo          string     `json:"asset_no,omitempty"`
	RemainingSeconds uint64     `json:"remaining_seconds"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
}

type LiveTimeCardSummary struct {
	ID                   int64      `json:"id"`
	AssetNo              string     `json:"asset_no"`
	ProductName          string     `json:"product_name"`
	Status               string     `json:"status"`
	OriginalSeconds      uint64     `json:"original_seconds"`
	RemainingSeconds     uint64     `json:"remaining_seconds"`
	ActivationMode       string     `json:"activation_mode"`
	ValidityDays         uint32     `json:"validity_days"`
	ActivationDeadlineAt *time.Time `json:"activation_deadline_at,omitempty"`
	ActivatedAt          *time.Time `json:"activated_at,omitempty"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	PurchasedAt          time.Time  `json:"purchased_at"`
}

type LiveTimeCardPage struct {
	Items    []LiveTimeCardSummary `json:"items"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
	Total    uint64                `json:"total"`
}

type LiveBillingRoomSummary struct {
	RoomID        int64     `json:"room_id"`
	RoomName      string    `json:"room_name"`
	SessionID     int64     `json:"session_id"`
	BilledSeconds uint64    `json:"billed_seconds"`
	StartedAt     time.Time `json:"started_at"`
}

type LiveQuotaSummary struct {
	ActiveSeconds          uint64                   `json:"active_seconds"`
	ActiveTimeCardSeconds  uint64                   `json:"active_time_card_seconds"`
	ReserveTimeCardSeconds uint64                   `json:"reserve_time_card_seconds"`
	ReserveTimeCardCount   uint32                   `json:"reserve_time_card_count"`
	Current                *LiveQuotaSourceSummary  `json:"current,omitempty"`
	Sources                []LiveQuotaSourceSummary `json:"sources"`
	TimeCards              []LiveTimeCardSummary    `json:"time_cards"`
	ActiveBillingRooms     []LiveBillingRoomSummary `json:"active_billing_rooms"`
}

type LiveRuntimeSnapshot struct {
	Session                *LiveRuntimeSession     `json:"session,omitempty"`
	AgentState             string                  `json:"agent_state"`
	AgentMode              string                  `json:"agent_mode"`
	AgentPlanID            int64                   `json:"agent_plan_id,omitempty"`
	AgentPlanName          string                  `json:"agent_plan_name,omitempty"`
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

type RoomInteractionPreferences struct {
	TenantID             int64     `json:"tenant_id"`
	RoomID               int64     `json:"room_id"`
	OverallInteraction   string    `json:"overall_interaction"`
	QuestionPreference   string    `json:"question_preference"`
	WelcomePreference    string    `json:"welcome_preference"`
	EngagementPreference string    `json:"engagement_preference"`
	ChatPreference       string    `json:"chat_preference"`
	ConversionPreference string    `json:"conversion_preference"`
	AutoHeat             bool      `json:"auto_heat"`
	UpdatedByUserID      *int64    `json:"updated_by_user_id,omitempty"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type RoomInteractionPreferencesInput struct {
	OverallInteraction   string `json:"overall_interaction"`
	QuestionPreference   string `json:"question_preference"`
	WelcomePreference    string `json:"welcome_preference"`
	EngagementPreference string `json:"engagement_preference"`
	ChatPreference       string `json:"chat_preference"`
	ConversionPreference string `json:"conversion_preference"`
	AutoHeat             bool   `json:"auto_heat"`
}

type RoomHumanBehaviorProfile struct {
	TenantID        int64      `json:"tenant_id"`
	RoomID          int64      `json:"room_id"`
	TraitText       string     `json:"trait_text"`
	StateText       string     `json:"state_text"`
	StateExpiresAt  *time.Time `json:"state_expires_at,omitempty"`
	UpdatedByUserID *int64     `json:"updated_by_user_id,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type RoomHumanBehaviorProfileInput struct {
	TraitText      string     `json:"trait_text"`
	StateText      string     `json:"state_text"`
	StateExpiresAt *time.Time `json:"state_expires_at,omitempty"`
}

type RoomAddressingPreferences struct {
	TenantID         int64     `json:"tenant_id"`
	RoomID           int64     `json:"room_id"`
	NamingPreference string    `json:"naming_preference"`
	PreferredTerms   []string  `json:"preferred_terms"`
	BlockedTerms     []string  `json:"blocked_terms"`
	UpdatedByUserID  *int64    `json:"updated_by_user_id,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type RoomAddressingPreferencesInput struct {
	NamingPreference string   `json:"naming_preference"`
	PreferredTerms   []string `json:"preferred_terms"`
	BlockedTerms     []string `json:"blocked_terms"`
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
