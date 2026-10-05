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
	ErrCustomerWithdrawalAccount      = errors.New("customer withdrawal account invalid")
	ErrCustomerWithdrawalAmount       = errors.New("customer withdrawal amount invalid")
	ErrCustomerWithdrawalInsufficient = errors.New("customer withdrawal balance insufficient")
	ErrCustomerWithdrawalState        = errors.New("customer withdrawal state invalid")
	ErrCashWithdrawalUseRefund        = errors.New("cash recharge principal must use original-route refund")
)

func customerWalletBeneficiaryType(accountType string) string {
	switch strings.TrimSpace(accountType) {
	case "cash":
		return "customer_cash"
	case "reward":
		return "customer_reward"
	default:
		return ""
	}
}

func customerWalletAccountType(beneficiaryType string) string {
	switch strings.TrimSpace(beneficiaryType) {
	case "customer_cash":
		return "cash"
	case "customer_reward":
		return "reward"
	default:
		return ""
	}
}

func customerWalletRowForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	accountType string,
) (int64, int64, int64, error) {
	var id, balance, frozen int64
	err := tx.QueryRowContext(ctx, `
		SELECT id, balance_cents, frozen_balance_cents
		FROM fin_wallet_accounts
		WHERE tenant_id=? AND account_type=? AND currency='CNY' AND status='active'
		FOR UPDATE
	`, tenantID, accountType).Scan(&id, &balance, &frozen)
	return id, balance, frozen, err
}

func insertCustomerWalletLedgerTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	walletID int64,
	externalID string,
	direction string,
	amount int64,
	before int64,
	after int64,
	businessType string,
	businessID int64,
	operatorUserID int64,
	reason string,
) error {
	if amount < 0 {
		amount = -amount
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO fin_wallet_ledger (
			external_id, tenant_id, wallet_account_id,
			direction, amount_cents,
			balance_before_cents, balance_after_cents,
			business_type, business_id,
			operator_user_id, reason,
			idempotency_key, occurred_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP(3))
	`,
		externalID,
		tenantID,
		walletID,
		direction,
		amount,
		before,
		after,
		businessType,
		businessID,
		operatorUserID,
		reason,
		externalID,
	)
	return err
}

func (s *Store) CreateCustomerWalletWithdrawal(
	ctx context.Context,
	tenantID int64,
	requestedByUserID int64,
	accountType string,
	amountCents int64,
) (model.WithdrawalRequest, error) {
	if strings.TrimSpace(accountType) == "cash" {
		return model.WithdrawalRequest{}, ErrCashWithdrawalUseRefund
	}
	beneficiaryType := customerWalletBeneficiaryType(accountType)
	if beneficiaryType == "" {
		return model.WithdrawalRequest{}, ErrCustomerWithdrawalAccount
	}
	if amountCents <= 0 {
		return model.WithdrawalRequest{}, ErrCustomerWithdrawalAmount
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	defer tx.Rollback()

	walletID, balance, frozen, err := customerWalletRowForUpdate(ctx, tx, tenantID, accountType)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	if balance < amountCents {
		return model.WithdrawalRequest{}, ErrCustomerWithdrawalInsufficient
	}

	withdrawalNo := fmt.Sprintf("WD-%s-%d", strings.ToUpper(accountType), time.Now().UTC().UnixNano())
	result, err := tx.ExecContext(ctx, `
		INSERT INTO fin_withdrawal_requests (
			withdrawal_no, wallet_id, beneficiary_type, beneficiary_id,
			amount_cents, status, requested_by_user_id
		)
		VALUES (?, ?, ?, ?, ?, 'reviewing', ?)
	`, withdrawalNo, walletID, beneficiaryType, tenantID, amountCents, requestedByUserID)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	withdrawalID, err := result.LastInsertId()
	if err != nil {
		return model.WithdrawalRequest{}, err
	}

	after := balance - amountCents
	if _, err := tx.ExecContext(ctx, `
		UPDATE fin_wallet_accounts
		SET balance_cents=?, frozen_balance_cents=?
		WHERE id=?
	`, after, frozen+amountCents, walletID); err != nil {
		return model.WithdrawalRequest{}, err
	}
	if err := insertCustomerWalletLedgerTx(
		ctx,
		tx,
		tenantID,
		walletID,
		fmt.Sprintf("withdrawal-hold-%d", withdrawalID),
		"debit",
		amountCents,
		balance,
		after,
		"withdrawal_hold",
		withdrawalID,
		requestedByUserID,
		"提现申请冻结",
	); err != nil {
		return model.WithdrawalRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.WithdrawalRequest{}, err
	}
	return s.getWithdrawalRequest(ctx, withdrawalID)
}

func (s *Store) ListCustomerWalletWithdrawals(
	ctx context.Context,
	tenantID int64,
	accountType string,
	limit int,
) ([]model.WithdrawalRequest, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query := `
		SELECT id, withdrawal_no, wallet_id, beneficiary_type, beneficiary_id,
		       amount_cents, status, requested_by_user_id,
		       approved_by_user_id, paid_by_user_id, reject_reason,
		       requested_at, approved_at, paid_at, updated_at
		FROM fin_withdrawal_requests
		WHERE beneficiary_id=?
		  AND beneficiary_type IN ('customer_cash','customer_reward')
	`
	args := []any{tenantID}
	if accountType != "" && accountType != "all" {
		beneficiaryType := customerWalletBeneficiaryType(accountType)
		if beneficiaryType == "" {
			return nil, ErrCustomerWithdrawalAccount
		}
		query += " AND beneficiary_type=?"
		args = append(args, beneficiaryType)
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

func (s *Store) ListAllCustomerWalletWithdrawals(ctx context.Context, status string, limit int) ([]model.WithdrawalRequest, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query := `
		SELECT id, withdrawal_no, wallet_id, beneficiary_type, beneficiary_id,
		       amount_cents, status, requested_by_user_id,
		       approved_by_user_id, paid_by_user_id, reject_reason,
		       requested_at, approved_at, paid_at, updated_at
		FROM fin_withdrawal_requests
		WHERE beneficiary_type IN ('customer_cash','customer_reward')
	`
	args := []any{}
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

func (s *Store) ApproveCustomerWalletWithdrawal(
	ctx context.Context,
	withdrawalID int64,
	approverUserID int64,
) (model.WithdrawalRequest, error) {
	return s.approveWithdrawalRequest(
		ctx,
		withdrawalID,
		approverUserID,
		"customer_cash",
		"customer_reward",
	)
}

func (s *Store) RejectCustomerWalletWithdrawal(
	ctx context.Context,
	withdrawalID int64,
	operatorUserID int64,
	reason string,
) (model.WithdrawalRequest, error) {
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
		WHERE id=?
		  AND beneficiary_type IN ('customer_cash','customer_reward')
		FOR UPDATE
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
	); err != nil {
		return model.WithdrawalRequest{}, err
	}
	if item.Status != "reviewing" && item.Status != "approved" {
		return model.WithdrawalRequest{}, ErrCustomerWithdrawalState
	}
	accountType := customerWalletAccountType(item.BeneficiaryType)
	walletID, balance, frozen, err := customerWalletRowForUpdate(ctx, tx, item.BeneficiaryID, accountType)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	if frozen < item.AmountCents {
		return model.WithdrawalRequest{}, ErrCustomerWithdrawalState
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE fin_withdrawal_requests
		SET status='rejected', reject_reason=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=?
	`, strings.TrimSpace(reason), withdrawalID); err != nil {
		return model.WithdrawalRequest{}, err
	}
	after := balance + item.AmountCents
	if _, err := tx.ExecContext(ctx, `
		UPDATE fin_wallet_accounts
		SET balance_cents=?, frozen_balance_cents=?
		WHERE id=?
	`, after, frozen-item.AmountCents, walletID); err != nil {
		return model.WithdrawalRequest{}, err
	}
	if err := insertCustomerWalletLedgerTx(
		ctx,
		tx,
		item.BeneficiaryID,
		walletID,
		fmt.Sprintf("withdrawal-release-%d", withdrawalID),
		"credit",
		item.AmountCents,
		balance,
		after,
		"withdrawal_rejected",
		withdrawalID,
		operatorUserID,
		"提现驳回，金额退回可用余额",
	); err != nil {
		return model.WithdrawalRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.WithdrawalRequest{}, err
	}
	return s.getWithdrawalRequest(ctx, withdrawalID)
}

func (s *Store) PayCustomerWalletWithdrawal(
	ctx context.Context,
	withdrawalID int64,
	paidByUserID int64,
) (model.WithdrawalRequest, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	defer tx.Rollback()

	var beneficiaryType string
	var tenantID, amount int64
	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT beneficiary_type, beneficiary_id, amount_cents, status
		FROM fin_withdrawal_requests
		WHERE id=?
		  AND beneficiary_type IN ('customer_cash','customer_reward')
		FOR UPDATE
	`, withdrawalID).Scan(&beneficiaryType, &tenantID, &amount, &status); err != nil {
		return model.WithdrawalRequest{}, err
	}
	if status != "approved" {
		return model.WithdrawalRequest{}, ErrCustomerWithdrawalState
	}
	accountType := customerWalletAccountType(beneficiaryType)
	walletID, _, frozen, err := customerWalletRowForUpdate(ctx, tx, tenantID, accountType)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	if frozen < amount {
		return model.WithdrawalRequest{}, ErrCustomerWithdrawalState
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE fin_wallet_accounts
		SET frozen_balance_cents=?
		WHERE id=?
	`, frozen-amount, walletID); err != nil {
		return model.WithdrawalRequest{}, err
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
	if err := tx.Commit(); err != nil {
		return model.WithdrawalRequest{}, err
	}
	return s.getWithdrawalRequest(ctx, withdrawalID)
}
