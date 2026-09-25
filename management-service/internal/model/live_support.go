package model

import "time"

const (
	LiveSupportCapabilityL3Policy       = "l3_policy"
	LiveSupportCapabilityAnchorTraining = "anchor_training"
	LiveSupportCapabilityVoiceClone     = "voice_clone"
)

type LiveSupportStaff struct {
	UserID              int64    `json:"user_id"`
	Username            string   `json:"username"`
	DisplayName         string   `json:"display_name"`
	AvatarURL           string   `json:"avatar_url,omitempty"`
	SpecialtyIndustries []string `json:"specialty_industries"`
	AllowedCapabilities []string `json:"allowed_capabilities"`
	L3RestrictionReason string   `json:"l3_restriction_reason,omitempty"`
}

type LiveSupportAuthorization struct {
	ID               int64      `json:"id"`
	TenantID         int64      `json:"tenant_id"`
	RoomID           int64      `json:"room_id"`
	StaffUserID      int64      `json:"staff_user_id"`
	StaffUsername    string     `json:"staff_username,omitempty"`
	StaffDisplayName string     `json:"staff_display_name,omitempty"`
	Capability       string     `json:"capability"`
	Status           string     `json:"status"`
	GrantedByUserID  int64      `json:"granted_by_user_id"`
	GrantedAt        time.Time  `json:"granted_at"`
	RevokedByUserID  *int64     `json:"revoked_by_user_id,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type LiveSupportAuthorizationInput struct {
	Capabilities []string `json:"capabilities"`
}

type LiveSupportRequest struct {
	ID                int64      `json:"id"`
	TenantID          int64      `json:"tenant_id"`
	RoomID            int64      `json:"room_id"`
	RoomName          string     `json:"room_name,omitempty"`
	StaffUserID       int64      `json:"staff_user_id"`
	StaffUsername     string     `json:"staff_username,omitempty"`
	StaffDisplayName  string     `json:"staff_display_name,omitempty"`
	RequestedByUserID int64      `json:"requested_by_user_id"`
	Capabilities      []string   `json:"capabilities"`
	Status            string     `json:"status"`
	DecidedByUserID   *int64     `json:"decided_by_user_id,omitempty"`
	DecisionNote      string     `json:"decision_note"`
	RequestedAt       time.Time  `json:"requested_at"`
	DecidedAt         *time.Time `json:"decided_at,omitempty"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type LiveSupportRequestInput struct {
	Capabilities []string `json:"capabilities"`
}

type LiveSupportRequestDecisionInput struct {
	Note string `json:"note"`
}

type LiveSupportRoomGrant struct {
	TenantID     int64    `json:"tenant_id"`
	RoomID       int64    `json:"room_id"`
	Capabilities []string `json:"capabilities"`
}

type LiveSupportTrainingInput struct {
	Text     string  `json:"text"`
	AssetIDs []int64 `json:"asset_ids,omitempty"`
}
