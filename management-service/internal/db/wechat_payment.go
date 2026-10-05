package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

var (
	ErrUnsupportedWechatAutoRenew = errors.New("wechat automatic renewal is not configured")
	ErrWechatPaymentNotPending    = errors.New("wechat payment is not pending")
	ErrWechatPaymentMismatch      = errors.New("wechat payment does not match local order")
	ErrWechatOAuthStateInvalid    = errors.New("wechat oauth state is invalid or expired")
	ErrWechatPaymentInProgress    = errors.New("wechat payment is in progress")
)

// MigrateWechatPay adds only provider-specific state. The authoritative money
// record remains fin_payment_transactions and the authoritative order remains
// biz_orders.
func wechatPaymentColumnMigrations() []struct{ name, sql string } {
	return []struct {
		name string
		sql  string
	}{
		{"recharge_order_id", "ALTER TABLE fin_payment_transactions ADD COLUMN recharge_order_id BIGINT UNSIGNED NULL AFTER order_id"},
		{"provider_prepay_id", "ALTER TABLE fin_payment_transactions ADD COLUMN provider_prepay_id VARCHAR(128) NOT NULL DEFAULT '' AFTER external_trade_no"},
		{"provider_code_url", "ALTER TABLE fin_payment_transactions ADD COLUMN provider_code_url VARCHAR(512) NOT NULL DEFAULT '' AFTER provider_prepay_id"},
		{"provider_transaction_id", "ALTER TABLE fin_payment_transactions ADD COLUMN provider_transaction_id VARCHAR(128) NULL AFTER provider_prepay_id"},
		{"provider_trade_state", "ALTER TABLE fin_payment_transactions ADD COLUMN provider_trade_state VARCHAR(32) NOT NULL DEFAULT '' AFTER provider_transaction_id"},
		{"expires_at", "ALTER TABLE fin_payment_transactions ADD COLUMN expires_at DATETIME(3) NULL AFTER provider_trade_state"},
		{"notified_at", "ALTER TABLE fin_payment_transactions ADD COLUMN notified_at DATETIME(3) NULL AFTER expires_at"},
		{"provider_checked_at", "ALTER TABLE fin_payment_transactions ADD COLUMN provider_checked_at DATETIME(3) NULL AFTER notified_at"},
		{"provider_app_id", "ALTER TABLE fin_payment_transactions ADD COLUMN provider_app_id VARCHAR(64) NOT NULL DEFAULT '' AFTER notified_at"},
		{"provider_mch_id", "ALTER TABLE fin_payment_transactions ADD COLUMN provider_mch_id VARCHAR(64) NOT NULL DEFAULT '' AFTER provider_app_id"},
		{"provider_payer_hash", "ALTER TABLE fin_payment_transactions ADD COLUMN provider_payer_hash CHAR(64) NOT NULL DEFAULT '' AFTER provider_mch_id"},
	}
}

