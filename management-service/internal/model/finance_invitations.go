package model

import "time"

// Finance invitation reads reuse the existing financial-account visibility right.
// Reading this projection grants no invitation, reward, or settlement write rights.
func CanReadFinanceInvitations(actor Actor, access StaffAccessContext) bool {
	if !actor.IsInternalStaff() || actor.UserID <= 0 {
		return false
	}
	if actor.IsPlatformAdmin() || access.IsSuperAdmin {
		return true
	}
	for _, permission := range access.Permissions {
		if permission == "finance.dashboard.view" {
			return true
		}
	}
	return false
}

type FinanceInvitation struct {
	InvitationRecord
	ConfirmedAt           *time.Time `json:"confirmed_at,omitempty"`
	ConfirmedAmountCents  uint64     `json:"confirmed_amount_cents"`
	ConfirmationReceiptID *int64     `json:"confirmation_receipt_id,omitempty"`
}

type FinanceInvitationEarning struct {
	ID                  int64     `json:"id"`
	ExternalID          string    `json:"external_id"`
	OrderID             int64     `json:"order_id"`
	OrderNo             string    `json:"order_no"`
	BeneficiaryType     string    `json:"beneficiary_type"`
	BeneficiaryID       int64     `json:"beneficiary_id"`
	EarningType         string    `json:"earning_type"`
	AmountCents         int64     `json:"amount_cents"`
	Currency            string    `json:"currency"`
	QuotaSeconds        int64     `json:"quota_seconds"`
	Status              string    `json:"status"`
	ProgramVersionID    *int64    `json:"program_version_id,omitempty"`
	RuleID              *int64    `json:"rule_id,omitempty"`
	SourceRefundID      *int64    `json:"source_refund_id,omitempty"`
	ReversalOfEarningID *int64    `json:"reversal_of_earning_id,omitempty"`
	SettlementBatchNo   string    `json:"settlement_batch_no"`
	SettlementStatus    string    `json:"settlement_status"`
	CreatedAt           time.Time `json:"created_at"`
}
