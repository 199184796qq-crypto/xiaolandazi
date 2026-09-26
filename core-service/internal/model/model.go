package model

import (
	"encoding/json"
	"time"
)

type Room struct {
	ID               int64      `json:"id"`
	TenantID         int64      `json:"tenant_id"`
	Platform         string     `json:"platform"`
	ExternalRoomID   string     `json:"external_room_id"`
	SourceURL        string     `json:"source_url,omitempty"`
	Name             string     `json:"name"`
	Status           string     `json:"status"`
	CollectorMode    string     `json:"collector_mode"`
	MonitorEnabled   bool       `json:"monitor_enabled"`
	MonitorStartedAt *time.Time `json:"monitor_started_at,omitempty"`
	DeviceOnline     bool       `json:"device_online"`
	OnlineCount      int        `json:"online_count"`
	LastEventAt      *time.Time `json:"last_event_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type RoomEvent struct {
	ID         int64           `json:"id"`
	TenantID   int64           `json:"tenant_id"`
	RoomID     int64           `json:"room_id"`
	EventType  string          `json:"event_type"`
	UserID     string          `json:"user_id,omitempty"`
	Nickname   string          `json:"nickname,omitempty"`
	Content    string          `json:"content,omitempty"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type CreateRoomInput struct {
	TenantID       int64  `json:"tenant_id"`
	Platform       string `json:"platform"`
	ExternalRoomID string `json:"external_room_id"`
	SourceURL      string `json:"source_url,omitempty"`
	Name           string `json:"name"`
	CollectorMode  string `json:"collector_mode"`
}

type CreateEventInput struct {
	EventType  string          `json:"event_type"`
	UserID     string          `json:"user_id,omitempty"`
	Nickname   string          `json:"nickname,omitempty"`
	Content    string          `json:"content,omitempty"`
	OccurredAt time.Time       `json:"occurred_at,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type UpdateRoomRuntimeInput struct {
	MonitorEnabled *bool `json:"monitor_enabled,omitempty"`
	DeviceOnline   *bool `json:"device_online,omitempty"`
}