func (s *Store) MigrateWechatPay(ctx context.Context) error {
	for _, column := range wechatPaymentColumnMigrations() {
		exists, err := s.columnExists(ctx, "fin_payment_transactions", column.name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := s.db.ExecContext(ctx, column.sql); err != nil {
			return fmt.Errorf("add fin_payment_transactions.%s: %w", column.name, err)
		}
	}

	hasTransactionIndex, err := s.indexExists(ctx, "fin_payment_transactions", "uk_fin_payment_transactions_provider_txn")
	if err != nil {
		return err
	}
	if !hasTransactionIndex {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE fin_payment_transactions
			ADD UNIQUE KEY uk_fin_payment_transactions_provider_txn (channel, provider_transaction_id)
		`); err != nil {
			return fmt.Errorf("add provider transaction unique key: %w", err)
		}
	}

	return s.migrateWechatStateTables(ctx)
}

func (s *Store) migrateWechatStateTables(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS fin_wechat_session_payers (
			session_hash CHAR(64) NOT NULL,
			user_id BIGINT UNSIGNED NOT NULL,
			appid VARCHAR(64) NOT NULL,
			openid VARCHAR(128) NOT NULL,
			expires_at DATETIME(3) NOT NULL,
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (session_hash, appid),
			KEY idx_fin_wechat_payer_expiry (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS fin_wechat_oauth_states (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			state_hash CHAR(64) NOT NULL,
			user_id BIGINT UNSIGNED NOT NULL,
			session_hash CHAR(64) NOT NULL,
			return_path VARCHAR(512) NOT NULL,
			expires_at DATETIME(3) NOT NULL,
			consumed_at DATETIME(3) NULL,
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			UNIQUE KEY uk_fin_wechat_oauth_state (state_hash),
			KEY idx_fin_wechat_oauth_expiry (expires_at),
			KEY idx_fin_wechat_oauth_user (user_id, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate wechat pay: %w", err)
		}
	}
	return nil
}

func hashWechatOAuthState(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func (s *Store) CreateWechatOAuthState(
	ctx context.Context,
	userID int64,
	sessionKey string,
	rawState string,
	returnPath string,
	expiresAt time.Time,
) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fin_wechat_oauth_states (state_hash, user_id, session_hash, return_path, expires_at)
		VALUES (?, ?, ?, ?, ?)
	`, hashWechatOAuthState(rawState), userID, sessionKey, strings.TrimSpace(returnPath), expiresAt.UTC())
	return err
}

func (s *Store) ConsumeWechatOAuthState(
	ctx context.Context,
	userID int64,
	sessionKey string,
	rawState string,
) (model.WechatOAuthState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WechatOAuthState{}, err
	}
	defer tx.Rollback()

	var item model.WechatOAuthState
	var id int64
	err = tx.QueryRowContext(ctx, `
		SELECT id, user_id, return_path, expires_at
		FROM fin_wechat_oauth_states
		WHERE state_hash=? AND session_hash=? AND consumed_at IS NULL
		FOR UPDATE
	`, hashWechatOAuthState(rawState), sessionKey).Scan(&id, &item.UserID, &item.ReturnPath, &item.ExpiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.WechatOAuthState{}, ErrWechatOAuthStateInvalid
		}
		return model.WechatOAuthState{}, err
	}
	if item.UserID != userID || !item.ExpiresAt.After(time.Now().UTC()) {
		return model.WechatOAuthState{}, ErrWechatOAuthStateInvalid
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE fin_wechat_oauth_states
		SET consumed_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND consumed_at IS NULL
	`, id); err != nil {
		return model.WechatOAuthState{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.WechatOAuthState{}, err
	}
	return item, nil
}

func (s *Store) GetWechatOpenID(ctx context.Context, userID int64, sessionKey, appID string) (string, error) {
	var openID string
	err := s.db.QueryRowContext(ctx, `
		SELECT openid
		FROM fin_wechat_session_payers
		WHERE user_id=? AND session_hash=? AND appid=? AND expires_at>CURRENT_TIMESTAMP(3)
		LIMIT 1
	`, userID, sessionKey, strings.TrimSpace(appID)).Scan(&openID)
	return openID, err
}

func (s *Store) BindWechatOpenID(ctx context.Context, userID int64, sessionKey, appID, openID string) error {
	appID = strings.TrimSpace(appID)
	openID = strings.TrimSpace(openID)
	if userID <= 0 || len(sessionKey) != 64 || appID == "" || openID == "" {
		return fmt.Errorf("wechat binding is incomplete")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fin_wechat_session_payers (session_hash, user_id, appid, openid, expires_at)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE openid=VALUES(openid), user_id=VALUES(user_id),
		  expires_at=VALUES(expires_at), updated_at=CURRENT_TIMESTAMP(3)
	`, sessionKey, userID, appID, openID, time.Now().UTC().Add(2*time.Hour))
	return err
}

