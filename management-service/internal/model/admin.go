package model

import "time"

type AdminCustomer struct {
	UserID             int64     `json:"user_id"`
	TenantID           int64     `json:"tenant_id"`
	Username           string    `json:"username"`
	DisplayName        string    `json:"display_name"`
	Phone              string    `json:"phone"`
	Email              string    `json:"email"`
	Status             string    `json:"status"`
	SourceType         string    `json:"source_type"`
	ParentOrgID        int64     `json:"parent_org_id"`
	ParentOrgName      string    `json:"parent_org_name"`
	ParentOrgType      string    `json:"parent_org_type"`
	InviterUserID      int64     `json:"inviter_user_id,omitempty"`
	InviterUsername    string    `json:"inviter_username,omitempty"`
	InviterDisplayName string    `json:"inviter_display_name,omitempty"`
	SalesStaffID       int64     `json:"sales_staff_id,omitempty"`
	SalesUserID        int64     `json:"sales_user_id,omitempty"`
	SalesEmployeeCode  string    `json:"sales_employee_code,omitempty"`
	SalesUsername      string    `json:"sales_username,omitempty"`
	SalesDisplayName   string    `json:"sales_display_name,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
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
