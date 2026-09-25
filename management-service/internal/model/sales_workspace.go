package model

import "time"

type SalesFollowup struct {
	ID               int64      `json:"id"`
	SalesStaffID     int64      `json:"sales_staff_id"`
	SalesDisplayName string     `json:"sales_display_name"`
	TenantID         int64      `json:"tenant_id"`
	CustomerUserID   int64      `json:"customer_user_id"`
	CustomerUsername string     `json:"customer_username"`
	CustomerName     string     `json:"customer_name"`
	CustomerPhone    string     `json:"customer_phone"`
	FollowupType     string     `json:"followup_type"`
	Content          string     `json:"content"`
	NextFollowupAt   *time.Time `json:"next_followup_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type SalesFollowupInput struct {
	TenantID       int64      `json:"tenant_id"`
	FollowupType   string     `json:"followup_type"`
	Content        string     `json:"content"`
	NextFollowupAt *time.Time `json:"next_followup_at,omitempty"`
}

type SalesFollowupSummary struct {
	TotalCount   int64 `json:"total_count"`
	DueCount     int64 `json:"due_count"`
	OverdueCount int64 `json:"overdue_count"`
}

type SalesCatalogMembership struct {
	ID                 int64  `json:"id"`
	Code               string `json:"code"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	PriceCents         uint64 `json:"price_cents"`
	IncludedSeconds    uint64 `json:"included_seconds"`
	BillingPeriodUnit  string `json:"billing_period_unit"`
	BillingPeriodCount uint32 `json:"billing_period_count"`
	AllowAutoRenew     bool   `json:"allow_auto_renew"`
	VersionNo          uint32 `json:"version_no"`
}

type SalesCatalogTimeCard struct {
	ID                     int64  `json:"id"`
	Code                   string `json:"code"`
	Name                   string `json:"name"`
	Description            string `json:"description"`
	PriceCents             uint64 `json:"price_cents"`
	DurationSeconds        uint64 `json:"duration_seconds"`
	ValidityDays           uint32 `json:"validity_days"`
	ActivationMode         string `json:"activation_mode"`
	ActivationDeadlineDays uint32 `json:"activation_deadline_days"`
	VersionNo              uint32 `json:"version_no"`
}

type SalesCatalogDevice struct {
	ID             int64  `json:"id"`
	Code           string `json:"code"`
	SKUCode        string `json:"sku_code"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	ImageURL       string `json:"image_url"`
	UnitLabel      string `json:"unit_label"`
	ListPriceCents uint64 `json:"list_price_cents"`
	SalePriceCents uint64 `json:"sale_price_cents"`
	AvailableStock int    `json:"available_stock"`
	VersionNo      uint32 `json:"version_no"`
}

type SalesCatalog struct {
	Memberships []SalesCatalogMembership `json:"memberships"`
	TimeCards   []SalesCatalogTimeCard   `json:"time_cards"`
	Devices     []SalesCatalogDevice     `json:"devices"`
}