func (s *Store) PrepareWechatShopPayment(
	ctx context.Context,
	tenantID int64,
	userID int64,
	orderID int64,
	identity model.WechatPaymentIdentity,
) (model.WechatPaymentPreparation, error) {
	if identity.AppID == "" || identity.MchID == "" || identity.OpenID == "" {
		return model.WechatPaymentPreparation{}, ErrWechatPaymentMismatch
	}
	paymentNo, err := newFinanceReference("WXP")
	if err != nil {
		return model.WechatPaymentPreparation{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(15 * time.Minute)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WechatPaymentPreparation{}, err
	}
	defer tx.Rollback()

	var item model.WechatPaymentPreparation
	var status, membershipCycle string
	if err := tx.QueryRowContext(ctx, `
		SELECT o.order_no, o.order_type, o.status, o.currency,
		       o.payable_amount_cents, COALESCE(JSON_UNQUOTE(JSON_EXTRACT(o.pricing_snapshot_json,'$.cycle')),''),
		       COALESCE((
		         SELECT product_name_snapshot
		         FROM biz_order_items
		         WHERE order_id=o.id
		         ORDER BY id ASC LIMIT 1
		       ), '小蓝商城订单')
		FROM biz_orders o
		WHERE o.id=? AND o.tenant_id=?
		FOR UPDATE
	`, orderID, tenantID).Scan(
		&item.OrderNo,
		&item.OrderType,
		&status,
		&item.Currency,
		&item.AmountCents,
		&membershipCycle,
		&item.Description,
	); err != nil {
		return model.WechatPaymentPreparation{}, err
	}
	item.OrderID = orderID
	// Hardware needs provider-aware inventory expiry reconciliation before it can
	// accept asynchronous payments. Phase one covers the mobile checkout products.
	if item.OrderType != "membership" && item.OrderType != "time_card" {
		return model.WechatPaymentPreparation{}, ErrUnsupportedShopProduct
	}
	if item.OrderType == "membership" && membershipCycle == "recurring_month" {
		return model.WechatPaymentPreparation{}, ErrUnsupportedWechatAutoRenew
	}
	if status == "paid" || status == "fulfilled" || status == "completed" {
		return model.WechatPaymentPreparation{}, ErrShopOrderAlreadyPaid
	}
	if status == "cancelled" {
		return model.WechatPaymentPreparation{}, ErrShopOrderCancelled
	}
	if status != "pending" || item.Currency != "CNY" || item.AmountCents == 0 {
		return model.WechatPaymentPreparation{}, ErrWechatPaymentMismatch
	}

	var existingExpires sql.NullTime
	var existingAppID, existingMchID, existingPayerHash string
	err = tx.QueryRowContext(ctx, `
		SELECT id, payment_no, provider_prepay_id, expires_at,
		       provider_trade_state, paid_at, provider_app_id, provider_mch_id, provider_payer_hash
		FROM fin_payment_transactions
		WHERE tenant_id=? AND order_id=? AND channel='wechat'
		  AND status='pending'
		ORDER BY id DESC
		LIMIT 1
		FOR UPDATE
	`, tenantID, orderID).Scan(
		&item.PaymentID,
		&item.PaymentNo,
		&item.PrepayID,
		&existingExpires,
		&item.ProviderTradeState,
		&item.PaidAt,
		&existingAppID,
		&existingMchID,
		&existingPayerHash,
	)
	if err == nil {
		if !existingExpires.Valid || existingAppID != identity.AppID || existingMchID != identity.MchID ||
			existingPayerHash != hashWechatOAuthState(identity.OpenID) {
			return model.WechatPaymentPreparation{}, ErrWechatPaymentMismatch
		}
		item.ExpiresAt = existingExpires.Time.UTC()
		if err := tx.Commit(); err != nil {
			return model.WechatPaymentPreparation{}, err
		}
		return item, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.WechatPaymentPreparation{}, err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO fin_payment_transactions (
			payment_no, tenant_id, order_id, order_no,
			channel, payment_method, currency,
			expected_amount_cents, input_amount_cents, paid_amount_cents,
			status, failure_reason, external_trade_no,
			operator_user_id, idempotency_key, expires_at, provider_app_id, provider_mch_id, provider_payer_hash
		) VALUES (?, ?, ?, ?, 'wechat', 'wechat_jsapi', ?, ?, ?, 0,
		          'pending', '', '', ?, ?, ?, ?, ?, ?)
	`, paymentNo, tenantID, orderID, item.OrderNo, item.Currency,
		item.AmountCents, item.AmountCents, userID,
		"wechat-jsapi:"+paymentNo, expiresAt, identity.AppID, identity.MchID, hashWechatOAuthState(identity.OpenID),
	)
	if err != nil {
		return model.WechatPaymentPreparation{}, err
	}
	item.PaymentID, err = result.LastInsertId()
	if err != nil {
		return model.WechatPaymentPreparation{}, err
	}
	item.PaymentNo = paymentNo
	item.PrepayID = ""
	item.ExpiresAt = expiresAt
	item.ProviderTradeState = "NOTPAY"
	if err := tx.Commit(); err != nil {
		return model.WechatPaymentPreparation{}, err
	}
	return item, nil
}

func (s *Store) SaveWechatPrepayID(
	ctx context.Context,
	paymentID int64,
	prepayID string,
) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE fin_payment_transactions
		SET provider_prepay_id=?, provider_trade_state='NOTPAY'
		WHERE id=? AND channel='wechat' AND status='pending'
	`, strings.TrimSpace(prepayID), paymentID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		// MySQL reports zero affected rows for an unchanged idempotent update.
		var existing string
		if err := s.db.QueryRowContext(ctx, `SELECT provider_prepay_id FROM fin_payment_transactions
			WHERE id=? AND channel='wechat' AND status='pending'`, paymentID).Scan(&existing); err != nil || existing != strings.TrimSpace(prepayID) {
			return ErrWechatPaymentNotPending
		}
	}
	return nil
}

func (s *Store) UpdateWechatTradeState(
	ctx context.Context,
	outTradeNo string,
	tradeState string,
) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE fin_payment_transactions
		SET provider_trade_state=?, provider_checked_at=CURRENT_TIMESTAMP(3),
		    status=CASE WHEN ? IN ('CLOSED','REVOKED','PAYERROR') THEN 'closed' ELSE status END
		WHERE payment_no=? AND channel='wechat' AND status='pending'
	`, strings.TrimSpace(tradeState), strings.TrimSpace(tradeState), strings.TrimSpace(outTradeNo))
	return err
}

func (s *Store) CompleteWechatShopPayment(
	ctx context.Context,
	result model.WechatPaymentResult,
) (model.CustomerShopOrder, error) {
	var paymentID int64
	var orderID int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT id, order_id
		FROM fin_payment_transactions
		WHERE payment_no=? AND channel='wechat'
		LIMIT 1
	`, strings.TrimSpace(result.OutTradeNo)).Scan(&paymentID, &orderID); err != nil {
		return model.CustomerShopOrder{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}
	defer tx.Rollback()

	var (
		tenantID      int64
		orderNo       string
		orderType     string
		orderStatus   string
		orderCurrency string
		payableAmount uint64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT tenant_id, order_no, order_type, status, currency, payable_amount_cents
		FROM biz_orders
		WHERE id=?
		FOR UPDATE
	`, orderID).Scan(
		&tenantID, &orderNo, &orderType, &orderStatus, &orderCurrency, &payableAmount,
	); err != nil {
		return model.CustomerShopOrder{}, err
	}

	var (
		paymentStatus           string
		paymentOrderNo          string
		paymentCurrency         string
		expectedAmount          uint64
		existingProviderID      sql.NullString
		operatorUserID          sql.NullInt64
		appID, mchID, payerHash string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT status, order_no, currency, expected_amount_cents,
		       provider_transaction_id, operator_user_id, provider_app_id, provider_mch_id, provider_payer_hash
		FROM fin_payment_transactions
		WHERE id=? AND order_id=? AND channel='wechat'
		FOR UPDATE
	`, paymentID, orderID).Scan(
		&paymentStatus,
		&paymentOrderNo,
		&paymentCurrency,
		&expectedAmount,
		&existingProviderID,
		&operatorUserID,
		&appID, &mchID, &payerHash,
	); err != nil {
		return model.CustomerShopOrder{}, err
	}

	if result.TradeState != "SUCCESS" || result.TransactionID == "" || result.SuccessTime.IsZero() ||
		result.AppID != appID || result.MchID != mchID ||
		result.Currency != paymentCurrency || result.AmountCents != expectedAmount ||
		payerHash != hashWechatOAuthState(result.PayerOpenID) {
		return model.CustomerShopOrder{}, ErrWechatPaymentMismatch
	}
	if paymentStatus == "paid" || paymentStatus == "paid_unapplied" {
		if existingProviderID.Valid && existingProviderID.String == result.TransactionID {
			_ = tx.Rollback()
			return s.GetCustomerShopOrder(ctx, tenantID, orderID)
		}
		return model.CustomerShopOrder{}, ErrWechatPaymentMismatch
	}
	if (paymentStatus != "pending" && paymentStatus != "closed") || result.TradeState != "SUCCESS" ||
		paymentOrderNo != orderNo ||
		paymentCurrency != "CNY" || orderCurrency != "CNY" || result.Currency != "CNY" ||
		expectedAmount != payableAmount || result.AmountCents != payableAmount ||
		strings.TrimSpace(result.TransactionID) == "" {
		return model.CustomerShopOrder{}, ErrWechatPaymentMismatch
	}
	if orderStatus != "pending" {
		// Persist real money even when another channel or an admin already changed
		// the order. Finance sees an unapplied payment; no second fulfilment occurs.
		if _, err := tx.ExecContext(ctx, `
			UPDATE fin_payment_transactions
			SET paid_amount_cents=?, status='paid_unapplied',
			    failure_reason='order_not_pending_requires_review', external_trade_no=?,
			    provider_transaction_id=?, provider_trade_state='SUCCESS', paid_at=?,
			    notified_at=CURRENT_TIMESTAMP(3)
			WHERE id=?
		`, result.AmountCents, result.TransactionID, result.TransactionID, result.SuccessTime.UTC(), paymentID); err != nil {
			return model.CustomerShopOrder{}, err
		}
		if err := recordWechatIncomeTx(ctx, tx, tenantID, orderID, orderNo, result, 0, true); err != nil {
			return model.CustomerShopOrder{}, err
		}
		if err := s.commitInboxTx(ctx, tx, "finance"); err != nil {
			return model.CustomerShopOrder{}, err
		}
		return s.GetCustomerShopOrder(ctx, tenantID, orderID)
	}

	paidAt := result.SuccessTime.UTC()
	if paidAt.IsZero() {
		paidAt = time.Now().UTC()
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE fin_payment_transactions
		SET paid_amount_cents=?, input_amount_cents=?, status='paid', failure_reason='',
		    external_trade_no=?, provider_transaction_id=?, provider_trade_state='SUCCESS',
		    paid_at=?, notified_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND status IN ('pending','closed')
	`, result.AmountCents, result.AmountCents, result.TransactionID,
		result.TransactionID, paidAt, paymentID,
	); err != nil {
		return model.CustomerShopOrder{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE biz_orders
		SET status='paid', paid_amount_cents=?, paid_at=?
		WHERE id=? AND status='pending'
	`, result.AmountCents, paidAt, orderID); err != nil {
		return model.CustomerShopOrder{}, err
	}

	userID := int64(0)
	if operatorUserID.Valid {
		userID = operatorUserID.Int64
	}
	switch orderType {
	case "time_card":
		err = fulfillTimeCardOrderTx(ctx, tx, tenantID, userID, orderID, orderNo)
	case "membership":
		err = fulfillMembershipOrderTx(ctx, tx, tenantID, userID, orderID, orderNo, "wechat")
	case "device":
		err = fulfillDeviceOrderTx(ctx, tx, tenantID, userID, orderID, orderNo, false)
	default:
		err = ErrUnsupportedShopProduct
	}
	if err != nil {
		return model.CustomerShopOrder{}, err
	}
	if err := consumeMarketingCampaignOrderTx(ctx, tx, orderID); err != nil {
		return model.CustomerShopOrder{}, err
	}
	if err := accrueReferralRewardForPaidOrderTx(ctx, tx, orderID); err != nil {
		return model.CustomerShopOrder{}, err
	}

	if err := recordWechatIncomeTx(ctx, tx, tenantID, orderID, orderNo, result, userID, false); err != nil {
		return model.CustomerShopOrder{}, err
	}
	if err := recordWechatReceiptTx(ctx, tx, tenantID, orderID, paymentID, userID, result); err != nil {
		return model.CustomerShopOrder{}, err
	}

	if err := s.commitInboxTx(ctx, tx, "finance", "sales"); err != nil {
		return model.CustomerShopOrder{}, err
	}
	return s.GetCustomerShopOrder(ctx, tenantID, orderID)
}

