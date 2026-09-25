package db

import (
	"context"
	"database/sql"
	"errors"

	"livecompanion/management/internal/model"
)

func containsWithdrawalBeneficiaryType(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func (s *Store) approveWithdrawalRequest(
	ctx context.Context,
	withdrawalID int64,
	approverUserID int64,
	allowedBeneficiaryTypes ...string,
) (model.WithdrawalRequest, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	defer tx.Rollback()

	policy, err := loadFinanceReviewPolicy(ctx, tx, true)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}

	var beneficiaryType, status string
	var requestedByUserID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT beneficiary_type, status, requested_by_user_id
		FROM fin_withdrawal_requests
		WHERE id=?
		FOR UPDATE
	`, withdrawalID).Scan(&beneficiaryType, &status, &requestedByUserID); err != nil {
		return model.WithdrawalRequest{}, err
	}
	if status != "reviewing" || !containsWithdrawalBeneficiaryType(allowedBeneficiaryTypes, beneficiaryType) {
		return model.WithdrawalRequest{}, ErrCustomerWithdrawalState
	}
	if policy.RequireDistinctReviewer && requestedByUserID.Valid && requestedByUserID.Int64 == approverUserID {
		return model.WithdrawalRequest{}, ErrFinanceDistinctReviewer
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE fin_withdrawal_requests
		SET status='approved',
		    approved_by_user_id=?,
		    approved_at=CURRENT_TIMESTAMP(3),
		    updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND status='reviewing'
	`, approverUserID, withdrawalID)
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.WithdrawalRequest{}, err
	}
	if affected == 0 {
		return model.WithdrawalRequest{}, ErrCustomerWithdrawalState
	}
	if err := tx.Commit(); err != nil {
		return model.WithdrawalRequest{}, err
	}
	return s.getWithdrawalRequest(ctx, withdrawalID)
}

func withdrawalReviewError(err error, stateErr error) error {
	if errors.Is(err, ErrCustomerWithdrawalState) {
		return stateErr
	}
	return err
}
