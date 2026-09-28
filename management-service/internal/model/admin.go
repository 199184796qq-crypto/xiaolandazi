package model

import "time"

type AdminCustomer struct {
	UserID                    int64      `json:"user_id"`
	TenantID                  int64      `json:"tenant_id"`
	Username                  string     `json:"username"`
	DisplayName               string     `json:"display_name"`
	Phone                     string     `json:"phone"`
	Email                     string     `json:"email"`
	Status                    string     `json:"status"`
	CooperationStatus         string     `json:"cooperation_status"`
	CooperationNote           string     `json:"cooperation_note"`
	CooperationMarkedAt       *time.Time `json:"cooperation_marked_at,omitempty"`
	CooperationMarkedByUserID int64      `json:"cooperation_marked_by_user_id,omitempty"`
	LastRechargeAt            *time.Time `json:"last_recharge_at,omitempty"`
	RechargeDormant90Days     bool       `json:"recharge_dormant_90_days"`
	SourceType                string     `json:"source_type"`
	ParentOrgID               int64      `json:"parent_org_id"`
	ParentOrgName             string     `json:"parent_org_name"`
	ParentOrgType             string     `json:"parent_org_type"`
	InviterUserID             int64      `json:"inviter_user_id,omitempty"`
	InviterUsername           string     `json:"inviter_username,omitempty"`
	InviterDisplayName        string     `json:"inviter_display_name,omitempty"`
	SalesStaffID              int64      `json:"sales_staff_id,omitempty"`
	SalesUserID               int64      `json:"sales_user_id,omitempty"`
	SalesEmployeeCode         string     `json:"sales_employee_code,omitempty"`
	SalesUsername             string     `json:"sales_username,omitempty"`
	SalesDisplayName          string     `json:"sales_display_name,omitempty"`
	IndustryCode              string     `json:"industry_code"`
	IndustryName              string     `json:"industry_name"`
	CreatedAt                 time.Time  `json:"created_at"`
}
type AgentSummary struct {
	OrganizationID int64     `json:"organization_id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	AdminUserID    int64     `json:"admin_user_id"`
	Username       string    `json:"username"`
	DisplayName    string    `json:"display_name"`
	Phone          string    `json:"phone"`
	Email          string    `json:"email"`
	UserStatus     string    `json:"user_status"`
	CustomerCount  int64     `json:"customer_count"`
}
type AdminAuditLog struct {
	ID               string    `json:"id"`
	OccurredAt       time.Time `json:"occurred_at"`
	ActorUserID      int64     `json:"actor_user_id"`
	ActorUsername    string    `json:"actor_username"`
	ActorRole        string    `json:"actor_role,omitempty"`
	ActorType        string    `json:"actor_type,omitempty"`
	Source           string    `json:"source,omitempty"`
	Action           string    `json:"action"`
	TargetUserID     int64     `json:"target_user_id,omitempty"`
	TargetUsername   string    `json:"target_username,omitempty"`
	TargetTenantID   int64     `json:"target_tenant_id,omitempty"`
	TargetRoomID     int64     `json:"target_room_id,omitempty"`
	ObjectType       string    `json:"object_type,omitempty"`
	ObjectID         string    `json:"object_id,omitempty"`
	ObjectName       string    `json:"object_name,omitempty"`
	Reason           string    `json:"reason,omitempty"`
	BeforeState      string    `json:"before_state,omitempty"`
	AfterState       string    `json:"after_state,omitempty"`
	RuntimeSessionID int64     `json:"runtime_session_id,omitempty"`
	CoreBootID       string    `json:"core_boot_id,omitempty"`
	RequestID        string    `json:"request_id,omitempty"`
	DetailJSON       string    `json:"detail_json,omitempty"`
	HTTPMethod       string    `json:"http_method,omitempty"`
	Path             string    `json:"path,omitempty"`
	ClientIP         string    `json:"client_ip,omitempty"`
	Result           string    `json:"result"`
}
