package model

import "time"

type BeneficiaryWallet struct {
	ID                    int64     `json:"id"`
	BeneficiaryType       string    `json:"beneficiary_type"`
	BeneficiaryID         int64     `json:"beneficiary_id"`
	Currency              string    `json:"currency"`
	AvailableBalanceCents int64     `json:"available_balance_cents"`
	FrozenBalanceCents    int64     `json:"frozen_balance_cents"`
	Version               uint64    `json:"version"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type BeneficiaryWalletLedger struct {
	ID                   int64     `json:"id"`
	WalletID             int64     `json:"wallet_id"`
	ExternalID           string    `json:"external_id"`
	BusinessType         string    `json:"business_type"`
	ReferenceType        string    `json:"reference_type"`
	ReferenceID          *int64    `json:"reference_id,omitempty"`
	AvailableDeltaCents  int64     `json:"available_delta_cents"`
	FrozenDeltaCents     int64     `json:"frozen_delta_cents"`
	AvailableBeforeCents int64     `json:"available_before_cents"`
	AvailableAfterCents  int64     `json:"available_after_cents"`
	FrozenBeforeCents    int64     `json:"frozen_before_cents"`
	FrozenAfterCents     int64     `json:"frozen_after_cents"`
	OperatorUserID       *int64    `json:"operator_user_id,omitempty"`
	Reason               string    `json:"reason"`
	CreatedAt            time.Time `json:"created_at"`
}

type WithdrawalRequest struct {
	ID                int64      `json:"id"`
	WithdrawalNo      string     `json:"withdrawal_no"`
	WalletID          int64      `json:"wallet_id"`
	BeneficiaryType   string     `json:"beneficiary_type"`
	BeneficiaryID     int64      `json:"beneficiary_id"`
	AmountCents       int64      `json:"amount_cents"`
	Status            string     `json:"status"`
	RequestedByUserID int64      `json:"requested_by_user_id"`
	ApprovedByUserID  *int64     `json:"approved_by_user_id,omitempty"`
	PaidByUserID      *int64     `json:"paid_by_user_id,omitempty"`
	RejectReason      string     `json:"reject_reason"`
	RequestedAt       time.Time  `json:"requested_at"`
	ApprovedAt        *time.Time `json:"approved_at,omitempty"`
	PaidAt            *time.Time `json:"paid_at,omitempty"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type BeneficiaryWalletDashboard struct {
	Wallet      BeneficiaryWallet        `json:"wallet"`
	Ledger      []BeneficiaryWalletLedger `json:"ledger"`
	Withdrawals []WithdrawalRequest       `json:"withdrawals"`
}

type CreateWithdrawalInput struct {
	AmountCents int64 `json:"amount_cents"`
}

type RejectWithdrawalInput struct {
	Reason string `json:"reason"`
}
