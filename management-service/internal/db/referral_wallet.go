package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

var (
	ErrReferralWithdrawalAmount       = errors.New("referral withdrawal amount invalid")
	ErrReferralWithdrawalInsufficient = errors.New("referral withdrawal balance insufficient")
	ErrReferralWithdrawalState        = errors.New("referral withdrawal state invalid")
)

func (s *Store) GetCustomerReferralWalletDashboard(ctx context.Context, tenantID int64, limit int) (model.BeneficiaryWalletDashboard, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.BeneficiaryWalletDashboard{}, err
	}
	defer tx.Rollback()
	if _, err := ensureBeneficiaryWalletTx(ctx, tx, customerReferralBeneficiaryType, tenantID); err != nil {
		return model.BeneficiaryWalletDashboard{}, err
	}
	if err := releaseMatureReferralRewardsTx(ctx, tx, tenantID); err != nil {
		return model.BeneficiaryWalletDashboard{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.BeneficiaryWalletDashboard{}, err
	}

	var result model.BeneficiaryWalletDashboard
	if err := s.db.QueryRowContext(ctx, `
		SELECT id, beneficiary_type, beneficiary_id, currency,
		       available_balance_cents, frozen_balance_cents,
		       version, created_at, updated_at
		FROM fin_beneficiary_wallets
		WHERE beneficiary_type=? AND beneficiary_id=? AND currency='CNY'
	`, customerReferralBeneficiaryType, tenantID).Scan(
		&result.Wallet.ID,
		&result.Wallet.BeneficiaryType,
		&result.Wallet.BeneficiaryID,
		&result.Wallet.Currency,
		&result.Wallet.AvailableBalanceCents,
		&result.Wallet.FrozenBalanceCents,
		&result.Wallet.Version,
		&result.Wallet.CreatedAt,
		&result.Wallet.UpdatedAt,
	); err != nil {
		return model.BeneficiaryWalletDashboard{}, err
	}

	result.Ledger = make([]model.BeneficiaryWalletLedger, 0)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wallet_id, external_id, business_type,
		       reference_type, reference_id,
		       available_delta_cents, frozen_delta_cents,
		       available_before_cents, available_after_cents,
		       frozen_before_cents, frozen_after_cents,
		       operator_user_id, reason, created_at
		FROM fin_beneficiary_wallet_ledger
		WHERE wallet_id=?
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, result.Wallet.ID, limit)
	if err != nil {
		return model.BeneficiaryWalletDashboard{}, err
	}
	for rows.Next() {
		var item model.BeneficiaryWalletLedger
		if err := rows.Scan(
			&item.ID,
			&item.WalletID,
			&item.ExternalID,
			&item.BusinessType,
			&item.ReferenceType,
			&item.ReferenceID,
			&item.AvailableDeltaCents,
			&item.FrozenDeltaCents,
			&item.AvailableBeforeCents,
			&item.AvailableAfterCents,
			&item.FrozenBeforeCents,
			&item.FrozenAfterCents,
			&item.OperatorUserID,
			&item.Reason,
			&item.CreatedAt,
		); err != nil {
			rows.Close()
			return model.BeneficiaryWalletDashboard{}, err
		}
		result.Ledger = append(result.Ledger, item)
	}
	if err := rows.Close(); err != nil {
		return model.BeneficiaryWalletDashboard{}, err
	}

	result.Withdrawals = make([]model.WithdrawalRequest, 0)
	withdrawalRows, err := s.db.QueryContext(ctx, `
		SELECT id, withdrawal_no, wallet_id, beneficiary_type, beneficiary_id,
		       amount_cents, status, requested_by_user_id,
		       approved_by_user_id, paid_by_user_id, reject_reason,
		       requested_at, approved_at, paid_at, updated_at
		FROM fin_withdrawal_requests
		WHERE beneficiary_type=? AND beneficiary_id=?
		ORDER BY requested_at DESC, id DESC
		LIMIT ?
	`, customerReferralBeneficiaryType, tenantID, limit)
	if err != nil {
		return model.BeneficiaryWalletDashboard{}, err
	}
	for withdrawalRows.Next() {
		var item model.WithdrawalRequest
		if err := withdrawalRows.Scan(
			&item.ID,
			&item.WithdrawalNo,
			&item.WalletID,
			&item.BeneficiaryType,
			&item.BeneficiaryID,
			&item.AmountCents,
			&item.Status,
			&item.RequestedByUserID,
			&item.ApprovedByUserID,
			&item.PaidByUserID,
			&item.RejectReason,
			&item.RequestedAt,
			&item.ApprovedAt,
			&item.PaidAt,
			&item.UpdatedAt,
		); err != nil {
			withdrawalRows.Close()
			return model.BeneficiaryWalletDashboard{}, err
		}
		result.Withdrawals = append(result.Withdrawals, item)
	}
	if err := withdrawalRows.Close(); err != nil {
		return model.BeneficiaryWalletDashboard{}, err
	}
	return result, nil
}

