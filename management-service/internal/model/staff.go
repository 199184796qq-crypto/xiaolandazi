package model

import "time"

type StaffGroupSummary struct {
	ID            int64     `json:"id"`
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Status        string    `json:"status"`
	SortOrder     int       `json:"sort_order"`
	SystemManaged bool      `json:"system_managed"`
	MemberCount   int64     `json:"member_count"`
	ManagerCount  int64     `json:"manager_count"`
	CreatedAt     time.Time `json:"created_at"`
}

type StaffPermissionSummary struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	Module      string `json:"module"`
	Action      string `json:"action"`
	Description string `json:"description"`
}

type StaffRoleSummary struct {
	ID               int64                    `json:"id"`
	GroupID          int64                    `json:"group_id"`
	GroupCode        string                   `json:"group_code"`
	GroupName        string                   `json:"group_name"`
	Code             string                   `json:"code"`
	Name             string                   `json:"name"`
	Description      string                   `json:"description"`
	IsGroupManager   bool                     `json:"is_group_manager"`
	DefaultScopeType string                   `json:"default_scope_type"`
	Status           string                   `json:"status"`
	Permissions      []StaffPermissionSummary `json:"permissions"`
	PermissionCodes  []string                 `json:"permission_codes"`
	CreatedAt        time.Time                `json:"created_at"`
}

type StaffEmployeeRoleSummary struct {
	RoleID         int64  `json:"role_id"`
	GroupID        int64  `json:"group_id"`
	GroupCode      string `json:"group_code"`
	GroupName      string `json:"group_name"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	ScopeType      string `json:"scope_type"`
	IsGroupManager bool   `json:"is_group_manager"`
}

type StaffEmployeeGroupSummary struct {
	GroupID   int64  `json:"group_id"`
	GroupCode string `json:"group_code"`
	GroupName string `json:"group_name"`
	IsPrimary bool   `json:"is_primary"`
}

type StaffEmployeeSummary struct {
	ID               int64                       `json:"id"`
	UserID           int64                       `json:"user_id"`
	EmployeeNo       string                      `json:"employee_no"`
	Username         string                      `json:"username"`
	DisplayName      string                      `json:"display_name"`
	Phone            string                      `json:"phone"`
	Email            string                      `json:"email"`
	Province         string                      `json:"province"`
	City             string                      `json:"city"`
	District         string                      `json:"district"`
	PrimaryGroupID   int64                       `json:"primary_group_id"`
	PrimaryGroupCode string                      `json:"primary_group_code"`
	PrimaryGroupName string                      `json:"primary_group_name"`
	EmploymentStatus string                      `json:"employment_status"`
	UserStatus       string                      `json:"user_status"`
	Roles            []StaffEmployeeRoleSummary  `json:"roles"`
	Groups           []StaffEmployeeGroupSummary `json:"groups"`
	CreatedAt        time.Time                   `json:"created_at"`
}

type StaffAccessContext struct {
	IsSuperAdmin       bool               `json:"is_super_admin"`
	EmployeeID         int64              `json:"employee_id,omitempty"`
	PrimaryGroupID     int64              `json:"primary_group_id,omitempty"`
	PrimaryGroupCode   string             `json:"primary_group_code,omitempty"`
	PrimaryGroupName   string             `json:"primary_group_name,omitempty"`
	RoleCodes          []string           `json:"role_codes"`
	Permissions        []string           `json:"permissions"`
	PermissionScopes   map[string]string  `json:"permission_scopes"`
	PermissionGroupIDs map[string][]int64 `json:"permission_group_ids"`
	GroupIDs           []int64            `json:"group_ids"`
	ManagedGroupIDs    []int64            `json:"managed_group_ids"`
}

type StaffApprovalPolicySummary struct {
	ID               int64   `json:"id"`
	Code             string  `json:"code"`
	Name             string  `json:"name"`
	OperationCode    string  `json:"operation_code"`
	Mode             string  `json:"mode"`
	ThresholdAmount  float64 `json:"threshold_amount"`
	ApproverRoleCode string  `json:"approver_role_code"`
	Status           string  `json:"status"`
}
