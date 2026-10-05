package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

func (s *Store) GetAccountDashboard(
	ctx context.Context,
	userID int64,
	tenantID *int64,
) (model.AccountDashboard, error) {
	profile, err := s.GetAccountProfile(ctx, userID)
	if err != nil {
		return model.AccountDashboard{}, err
	}

	result := model.AccountDashboard{
		Profile: profile,
		Quota:   model.QuotaSummary{},
	}
	if tenantID == nil {
		return result, nil
	}

	membership, err := s.getCurrentMembership(ctx, *tenantID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.AccountDashboard{}, err
	}
	if err == nil {
		result.Membership = &membership
	}

	quota, err := s.getQuotaSummary(ctx, *tenantID)
	if err != nil {
		return model.AccountDashboard{}, err
	}
	result.Quota = quota

	return result, nil
}

func (s *Store) GetAccountProfile(ctx context.Context, userID int64) (model.AccountProfile, error) {
	var item model.AccountProfile
	var tenantID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			tenant_id,
			username,
			display_name,
			avatar_url,
			role,
			phone,
			email,
			qq,
			wechat,
			province,
			city,
			district,
			address,
			status,
			created_at
		FROM mgmt_users
		WHERE id = ?
		LIMIT 1
	`, userID).Scan(
		&item.UserID,
		&tenantID,
		&item.Username,
		&item.DisplayName,
		&item.AvatarURL,
		&item.Role,
		&item.Phone,
		&item.Email,
		&item.QQ,
		&item.Wechat,
		&item.Province,
		&item.City,
		&item.District,
		&item.Address,
		&item.Status,
		&item.CreatedAt,
	)
	if err != nil {
		return model.AccountProfile{}, err
	}
	if tenantID.Valid {
		value := tenantID.Int64
		item.TenantID = &value
	}
	return item, nil
}

func (s *Store) UpdateAccountProfile(
	ctx context.Context,
	userID int64,
	displayName string,
	phone string,
	email string,
	qq string,
	wechat string,
	province string,
	city string,
	district string,
	address string,
) (model.AccountProfile, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE mgmt_users
		SET display_name = ?,
		    phone = ?,
		    email = ?,
		    qq = ?,
		    wechat = ?,
		    province = ?,
		    city = ?,
		    district = ?,
		    address = ?
		WHERE id = ? AND status = 'active'
	`,
		strings.TrimSpace(displayName),
		strings.TrimSpace(phone),
		strings.TrimSpace(email),
		strings.TrimSpace(qq),
		strings.TrimSpace(wechat),
		strings.TrimSpace(province),
		strings.TrimSpace(city),
		strings.TrimSpace(district),
		strings.TrimSpace(address),
		userID,
	)
	if err != nil {
		return model.AccountProfile{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.AccountProfile{}, err
	}
	if affected == 0 {
		return model.AccountProfile{}, sql.ErrNoRows
	}
	return s.GetAccountProfile(ctx, userID)
}
func (s *Store) UpdateAccountAvatar(
	ctx context.Context,
	userID int64,
	avatarURL string,
) (model.AccountProfile, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE mgmt_users
		SET avatar_url = ?
		WHERE id = ? AND status = 'active'
	`, strings.TrimSpace(avatarURL), userID)
	if err != nil {
		return model.AccountProfile{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.AccountProfile{}, err
	}
	if affected == 0 {
		return model.AccountProfile{}, sql.ErrNoRows
	}
	return s.GetAccountProfile(ctx, userID)
}

func (s *Store) getCurrentMembership(
	ctx context.Context,
	tenantID int64,
) (model.MembershipSummary, error) {
	var item model.MembershipSummary
	err := s.db.QueryRowContext(ctx, `
		SELECT
			m.id,
			m.plan_id,
			m.plan_version_id,
			p.name,
			m.status,
			m.cycle_start_at,
			m.cycle_end_at,
			v.included_seconds,
			m.auto_renew
		FROM biz_memberships m
		INNER JOIN catalog_membership_plans p ON p.id = m.plan_id
		INNER JOIN catalog_membership_plan_versions v ON v.id = m.plan_version_id
		WHERE m.tenant_id = ?
		  AND m.status = 'active'
		  AND m.cycle_start_at <= CURRENT_TIMESTAMP(3)
		  AND m.cycle_end_at > CURRENT_TIMESTAMP(3)
		ORDER BY m.cycle_end_at DESC, m.id DESC
		LIMIT 1
	`, tenantID).Scan(
		&item.ID,
		&item.PlanID,
		&item.PlanVersionID,
		&item.PlanName,
		&item.Status,
		&item.CycleStartAt,
		&item.CycleEndAt,
		&item.IncludedSeconds,
		&item.AutoRenew,
	)
	return item, err
}

func (s *Store) getQuotaSummary(ctx context.Context, tenantID int64) (model.QuotaSummary, error) {
	var membership uint64
	var purchased uint64
	var reward uint64
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN source_type = 'membership_cycle' THEN remaining_seconds ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN source_type = 'time_card_purchase' THEN remaining_seconds ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN source_type NOT IN ('membership_cycle', 'time_card_purchase') THEN remaining_seconds ELSE 0 END), 0)
		FROM quota_buckets
		WHERE tenant_id = ?
		  AND status = 'active'
		  AND effective_at <= CURRENT_TIMESTAMP(3)
		  AND expires_at > CURRENT_TIMESTAMP(3)
		  AND remaining_seconds > 0
	`, tenantID).Scan(&membership, &purchased, &reward)
	if err != nil {
		return model.QuotaSummary{}, err
	}
	return model.QuotaSummary{
		MembershipSeconds: membership,
		PurchasedSeconds:  purchased,
		RewardSeconds:     reward,
		TotalSeconds:      membership + purchased + reward,
	}, nil
}