// A cryptographically verified provider payment is an automatically posted
// platform receipt, not a customer-submitted proof or human self-approval.
// It qualifies the customer and updates finance without crediting cash again.
func recordWechatReceiptTx(ctx context.Context, tx *sql.Tx, tenantID, orderID, paymentID, userID int64, result model.WechatPaymentResult) error {
	return recordWechatPlatformReceiptTx(ctx, tx, tenantID, &orderID, nil, paymentID, userID, result)
}

func recordWechatPlatformReceiptTx(ctx context.Context, tx *sql.Tx, tenantID int64, orderID, rechargeID *int64, paymentID, userID int64, result model.WechatPaymentResult) error {
	no, err := newFinanceReference("RCPT")
	if err != nil {
		return err
	}
	input := model.CustomerReceiptInput{
		TenantID: tenantID, Channel: "platform", Purpose: "platform_payment", AmountCents: result.AmountCents,
		OrderID: orderID, VerifiedPaymentID: &paymentID, ExternalTradeNo: result.TransactionID,
		PayerName: "微信付款人（平台核验）", ReceivingAccount: "微信商户 " + result.MchID,
		Evidence:   "微信支付官方SDK验签解密/主动查单核验：商户、AppID、支付流水、金额和付款人均与支付快照一致",
		OccurredAt: result.SuccessTime.UTC(), IdempotencyKey: "wechat-settlement:" + result.TransactionID,
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO fin_customer_receipts (
		receipt_no,tenant_id,channel,purpose,amount_cents,order_id,recharge_id,verified_payment_id,payment_id,
		payer_name,receiving_account,external_trade_no,evidence,occurred_at,status,requester_user_id,
		last_submitter_user_id,request_hash,review_note,reviewed_at,posted_at,idempotency_key,evidence_key)
		VALUES (?,?,'platform','platform_payment',?,?,?,?,?,?,?,?,?,?,'posted',?,?,?,
		'微信平台已核验到账，系统自动入账；非人工审核',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3),?,?)`,
		no, tenantID, result.AmountCents, orderID, rechargeID, paymentID, paymentID, input.PayerName, input.ReceivingAccount,
		result.TransactionID, input.Evidence, input.OccurredAt, userID, userID, receiptRequestHash(input), input.IdempotencyKey, receiptEvidenceKey(input))
	if err != nil {
		return err
	}
	receiptID, err := r.LastInsertId()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO fin_customer_receipt_events (receipt_id,actor_user_id,action,note)
		VALUES (?,0,'provider_verified','微信服务端核验成功，订单、权益、经营收款与客户认定同事务入账')`, receiptID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO fin_customer_confirmations (tenant_id,receipt_id,reviewer_user_id,confirmed_amount_cents)
		VALUES (?,?,0,?) ON DUPLICATE KEY UPDATE tenant_id=fin_customer_confirmations.tenant_id`, tenantID, receiptID, result.AmountCents); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE crm_sales_leads SET status='won',stage='won' WHERE converted_tenant_id=? AND status='registered'`, tenantID)
	return err
}

