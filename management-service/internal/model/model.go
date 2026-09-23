package model

import "time"

type Tenant struct {
	ID        int64     `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	ID                 int64
	TenantID           *int64
	Username           string
	PasswordHash       string
	DisplayName        string
	AvatarURL          string
	Role               string
	MustChangePassword bool
	Phone              string
	Province           string
	City               string
	District           string
	Status             string
	CreatedAt          time.Time
}

type Actor struct {
	UserID             int64  `json:"user_id"`
	Username           string `json:"username"`
	Role               string `json:"role"`
	MustChangePassword bool   `json:"must_change_password"`
	Phone              string `json:"phone"`
	Province           string `json:"province"`
	City               string `json:"city"`
	District           string `json:"district"`
	TenantID           *int64 `json:"tenant_id,omitempty"`
	DisplayName        string `json:"display_name"`
	AvatarURL          string `json:"avatar_url"`
}

type AuthSessionSummary struct {
	ID         int64     `json:"id"`
	ClientIP   string    `json:"client_ip"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

func (a Actor) IsPlatformAdmin() bool {
	return a.Role == "platform_admin"
}

func (a Actor) IsAgentAdmin() bool {
	return a.Role == "agent_admin"
}
func (a Actor) IsSalesStaff() bool {
	return a.Role == "sales_staff"
}
func (a Actor) IsInternalStaff() bool {
	return a.Role == "platform_admin" ||
		a.Role == "staff" ||
		a.Role == "sales_staff"
}

type CreateRoomRequest struct {
	TenantID       int64  `json:"tenant_id"`
	Platform       string `json:"platform"`
	ExternalRoomID string `json:"external_room_id"`
	SourceURL      string `json:"source_url,omitempty"`
	Name           string `json:"name"`
	CollectorMode  string `json:"collector_mode"`
}
