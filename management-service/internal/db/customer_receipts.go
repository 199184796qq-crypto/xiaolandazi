package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/model"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

func ValidateCustomerReceipt(p *model.CustomerReceiptInput) error {
	p.Channel = strings.TrimSpace(p.Channel)
	p.Purpose = strings.TrimSpace(p.Purpose)
	p.PayerName = strings.TrimSpace(p.PayerName)
	p.ReceivingAccount = strings.TrimSpace(p.ReceivingAccount)
	p.ExternalTradeNo = strings.TrimSpace(p.ExternalTradeNo)
	p.Evidence = strings.TrimSpace(p.Evidence)
	p.IdempotencyKey = strings.TrimSpace(p.IdempotencyKey)
	p.OccurredAt = p.OccurredAt.UTC().Truncate(time.Millisecond)
	if p.TenantID <= 0 || p.AmountCents == 0 || p.AmountCents > 100000000000 {
		return errors.New("客户或收款金额无效（金额必须为正数）")
	}
	if p.Channel != "bank_transfer" && p.Channel != "offline" && p.Channel != "platform" {
		return errors.New("请选择转账、线下收款或平台支付")
	}
	for _, f := range []struct {
		s   string
		max int
	}{{p.PayerName, 128}, {p.ReceivingAccount, 128}, {p.ExternalTradeNo, 128}, {p.Evidence, 2000}, {p.IdempotencyKey, 128}} {
		if f.s == "" || utf8.RuneCountInString(f.s) > f.max {
			return errors.New("付款方、收款账户标识、流水号、凭据说明和提交标识必须填写且不能超长")
		}
	}
	if p.OccurredAt.IsZero() || p.OccurredAt.After(time.Now().Add(5*time.Minute)) {
		return errors.New("请填写有效的收款发生时间")
	}
	if strings.HasPrefix(strings.ToUpper(p.ExternalTradeNo), "SIM-") || strings.HasPrefix(strings.ToUpper(p.ExternalTradeNo), "PAY-SIM-") {
		return errors.New("模拟流水不能用于真实客户认定")
	}
	switch p.Purpose {
	case "recharge":
		if p.OrderID != nil || p.VerifiedPaymentID != nil || p.ExistingRechargeID != nil || p.Channel == "platform" {
			return errors.New("充值入账参数不匹配")
		}
	case "order":
		if p.OrderID == nil || *p.OrderID <= 0 || p.VerifiedPaymentID != nil || p.ExistingRechargeID != nil || p.Channel == "platform" {
			return errors.New("线下订单收款必须关联有效订单")
		}
	case "platform_payment":
		if p.VerifiedPaymentID == nil || *p.VerifiedPaymentID <= 0 || p.ExistingRechargeID != nil || p.Channel != "platform" {
			return errors.New("平台收款必须关联服务端已核验支付记录")
		}
	case "existing_recharge":
		if p.ExistingRechargeID == nil || *p.ExistingRechargeID <= 0 || p.OrderID != nil || p.VerifiedPaymentID != nil || p.Channel == "platform" {
			return errors.New("请关联已入账的充值单，避免再次充值")
		}
	default:
		return errors.New("收款用途无效")
	}
	return nil
}
func receiptEvidenceKey(p model.CustomerReceiptInput) string {
	v := sha256.Sum256([]byte(p.Channel + "\x00" + strings.ToLower(strings.TrimSpace(p.ExternalTradeNo))))
	return hex.EncodeToString(v[:])
}

