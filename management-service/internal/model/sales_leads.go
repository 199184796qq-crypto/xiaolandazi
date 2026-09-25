package model

import "time"

type SalesLead struct {
	ID                int64      `json:"id"`
	LeadNo            string     `json:"lead_no"`
	OwnerSalesStaffID int64      `json:"owner_sales_staff_id"`
	BusinessName      string     `json:"business_name"`
	ContactName       string     `json:"contact_name"`
	Phone             string     `json:"phone"`
	Wechat            string     `json:"wechat"`
	Email             string     `json:"email"`
	IndustryName      string     `json:"industry_name"`
	Province          string     `json:"province"`
	City              string     `json:"city"`
	District          string     `json:"district"`
	Address           string     `json:"address"`
	SourceType        string     `json:"source_type"`
	Stage             string     `json:"stage"`
	Status            string     `json:"status"`
	PlannedVisitAt    *time.Time `json:"planned_visit_at,omitempty"`
	NextFollowupAt    *time.Time `json:"next_followup_at,omitempty"`
	LatestActivityAt  *time.Time `json:"latest_activity_at,omitempty"`
	LostReason        string     `json:"lost_reason"`
	ConvertedTenantID *int64     `json:"converted_tenant_id,omitempty"`
	ConvertedUserID   *int64     `json:"converted_user_id,omitempty"`
	ConvertedAt       *time.Time `json:"converted_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type SalesLeadSummary struct {
	TotalCount       int64 `json:"total_count"`
	OpenCount        int64 `json:"open_count"`
	VisitDueCount    int64 `json:"visit_due_count"`
	FollowupDueCount int64 `json:"followup_due_count"`
	WonCount         int64 `json:"won_count"`
	LostCount        int64 `json:"lost_count"`
}

type SalesLeadInput struct {
	BusinessName   string     `json:"business_name"`
	ContactName    string     `json:"contact_name"`
	Phone          string     `json:"phone"`
	Wechat         string     `json:"wechat"`
	Email          string     `json:"email"`
	IndustryName   string     `json:"industry_name"`
	Province       string     `json:"province"`
	City           string     `json:"city"`
	District       string     `json:"district"`
	Address        string     `json:"address"`
	SourceType     string     `json:"source_type"`
	Stage          string     `json:"stage"`
	PlannedVisitAt *time.Time `json:"planned_visit_at,omitempty"`
	NextFollowupAt *time.Time `json:"next_followup_at,omitempty"`
}

type SalesLeadActivity struct {
	ID               int64      `json:"id"`
	LeadID           int64      `json:"lead_id"`
	SalesStaffID     int64      `json:"sales_staff_id"`
	SalesDisplayName string     `json:"sales_display_name"`
	ActivityType     string     `json:"activity_type"`
	Outcome          string     `json:"outcome"`
	Content          string     `json:"content"`
	OccurredAt       time.Time  `json:"occurred_at"`
	NextFollowupAt   *time.Time `json:"next_followup_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type SalesLeadActivityInput struct {
	ActivityType   string     `json:"activity_type"`
	Outcome        string     `json:"outcome"`
	Content        string     `json:"content"`
	OccurredAt     *time.Time `json:"occurred_at,omitempty"`
	NextFollowupAt *time.Time `json:"next_followup_at,omitempty"`
	Stage          string     `json:"stage"`
}

type ConvertSalesLeadInput struct {
	Username       string `json:"username"`
	DisplayName    string `json:"display_name"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	Province       string `json:"province"`
	City           string `json:"city"`
	District       string `json:"district"`
	Address        string `json:"address"`
	DeliveryMethod string `json:"delivery_method"`
	HandoffSummary string `json:"handoff_summary"`
}

type CustomerHandoff struct {
	ID                int64      `json:"id"`
	HandoffNo         string     `json:"handoff_no"`
	LeadID            *int64     `json:"lead_id,omitempty"`
	TenantID          int64      `json:"tenant_id"`
	CustomerUserID    int64      `json:"customer_user_id"`
	CustomerUsername  string     `json:"customer_username"`
	CustomerName      string     `json:"customer_name"`
	CustomerPhone     string     `json:"customer_phone"`
	SalesStaffID      int64      `json:"sales_staff_id"`
	SalesDisplayName  string     `json:"sales_display_name"`
	TargetGroupCode   string     `json:"target_group_code"`
	Status            string     `json:"status"`
	Summary           string     `json:"summary"`
	AcceptedByUserID  *int64     `json:"accepted_by_user_id,omitempty"`
	AcceptedByName    string     `json:"accepted_by_name"`
	AcceptedAt        *time.Time `json:"accepted_at,omitempty"`
	CompletedByUserID *int64     `json:"completed_by_user_id,omitempty"`
	CompletedByName   string     `json:"completed_by_name"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type SalesPortfolioHandoverPreview struct {
	FromSalesStaffID int64  `json:"from_sales_staff_id"`
	FromDisplayName  string `json:"from_display_name"`
	CustomerCount    int64  `json:"customer_count"`
	OpenLeadCount    int64  `json:"open_lead_count"`
}

type SalesPortfolioHandover struct {
	ID               int64     `json:"id"`
	HandoverNo       string    `json:"handover_no"`
	FromSalesStaffID int64     `json:"from_sales_staff_id"`
	FromDisplayName  string    `json:"from_display_name"`
	ToSalesStaffID   int64     `json:"to_sales_staff_id"`
	ToDisplayName    string    `json:"to_display_name"`
	CustomerCount    int64     `json:"customer_count"`
	LeadCount        int64     `json:"lead_count"`
	Reason           string    `json:"reason"`
	Status           string    `json:"status"`
	CreatedByUserID  int64     `json:"created_by_user_id"`
	CreatedAt        time.Time `json:"created_at"`
}