func recordWechatIncomeTx(ctx context.Context, tx *sql.Tx, tenantID, orderID int64, orderNo string, result model.WechatPaymentResult, userID int64, unapplied bool) error {
	var counterparty string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM mgmt_tenants WHERE id=?`, tenantID).Scan(&counterparty); err != nil {
		return err
	}
	category, reason := "customer_order_payment", "微信支付商城订单 · "+orderNo
	if unapplied {
		category, reason = "unapplied_wechat_payment", "微信已收款但订单状态异常，请财务核查 · "+orderNo
	}
	return insertOperatingEntryTx(ctx, tx, "income", category, result.AmountCents, "shop_order", &orderID,
		orderNo, "wechat_payment:"+result.TransactionID, counterparty, "wechat_jsapi", reason, userID, result.SuccessTime.UTC())
}

// All callers hold the order lock before this check, matching payment settlement
// lock order. A network timeout is not proof that WeChat did not receive money.
func ensureNoWechatPendingTx(ctx context.Context, tx *sql.Tx, orderID int64) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM fin_payment_transactions
		WHERE order_id=? AND channel='wechat' AND status='pending'`, orderID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrWechatPaymentInProgress
	}
	return nil
}

func (s *Store) GetPendingWechatPayment(ctx context.Context, tenantID, orderID int64) (model.WechatPaymentPreparation, error) {
	var item model.WechatPaymentPreparation
	err := s.db.QueryRowContext(ctx, `SELECT id,payment_no,order_id,order_no,expires_at
		FROM fin_payment_transactions WHERE tenant_id=? AND order_id=? AND channel='wechat' AND status='pending'
		ORDER BY id DESC LIMIT 1`, tenantID, orderID).Scan(&item.PaymentID, &item.PaymentNo, &item.OrderID, &item.OrderNo, &item.ExpiresAt)
	return item, err
}

func (s *Store) ListPendingWechatPayments(ctx context.Context) ([]model.WechatPaymentPreparation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,payment_no,order_id,order_no,expires_at
		FROM fin_payment_transactions WHERE channel='wechat' AND status='pending' AND created_at<UTC_TIMESTAMP(3)-INTERVAL 30 SECOND
		ORDER BY COALESCE(provider_checked_at,created_at) ASC,id ASC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.WechatPaymentPreparation, 0)
	for rows.Next() {
		var item model.WechatPaymentPreparation
		if err := rows.Scan(&item.PaymentID, &item.PaymentNo, &item.OrderID, &item.OrderNo, &item.ExpiresAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