func receiptRequestHash(p model.CustomerReceiptInput) string {
	raw, _ := json.Marshal(p)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

const receiptSelect = `SELECT r.id,r.receipt_no,r.tenant_id,t.name,r.channel,r.purpose,r.amount_cents,r.order_id,r.verified_payment_id,r.existing_recharge_id,r.payer_name,r.receiving_account,r.external_trade_no,r.evidence,r.occurred_at,r.status,r.requester_user_id,r.reviewer_user_id,r.review_note,r.reviewed_at,r.posted_at,r.recharge_id,r.payment_id,r.version_no,r.idempotency_key,r.last_submitter_user_id,r.request_hash,r.created_at FROM fin_customer_receipts r JOIN mgmt_tenants t ON t.id=r.tenant_id`

func scanReceipt(q interface{ Scan(...any) error }) (model.CustomerReceipt, error) {
	var v model.CustomerReceipt
	err := q.Scan(&v.ID, &v.ReceiptNo, &v.TenantID, &v.CustomerName, &v.Channel, &v.Purpose, &v.AmountCents, &v.OrderID, &v.VerifiedPaymentID, &v.ExistingRechargeID, &v.PayerName, &v.ReceivingAccount, &v.ExternalTradeNo, &v.Evidence, &v.OccurredAt, &v.Status, &v.RequesterUserID, &v.ReviewerUserID, &v.ReviewNote, &v.ReviewedAt, &v.PostedAt, &v.RechargeID, &v.PaymentID, &v.Version, &v.IdempotencyKey, &v.LastSubmitterUserID, &v.RequestHash, &v.CreatedAt)
	return v, err
}
func (s *Store) GetCustomerReceipt(ctx context.Context, sc model.CustomerBusinessScope, id int64) (model.CustomerReceipt, error) {
	w, a := customerScopeSQL(sc, "r.tenant_id")
	a = append([]any{id}, a...)
	return scanReceipt(s.db.QueryRowContext(ctx, receiptSelect+" WHERE r.id=? AND ("+w+")", a...))
}
func (s *Store) SubmitCustomerReceipt(ctx context.Context, sc model.CustomerBusinessScope, p model.CustomerReceiptInput) (model.CustomerReceipt, error) {
	if err := ValidateCustomerReceipt(&p); err != nil {
		return model.CustomerReceipt{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CustomerReceipt{}, err
	}
	defer tx.Rollback()
	if err = lockCustomerBusinessWriter(ctx, tx, sc); err != nil {
		return model.CustomerReceipt{}, err
	}
	if err = checkCustomerBusinessAccess(ctx, tx, sc, p.TenantID); err != nil {
		return model.CustomerReceipt{}, err
	}
	existing, e := scanReceipt(tx.QueryRowContext(ctx, receiptSelect+" WHERE r.requester_user_id=? AND r.idempotency_key=?", sc.UserID, p.IdempotencyKey))
	if e == nil {
		if existing.RequestHash != receiptRequestHash(p) {
			return model.CustomerReceipt{}, ErrCustomerBusinessConflict
		}
		return existing, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return model.CustomerReceipt{}, e
	}
	if p.OrderID != nil {
		var id int64
		if err = tx.QueryRowContext(ctx, "SELECT id FROM biz_orders WHERE id=? AND tenant_id=?", p.OrderID, p.TenantID).Scan(&id); err != nil {
			return model.CustomerReceipt{}, err
		}
	}
	if p.ExistingRechargeID != nil {
		var id int64
		if err = tx.QueryRowContext(ctx, "SELECT id FROM fin_recharge_orders WHERE id=? AND tenant_id=?", p.ExistingRechargeID, p.TenantID).Scan(&id); err != nil {
			return model.CustomerReceipt{}, err
		}
	}
	// Recheck stored provider evidence; a browser success flag is never accepted.
	if p.Purpose == "platform_payment" {
		if _, err = verifiedReceiptPaymentTx(ctx, tx, p); err != nil {
			return model.CustomerReceipt{}, err
		}
	}
	no, err := newFinanceReference("RCPT")
	if err != nil {
		return model.CustomerReceipt{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO fin_customer_receipts(receipt_no,tenant_id,channel,purpose,amount_cents,order_id,verified_payment_id,existing_recharge_id,payer_name,receiving_account,external_trade_no,evidence,occurred_at,requester_user_id,idempotency_key,evidence_key,last_submitter_user_id,request_hash) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, no, p.TenantID, p.Channel, p.Purpose, p.AmountCents, p.OrderID, p.VerifiedPaymentID, p.ExistingRechargeID, p.PayerName, p.ReceivingAccount, p.ExternalTradeNo, p.Evidence, p.OccurredAt.UTC(), sc.UserID, p.IdempotencyKey, receiptEvidenceKey(p), sc.UserID, receiptRequestHash(p))
	if err != nil {
		return model.CustomerReceipt{}, normalizeDuplicate(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.CustomerReceipt{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO fin_customer_receipt_events(receipt_id,actor_user_id,action,note) VALUES(?,?,'submitted','收款资料已提交，等待财务核实')", id, sc.UserID); err != nil {
		return model.CustomerReceipt{}, err
	}
	v, err := scanReceipt(tx.QueryRowContext(ctx, receiptSelect+" WHERE r.id=?", id))
	if err != nil {
		return v, err
	}
	if err = s.commitInboxTx(ctx, tx, "finance"); err != nil {
		return v, err
	}
	return v, nil
}
func verifiedReceiptPaymentTx(ctx context.Context, tx *sql.Tx, p model.CustomerReceiptInput) (int64, error) {
	var id, order int64
	var amount uint64
	var channel, trade, currency, state string
	err := tx.QueryRowContext(ctx, `SELECT id,order_id,paid_amount_cents,channel,external_trade_no,currency,status FROM fin_payment_transactions WHERE id=? AND tenant_id=? FOR UPDATE`, p.VerifiedPaymentID, p.TenantID).Scan(&id, &order, &amount, &channel, &trade, &currency, &state)
	if err != nil {
		return 0, err
	}
	if (channel != "alipay" && channel != "wechat" && channel != "wxpay") || state != "paid" || currency != "CNY" || amount != p.AmountCents || trade != p.ExternalTradeNo || (p.OrderID != nil && *p.OrderID != order) {
		return 0, errors.New("不存在匹配的真实平台支付，模拟/失败/金额不符的记录不能审核入账")
	}
	return id, nil
}
func (s *Store) ListCustomerReceipts(ctx context.Context, sc model.CustomerBusinessScope, tenant int64, search, status, channel string, page, size int) ([]model.CustomerReceipt, int64, error) {
	page, size = businessPage(page, size)
	w, args := customerScopeSQL(sc, "r.tenant_id")
	w = " WHERE (" + w + ")"
	if tenant > 0 {
		w += " AND r.tenant_id=?"
		args = append(args, tenant)
	}
	if status != "" && status != "all" {
		w += " AND r.status=?"
		args = append(args, status)
	}
	if channel != "" && channel != "all" {
		w += " AND r.channel=?"
		args = append(args, channel)
	}
	if search != "" {
		w += " AND (r.receipt_no LIKE ? ESCAPE '!' OR t.name LIKE ? ESCAPE '!')"
		args = append(args, businessLike(search), businessLike(search))
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fin_customer_receipts r JOIN mgmt_tenants t ON t.id=r.tenant_id"+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, receiptSelect+w+" ORDER BY r.created_at DESC,r.id DESC LIMIT ? OFFSET ?", append(append([]any{}, args...), size, int64(page-1)*int64(size))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []model.CustomerReceipt{}
	for rows.Next() {
		v, e := scanReceipt(rows)
		if e != nil {
			return nil, 0, e
		}
		items = append(items, v)
	}
	return items, total, rows.Err()
}
func (s *Store) ReviewCustomerReceipt(ctx context.Context, sc model.CustomerBusinessScope, id int64, version int, action, note string) (model.CustomerReceipt, error) {
	if !sc.Finance {
		return model.CustomerReceipt{}, sql.ErrNoRows
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CustomerReceipt{}, err
	}
	defer tx.Rollback()
	policy, err := loadFinanceReviewPolicy(ctx, tx, true)
	if err != nil {
		return model.CustomerReceipt{}, err
	}
	v, err := scanReceipt(tx.QueryRowContext(ctx, receiptSelect+" WHERE r.id=? FOR UPDATE", id))
	if err != nil {
		return v, err
	}
	if policy.BlocksReviewer(sc.UserID, v.RequesterUserID, v.LastSubmitterUserID) {
		return v, ErrFinanceDistinctReviewer
	}
	if v.Status == "posted" && action == "post" {
		return v, nil
	}
	if v.Version != version || (v.Status != "pending" && v.Status != "posting_failed") {
		return v, ErrCustomerBusinessConflict
	}
	note = strings.TrimSpace(note)
	if note == "" || utf8.RuneCountInString(note) > 1000 {
		return v, errors.New("请填写审核说明（最多1000字）")
	}
	state := ""
	switch action {
	case "reject":
		state = "rejected"
	case "needs_info":
		state = "needs_info"
	case "post":
		state = "posted"
	default:
		return v, errors.New("审核操作无效")
	}
	if action == "post" {
		if err = postCustomerReceiptTx(ctx, tx, &v, sc.UserID); err != nil {
			_ = tx.Rollback()
			// Persist a retryable task only after an explicit business transaction rollback.
			_, _ = s.db.ExecContext(ctx, "UPDATE fin_customer_receipts SET status='posting_failed',review_note='入账未完成，请财务检查收款与订单后重试',version_no=version_no+1 WHERE id=? AND version_no=? AND status IN ('pending','posting_failed')", id, version)
			return v, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO fin_customer_confirmations(tenant_id,receipt_id,reviewer_user_id,confirmed_amount_cents) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE tenant_id=fin_customer_confirmations.tenant_id`, v.TenantID, v.ID, sc.UserID, v.AmountCents); err != nil {
			return v, err
		}
		// The conversion record becomes won only after audited posting; never creates support work.
		if _, err = tx.ExecContext(ctx, "UPDATE crm_sales_leads SET status='won',stage='won' WHERE converted_tenant_id=? AND status='registered'", v.TenantID); err != nil {
			return v, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE fin_customer_receipts SET status=?,review_note=?,reviewer_user_id=?,reviewed_at=UTC_TIMESTAMP(3),posted_at=CASE WHEN ?='posted' THEN UTC_TIMESTAMP(3) ELSE NULL END,recharge_id=?,payment_id=?,version_no=version_no+1 WHERE id=?`, state, note, sc.UserID, state, v.RechargeID, v.PaymentID, id)
	if err != nil {
		return v, err
	}
	policyNote := "审核规则：强制经办与审核分离"
	if !policy.RequireDistinctReviewer {
		policyNote = "审核规则：不强制分人，按审核权限办理"
		if v.RequesterUserID == sc.UserID || v.LastSubmitterUserID == sc.UserID {
			policyNote += "；本次经办/补件与审核为同一人"
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO fin_customer_receipt_events(receipt_id,actor_user_id,action,note) VALUES(?,?,'review_policy',?)", id, sc.UserID, policyNote); err != nil {
		return v, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO fin_customer_receipt_events(receipt_id,actor_user_id,action,note) VALUES(?,?,?,?)", id, sc.UserID, state, note); err != nil {
		return v, err
	}
	v, err = scanReceipt(tx.QueryRowContext(ctx, receiptSelect+" WHERE r.id=?", id))
	if err != nil {
		return v, err
	}
	if err = s.commitInboxTx(ctx, tx, "finance", "inventory", "logistics", "sales"); err != nil {
		return v, err
	}
	return v, nil
}
func postCustomerReceiptTx(ctx context.Context, tx *sql.Tx, v *model.CustomerReceipt, reviewer int64) error {
	var tenant int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM mgmt_tenants WHERE id=? AND org_type='customer' FOR UPDATE", v.TenantID).Scan(&tenant); err != nil {
		return err
	}
	switch v.Purpose {
	case "recharge":
		var duplicate int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM fin_recharge_orders WHERE external_trade_no=? AND status='paid'", v.ExternalTradeNo).Scan(&duplicate); err != nil {
			return err
		}
		if duplicate > 0 {
			return errors.New("该流水已经充值，请使用关联既有充值，不能再次入账")
		}
		_, before, err := lockWalletAccountTx(ctx, tx, v.TenantID, "cash")
		if err != nil {
			return err
		}
		if before > math.MaxInt64-int64(v.AmountCents) {
			return errors.New("余额超出上限，入账未执行")
		}
		no, err := newFinanceReference("RCG")
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO fin_recharge_orders(recharge_no,tenant_id,requested_amount_cents,payment_method,status,external_trade_no,operator_user_id,idempotency_key) VALUES(?,?,?,?,'pending',?,?,?)`, no, v.TenantID, v.AmountCents, v.Channel, v.ExternalTradeNo, reviewer, "customer-receipt-"+v.ReceiptNo)
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		v.RechargeID = &id
		return applyRechargeTx(ctx, tx, v.TenantID, id, no, v.AmountCents, reviewer, "审核收款单 "+v.ReceiptNo)
	case "existing_recharge":
		var amount uint64
		var state, method string
		var id int64
		err := tx.QueryRowContext(ctx, "SELECT id,credited_amount_cents,status,payment_method FROM fin_recharge_orders WHERE id=? AND tenant_id=? FOR UPDATE", v.ExistingRechargeID, v.TenantID).Scan(&id, &amount, &state, &method)
		if err != nil {
			return err
		}
		if state != "paid" || amount != v.AmountCents || strings.Contains(strings.ToLower(method), "sandbox") || strings.Contains(strings.ToLower(method), "simulat") {
			return errors.New("原充值单未入账或金额不匹配，不能确认")
		}
		var linked int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM fin_customer_receipts WHERE recharge_id=? AND id<>?", id, v.ID).Scan(&linked); err != nil {
			return err
		}
		if linked > 0 {
			return errors.New("这笔充值已在其他收款单核实，不可重复认定")
		}
		v.RechargeID = &id
		return nil
	case "platform_payment":
		id, err := verifiedReceiptPaymentTx(ctx, tx, v.CustomerReceiptInput)
		if err != nil {
			return err
		}
		v.PaymentID = &id
		return nil
	case "order":
		return postReceiptOrderTx(ctx, tx, v, reviewer)
	default:
		return errors.New("不支持的收款用途")
	}
}

// Direct order receipts never credit a wallet. Fulfilment uses the existing order snapshots.
func postReceiptOrderTx(ctx context.Context, tx *sql.Tx, v *model.CustomerReceipt, reviewer int64) error {
	var no, kind, state, currency string
	var amount uint64
	var id int64
	err := tx.QueryRowContext(ctx, "SELECT id,order_no,order_type,status,currency,payable_amount_cents FROM biz_orders WHERE id=? AND tenant_id=? FOR UPDATE", v.OrderID, v.TenantID).Scan(&id, &no, &kind, &state, &currency, &amount)
	if err != nil {
		return err
	}
	if state != "pending" || currency != "CNY" || amount != v.AmountCents {
		return errors.New("订单状态或应付金额不匹配；已支付、取消或部分收款不能重复入账")
	}
	if err := ensureNoWechatPendingTx(ctx, tx, id); err != nil {
		return err
	}
	if kind == "device" {
		var expired int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM biz_order_devices WHERE order_id=? AND status='payment_hold' AND hold_expires_at<=UTC_TIMESTAMP(3)", id).Scan(&expired); err != nil {
			return err
		}
		if expired > 0 {
			return errors.New("订单锁库已过期，请核实订单与库存，不会重复收款")
		}
	}
	payno, err := newFinanceReference("PAY")
	if err != nil {
		return err
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO fin_payment_transactions(payment_no,tenant_id,order_id,order_no,channel,payment_method,currency,expected_amount_cents,input_amount_cents,paid_amount_cents,status,external_trade_no,operator_user_id,idempotency_key,paid_at) VALUES(?,?,?,?,?,?,'CNY',?,?,?,'paid',?,?,?,UTC_TIMESTAMP(3))`, payno, v.TenantID, id, no, v.Channel, "finance_verified", amount, amount, amount, v.ExternalTradeNo, reviewer, "receipt-"+v.ReceiptNo)
	if err != nil {
		return err
	}
	payid, err := r.LastInsertId()
	if err != nil {
		return err
	}
	v.PaymentID = &payid
	if _, err = tx.ExecContext(ctx, "UPDATE biz_orders SET status='paid',paid_amount_cents=?,paid_at=UTC_TIMESTAMP(3) WHERE id=? AND status='pending'", amount, id); err != nil {
		return err
	}
	switch kind {
	case "time_card":
		err = fulfillTimeCardOrderTx(ctx, tx, v.TenantID, reviewer, id, no)
	case "membership":
		err = fulfillMembershipOrderTx(ctx, tx, v.TenantID, reviewer, id, no, v.Channel)
	case "device":
		err = fulfillDeviceOrderTx(ctx, tx, v.TenantID, reviewer, id, no, false)
	default:
		err = ErrUnsupportedShopProduct
	}
	if err != nil {
		return err
	}
	if err := consumeMarketingCampaignOrderTx(ctx, tx, id); err != nil {
		return err
	}
	return accrueReferralRewardForPaidOrderTx(ctx, tx, id)
}
func (s *Store) ResubmitCustomerReceipt(ctx context.Context, sc model.CustomerBusinessScope, id int64, version int, evidence string) (model.CustomerReceipt, error) {
	evidence = strings.TrimSpace(evidence)
	if evidence == "" || utf8.RuneCountInString(evidence) > 2000 {
		return model.CustomerReceipt{}, errors.New("补充凭据说明不能为空，最多2000字")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CustomerReceipt{}, err
	}
	defer tx.Rollback()
	if err = lockCustomerBusinessWriter(ctx, tx, sc); err != nil {
		return model.CustomerReceipt{}, err
	}
	v, err := scanReceipt(tx.QueryRowContext(ctx, receiptSelect+" WHERE r.id=? FOR UPDATE", id))
	if err != nil {
		return v, err
	}
	if err = checkCustomerBusinessAccess(ctx, tx, sc, v.TenantID); err != nil {
		return v, err
	}
	if v.Version != version || (v.Status != "needs_info" && v.Status != "rejected") {
		return v, ErrCustomerBusinessConflict
	}
	// Immutable amount/source/beneficiary. Corrections do not turn an old approval into a different receipt.
	if _, err = tx.ExecContext(ctx, "UPDATE fin_customer_receipts SET evidence=?,last_submitter_user_id=?,status='pending',reviewer_user_id=NULL,reviewed_at=NULL,version_no=version_no+1 WHERE id=?", evidence, sc.UserID, id); err != nil {
		return v, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO fin_customer_receipt_events(receipt_id,actor_user_id,action,note) VALUES(?,?,'resubmitted',?)", id, sc.UserID, evidence); err != nil {
		return v, err
	}
	v, err = scanReceipt(tx.QueryRowContext(ctx, receiptSelect+" WHERE r.id=?", id))
	if err != nil {
		return v, err
	}
	return v, s.commitInboxTx(ctx, tx, "finance")
}
func (s *Store) ReceiptTodos(ctx context.Context, sc model.CustomerBusinessScope) (map[string]int64, error) {
	w, a := customerScopeSQL(sc, "r.tenant_id")
	out := map[string]int64{}
	var pending, failed, info int64
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(r.status='pending'),0),COALESCE(SUM(r.status='posting_failed'),0),COALESCE(SUM(r.status='needs_info'),0) FROM fin_customer_receipts r WHERE "+w, a...).Scan(&pending, &failed, &info)
	out["pending"] = pending
	out["posting_failed"] = failed
	out["needs_info"] = info
	return out, err
}
func (s *Store) ReceiptEvents(ctx context.Context, sc model.CustomerBusinessScope, id int64, page, size int) ([]model.CustomerBusinessEvent, int64, error) {
	if _, err := s.GetCustomerReceipt(ctx, sc, id); err != nil {
		return nil, 0, err
	}
	page, size = businessPage(page, size)
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fin_customer_receipt_events WHERE receipt_id=?", id).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,COALESCE(u.display_name,''),e.action,CASE WHEN e.action='resubmitted' THEN '已补充收款资料' ELSE e.note END,e.created_at FROM fin_customer_receipt_events e LEFT JOIN mgmt_users u ON u.id=e.actor_user_id WHERE receipt_id=? ORDER BY e.id DESC LIMIT ? OFFSET ?`, id, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.CustomerBusinessEvent{}
	for rows.Next() {
		var v model.CustomerBusinessEvent
		if err = rows.Scan(&v.ID, &v.ActorName, &v.Action, &v.Note, &v.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
