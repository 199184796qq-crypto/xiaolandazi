package model

import "time"

type InviteCodeSummary struct {
	ID            int64      `json:"id"`
	Code          string     `json:"code"`
	OwnerUserID   int64      `json:"owner_user_id"`
	OwnerTenantID *int64     `json:"owner_tenant_id,omitempty"`
	OwnerRole     string     `json:"owner_role"`
	OwnerUsername string     `json:"owner_username"`
	OwnerName     string     `json:"owner_name"`
	Status        string     `json:"status"`
	MaxUses       uint64     `json:"max_uses"`
	UsedCount     uint64     `json:"used_count"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type InvitationRecord struct {
	ID                  int64     `json:"id"`
	InviteCodeID        int64     `json:"invite_code_id"`
	InviteCode          string    `json:"invite_code"`
	InviterUserID       int64     `json:"inviter_user_id"`
	InviterTenantID     *int64    `json:"inviter_tenant_id,omitempty"`
	InviterUsername     string    `json:"inviter_username"`
	InviterDisplayName  string    `json:"inviter_display_name"`
	ReferredUserID      int64     `json:"referred_user_id"`
	ReferredTenantID    int64     `json:"referred_tenant_id"`
	ReferredUsername    string    `json:"referred_username"`
	ReferredDisplayName string    `json:"referred_display_name"`
	SourceType          string    `json:"source_type"`
	ParentOrgID         int64     `json:"parent_org_id"`
	ParentOrgName       string    `json:"parent_org_name"`
	BoundAt             time.Time `json:"bound_at"`
}

type InvitationDashboard struct {
	MyCode           InviteCodeSummary   `json:"my_code"`
	Codes            []InviteCodeSummary `json:"codes"`
	CodesTotal       int                 `json:"codes_total"`
	Records          []InvitationRecord  `json:"records"`
	RecordsTotal     int                 `json:"records_total"`
	OwnReferralCount int                 `json:"own_referral_count"`
}
type InvitePreview struct {
	Code        string `json:"code"`
	InviterName string `json:"inviter_name"`
	InviterRole string `json:"inviter_role"`
	SourceType  string `json:"source_type"`
}