func (s *Store) getFinanceDashboardOnce(
	ctx context.Context,
	tenantID int64,
	limit int,
) (model.FinanceDashboard, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	result := model.FinanceDashboard{
		Ledger:    make([]model.WalletLedgerRecord, 0),
		Recharges: make([]model.RechargeRecord, 0),
		Purchases: make([]model.PurchaseRecord, 0),
		Refunds:   make([]model.RefundRecord, 0),
		Payments:  make([]model.SandboxPaymentRecord, 0),
	}

	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN account_type = 'cash' THEN balance_cents ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN account_type = 'reward' THEN balance_cents ELSE 0 END), 0),
			COALESCE(SUM(balance_cents), 0)
		FROM fin_wallet_accounts
		WHERE tenant_id = ? AND status = 'active'
	`, tenantID).Scan(
		&result.CashBalanceCents,
		&result.RewardBalanceCents,
		&result.TotalBalanceCents,
	); err != nil {
		return model.FinanceDashboard{}, err
	}

	monthStart := time.Now().UTC()
	monthStart = time.Date(monthStart.Year(), monthStart.Month(), 1, 0, 0, 0, 0, time.UTC)
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(GREATEST(paid_amount_cents - refunded_amount_cents, 0)), 0)
		FROM biz_orders
		WHERE tenant_id = ?
		  AND paid_at IS NOT NULL
		  AND paid_at >= ?
	`, tenantID, monthStart).Scan(&result.MonthSpentCents); err != nil {
		return model.FinanceDashboard{}, err
	}

	quota, err := s.getQuotaSummary(ctx, tenantID)
	if err != nil {
		return model.FinanceDashboard{}, err
	}
	result.AvailableSeconds = quota.TotalSeconds

	membership, err := s.getCurrentMembership(ctx, tenantID)
	if err == nil {
		result.MembershipName = membership.PlanName
	} else if !errors.Is(err, sql.ErrNoRows) {
		return model.FinanceDashboard{}, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, direction, amount_cents, balance_before_cents, balance_after_cents,
			business_type, COALESCE(order_no, ''), reason, occurred_at
		FROM fin_wallet_ledger
		WHERE tenant_id = ?
		ORDER BY occurred_at DESC, id DESC
		LIMIT ?
	`, tenantID, limit)
	if err != nil {
		return model.FinanceDashboard{}, err
	}
	for rows.Next() {
		var item model.WalletLedgerRecord
		if err := rows.Scan(
			&item.ID,
			&item.Direction,
			&item.AmountCents,
			&item.BalanceBeforeCents,
			&item.BalanceAfterCents,
			&item.BusinessType,
			&item.OrderNo,
			&item.Reason,
			&item.OccurredAt,
		); err != nil {
			rows.Close()
			return model.FinanceDashboard{}, err
		}
		result.Ledger = append(result.Ledger, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return model.FinanceDashboard{}, err
	}
	rows.Close()

	rechargeRows, err := s.db.QueryContext(ctx, `
		SELECT
			id, recharge_no, requested_amount_cents, credited_amount_cents,
			payment_method, status, paid_at, created_at
		FROM fin_recharge_orders
		WHERE tenant_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, tenantID, limit)
	if err != nil {
		return model.FinanceDashboard{}, err
	}
	for rechargeRows.Next() {
		var item model.RechargeRecord
		if err := rechargeRows.Scan(
			&item.ID,
			&item.RechargeNo,
			&item.RequestedAmountCents,
			&item.CreditedAmountCents,
			&item.PaymentMethod,
			&item.Status,
			&item.PaidAt,
			&item.CreatedAt,
		); err != nil {
			rechargeRows.Close()
			return model.FinanceDashboard{}, err
		}
		result.Recharges = append(result.Recharges, item)
	}
	if err := rechargeRows.Err(); err != nil {
		rechargeRows.Close()
		return model.FinanceDashboard{}, err
	}
	rechargeRows.Close()

	paymentRows, err := s.db.QueryContext(ctx, `
		SELECT id, payment_no, tenant_id, order_id, order_no,
		       channel, payment_method, currency, expected_amount_cents,
		       input_amount_cents, paid_amount_cents, status, failure_reason,
		       external_trade_no, operator_user_id, idempotency_key,
		       paid_at, created_at, updated_at
		FROM fin_payment_transactions
		WHERE tenant_id=?
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, tenantID, limit)
	if err != nil {
		return model.FinanceDashboard{}, err
	}
	for paymentRows.Next() {
		var item model.SandboxPaymentRecord
		if err := paymentRows.Scan(
			&item.ID,
			&item.PaymentNo,
			&item.TenantID,
			&item.OrderID,
			&item.OrderNo,
			&item.Channel,
			&item.PaymentMethod,
			&item.Currency,
			&item.ExpectedAmountCents,
			&item.InputAmountCents,
			&item.PaidAmountCents,
			&item.Status,
			&item.FailureReason,
			&item.ExternalTradeNo,
			&item.OperatorUserID,
			&item.IdempotencyKey,
			&item.PaidAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			paymentRows.Close()
			return model.FinanceDashboard{}, err
		}
		result.Payments = append(result.Payments, item)
	}
	if err := paymentRows.Err(); err != nil {
		paymentRows.Close()
		return model.FinanceDashboard{}, err
	}
	paymentRows.Close()

	purchaseRows, err := s.db.QueryContext(ctx, `
		SELECT
			id, order_no, order_type, status, list_amount_cents,
			discount_amount_cents, paid_amount_cents, refunded_amount_cents,
			paid_at, created_at
		FROM biz_orders
		WHERE tenant_id = ?
		  AND order_type IN ('membership', 'time_card')
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, tenantID, limit)
	if err != nil {
		return model.FinanceDashboard{}, err
	}
	for purchaseRows.Next() {
		var item model.PurchaseRecord
		if err := purchaseRows.Scan(
			&item.ID,
			&item.OrderNo,
			&item.OrderType,
			&item.Status,
			&item.ListAmountCents,
			&item.DiscountAmountCents,
			&item.PaidAmountCents,
			&item.RefundedAmountCents,
			&item.PaidAt,
			&item.CreatedAt,
		); err != nil {
			purchaseRows.Close()
			return model.FinanceDashboard{}, err
		}
		result.Purchases = append(result.Purchases, item)
	}
	if err := purchaseRows.Err(); err != nil {
		purchaseRows.Close()
		return model.FinanceDashboard{}, err
	}
	purchaseRows.Close()

	refundRows, err := s.db.QueryContext(ctx, `
		SELECT
			id, refund_no, source_type, source_id, refund_amount_cents,
			refund_method, status, reason, processed_at, created_at
		FROM fin_refund_orders
		WHERE tenant_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, tenantID, limit)
	if err != nil {
		return model.FinanceDashboard{}, err
	}
	for refundRows.Next() {
		var item model.RefundRecord
		if err := refundRows.Scan(
			&item.ID,
			&item.RefundNo,
			&item.SourceType,
			&item.SourceID,
			&item.RefundAmountCents,
			&item.RefundMethod,
			&item.Status,
			&item.Reason,
			&item.ProcessedAt,
			&item.CreatedAt,
		); err != nil {
			refundRows.Close()
			return model.FinanceDashboard{}, err
		}
		result.Refunds = append(result.Refunds, item)
	}
	if err := refundRows.Err(); err != nil {
		refundRows.Close()
		return model.FinanceDashboard{}, err
	}
	refundRows.Close()

	return result, nil
}
