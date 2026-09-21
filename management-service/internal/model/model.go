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
	ID           int64
	TenantID     *int64
	Username     string
	PasswordHash string
	DisplayName  string
	Role         string
	Status       string
	CreatedAt    time.Time
}
type Actor struct {
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	TenantID    *int64 `json:"tenant_id,omitempty"`
	DisplayName string `json:"display_name"`
}

func (a Actor) IsPlatformAdmin() bool {
	return a.Role == "platform_admin"
}

type CreateRoomRequest struct {
	TenantID       int64  `json:"tenant_id"`
	Platform       string `json:"platform"`
	ExternalRoomID string `json:"external_room_id"`
	SourceURL      string `json:"source_url,omitempty"`
	Name           string `json:"name"`
	CollectorMode  string `json:"collector_mode"`
}