func (s *Store) CreateCustomerReferralWithdrawal(ctx context.Context, tenantID int64, requestedByUserID int64, amountCents int64) (model.WithdrawalRequest, error) {
	if amountCents <= 0 {
		return model.WithdrawalRequest{}, ErrReferralWithdrawalAmount
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	defer tx.Rollback()
	if err := releaseMatureReferralRewardsTx(ctx, tx, tenantID); err != nil {
		return model.WithdrawalRequest{}, err
	}
	wallet, err := ensureBeneficiaryWalletTx(ctx, tx, customerReferralBeneficiaryType, tenantID)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	if wallet.AvailableBalanceCents < amountCents {
		return model.WithdrawalRequest{}, ErrReferralWithdrawalInsufficient
	}
	withdrawalNo := fmt.Sprintf("WD-REF-%d", time.Now().UTC().UnixNano())
	result, err := tx.ExecContext(ctx, `
		INSERT INTO fin_withdrawal_requests (
			withdrawal_no, wallet_id, beneficiary_type, beneficiary_id,
			amount_cents, status, requested_by_user_id
		)
		VALUES (?, ?, ?, ?, ?, 'reviewing', ?)
	`, withdrawalNo, wallet.ID, customerReferralBeneficiaryType, tenantID, amountCents, requestedByUserID)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	withdrawalID, err := result.LastInsertId()
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	referenceID := withdrawalID
	operatorID := requestedByUserID
	if err := applyBeneficiaryWalletDeltaTx(
		ctx, tx, customerReferralBeneficiaryType, tenantID,
		fmt.Sprintf("referral-withdrawal-hold-%d", withdrawalID),
		"withdrawal_hold", "withdrawal", &referenceID,
		-amountCents, amountCents, &operatorID, "返佣提现申请冻结",
	); err != nil {
		return model.WithdrawalRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.WithdrawalRequest{}, err
	}
	return s.getWithdrawalRequest(ctx, withdrawalID)
}

func (s *Store) getWithdrawalRequest(ctx context.Context, withdrawalID int64) (model.WithdrawalRequest, error) {
	var item model.WithdrawalRequest
	err := s.db.QueryRowContext(ctx, `
		SELECT id, withdrawal_no, wallet_id, beneficiary_type, beneficiary_id,
		       amount_cents, status, requested_by_user_id,
		       approved_by_user_id, paid_by_user_id, reject_reason,
		       requested_at, approved_at, paid_at, updated_at
		FROM fin_withdrawal_requests
		WHERE id=?
	`, withdrawalID).Scan(
		&item.ID,
		&item.WithdrawalNo,
		&item.WalletID,
		&item.BeneficiaryType,
		&item.BeneficiaryID,
		&item.AmountCents,
		&item.Status,
		&item.RequestedByUserID,
		&item.ApprovedByUserID,
		&item.PaidByUserID,
		&item.RejectReason,
		&item.RequestedAt,
		&item.ApprovedAt,
		&item.PaidAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (s *Store) ListReferralWithdrawals(ctx context.Context, status string, limit int) ([]model.WithdrawalRequest, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query := `
		SELECT id, withdrawal_no, wallet_id, beneficiary_type, beneficiary_id,
		       amount_cents, status, requested_by_user_id,
		       approved_by_user_id, paid_by_user_id, reject_reason,
		       requested_at, approved_at, paid_at, updated_at
		FROM fin_withdrawal_requests
		WHERE beneficiary_type=?
	`
	args := []any{customerReferralBeneficiaryType}
	status = strings.TrimSpace(status)
	if status != "" && status != "all" {
		query += " AND status=?"
		args = append(args, status)
	}
	query += " ORDER BY requested_at DESC, id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.WithdrawalRequest, 0)
	for rows.Next() {
		var item model.WithdrawalRequest
		if err := rows.Scan(
			&item.ID,
			&item.WithdrawalNo,
			&item.WalletID,
			&item.BeneficiaryType,
			&item.BeneficiaryID,
			&item.AmountCents,
			&item.Status,
			&item.RequestedByUserID,
			&item.ApprovedByUserID,
			&item.PaidByUserID,
			&item.RejectReason,
			&item.RequestedAt,
			&item.ApprovedAt,
			&item.PaidAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ApproveReferralWithdrawal(ctx context.Context, withdrawalID int64, approverUserID int64) (model.WithdrawalRequest, error) {
	item, err := s.approveWithdrawalRequest(
		ctx,
		withdrawalID,
		approverUserID,
		customerReferralBeneficiaryType,
	)
	return item, withdrawalReviewError(err, ErrReferralWithdrawalState)
}

func (s *Store) RejectReferralWithdrawal(ctx context.Context, withdrawalID int64, operatorUserID int64, reason string) (model.WithdrawalRequest, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	defer tx.Rollback()

	var item model.WithdrawalRequest
	if err := tx.QueryRowContext(ctx, `
		SELECT id, withdrawal_no, wallet_id, beneficiary_type, beneficiary_id,
		       amount_cents, status, requested_by_user_id,
		       approved_by_user_id, paid_by_user_id, reject_reason,
		       requested_at, approved_at, paid_at, updated_at
		FROM fin_withdrawal_requests
		WHERE id=? AND beneficiary_type=?
		FOR UPDATE
	`, withdrawalID, customerReferralBeneficiaryType).Scan(
		&item.ID,
		&item.WithdrawalNo,
		&item.WalletID,
		&item.BeneficiaryType,
		&item.BeneficiaryID,
		&item.AmountCents,
		&item.Status,
		&item.RequestedByUserID,
		&item.ApprovedByUserID,
		&item.PaidByUserID,
		&item.RejectReason,
		&item.RequestedAt,
		&item.ApprovedAt,
		&item.PaidAt,
		&item.UpdatedAt,
	); err != nil {
		return model.WithdrawalRequest{}, err
	}
	if item.Status != "reviewing" && item.Status != "approved" {
		return model.WithdrawalRequest{}, ErrReferralWithdrawalState
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE fin_withdrawal_requests
		SET status='rejected', reject_reason=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=?
	`, strings.TrimSpace(reason), withdrawalID); err != nil {
		return model.WithdrawalRequest{}, err
	}
	referenceID := withdrawalID
	operatorID := operatorUserID
	if err := applyBeneficiaryWalletDeltaTx(
		ctx, tx, customerReferralBeneficiaryType, item.BeneficiaryID,
		fmt.Sprintf("referral-withdrawal-release-%d", withdrawalID),
		"withdrawal_rejected", "withdrawal", &referenceID,
		item.AmountCents, -item.AmountCents, &operatorID,
		"返佣提现驳回，金额退回可用余额",
	); err != nil {
		return model.WithdrawalRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.WithdrawalRequest{}, err
	}
	return s.getWithdrawalRequest(ctx, withdrawalID)
}

func (s *Store) PayReferralWithdrawal(ctx context.Context, withdrawalID int64, paidByUserID int64) (model.WithdrawalRequest, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	defer tx.Rollback()

	var beneficiaryID int64
	var amount int64
	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT beneficiary_id, amount_cents, status
		FROM fin_withdrawal_requests
		WHERE id=? AND beneficiary_type=?
		FOR UPDATE
	`, withdrawalID, customerReferralBeneficiaryType).Scan(&beneficiaryID, &amount, &status); err != nil {
		return model.WithdrawalRequest{}, err
	}
	wallet, err := ensureBeneficiaryWalletTx(ctx, tx, customerReferralBeneficiaryType, beneficiaryID)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	if wallet.AvailableBalanceCents < 0 {
		return model.WithdrawalRequest{}, ErrReferralWithdrawalInsufficient
	}
	if status != "approved" {
		return model.WithdrawalRequest{}, ErrReferralWithdrawalState
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE fin_withdrawal_requests
		SET status='paid',
		    paid_by_user_id=?,
		    paid_at=CURRENT_TIMESTAMP(3),
		    updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=?
	`, paidByUserID, withdrawalID); err != nil {
		return model.WithdrawalRequest{}, err
	}
	referenceID := withdrawalID
	operatorID := paidByUserID
	if err := applyBeneficiaryWalletDeltaTx(
		ctx, tx, customerReferralBeneficiaryType, beneficiaryID,
		fmt.Sprintf("referral-withdrawal-paid-%d", withdrawalID),
		"withdrawal_paid", "withdrawal", &referenceID,
		0, -amount, &operatorID, "返佣提现已打款",
	); err != nil {
		return model.WithdrawalRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.WithdrawalRequest{}, err
	}
	return s.getWithdrawalRequest(ctx, withdrawalID)
}

func (s *Store) HasReferralWithdrawal(ctx context.Context, withdrawalID int64) (bool, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM fin_withdrawal_requests
		WHERE id=? AND beneficiary_type=?
	`, withdrawalID, customerReferralBeneficiaryType).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
