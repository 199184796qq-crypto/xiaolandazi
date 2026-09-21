package model

import "time"

type AdminCustomer struct {
	UserID      int64     `json:"user_id"`
	TenantID    int64     `json:"tenant_id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type AdminAuditLog struct {
	ID             string    `json:"id"`
	OccurredAt     time.Time `json:"occurred_at"`
	ActorUserID    int64     `json:"actor_user_id"`
	ActorUsername  string    `json:"actor_username"`
	Action         string    `json:"action"`
	TargetUserID   int64     `json:"target_user_id,omitempty"`
	TargetUsername string    `json:"target_username,omitempty"`
	TargetTenantID int64     `json:"target_tenant_id,omitempty"`
	HTTPMethod     string    `json:"http_method,omitempty"`
	Path           string    `json:"path,omitempty"`
	ClientIP       string    `json:"client_ip,omitempty"`
	Result         string    `json:"result"`
}
