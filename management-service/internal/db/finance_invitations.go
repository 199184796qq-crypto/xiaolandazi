package db

import (
	"context"
	"database/sql"
	"errors"
	"livecompanion/management/internal/model"
	"strings"
	"unicode/utf8"
)

var ErrFinanceInvitationAccess = errors.New("没有财务邀请记录查看权限")

// Only server-built actor/access values are accepted. No role impersonation is used.
func (s *Store) ListFinanceInvitations(ctx context.Context, actor model.Actor, access model.StaffAccessContext, search, status string, page, size int) ([]model.FinanceInvitation, int64, error) {
	if !model.CanReadFinanceInvitations(actor, access) {
		return nil, 0, ErrFinanceInvitationAccess
	}
	search = strings.TrimSpace(search)
	if utf8.RuneCountInString(search) > 160 {
		return nil, 0, errors.New("搜索内容过长")
	}
	where := " WHERE 1=1"
	args := []any{}
	if search != "" {
		where += ` AND (EXISTS (SELECT 1 FROM mgmt_users u WHERE u.id IN (r.inviter_user_id,r.referred_user_id) AND (u.display_name LIKE ? OR u.username LIKE ?)) OR EXISTS (SELECT 1 FROM iam_invite_codes c WHERE c.id=r.invite_code_id AND c.code LIKE ?))`
		value := "%" + search + "%"
		args = append(args, value, value, value)
	}
	switch status {
	case "", "all":
	case "confirmed":
		where += " AND EXISTS (SELECT 1 FROM fin_customer_confirmations f WHERE f.tenant_id=r.referred_tenant_id)"
	case "unconfirmed":
		where += " AND NOT EXISTS (SELECT 1 FROM fin_customer_confirmations f WHERE f.tenant_id=r.referred_tenant_id)"
	default:
		return nil, 0, errors.New("客户认定筛选无效")
	}
	records, total, err := s.listInvitationRecordsWhere(ctx, where, args, page, size)
	if err != nil {
		return nil, 0, err
	}
	items := make([]model.FinanceInvitation, len(records))
	if len(records) == 0 {
		return items, int64(total), nil
	}
	ids := make([]any, 0, len(records))
	placeholders := make([]string, len(records))
	positions := map[int64]int{}
	for i, v := range records {
		items[i].InvitationRecord = v
		ids = append(ids, v.ReferredTenantID)
		placeholders[i] = "?"
		positions[v.ReferredTenantID] = i
	}
	rows, err := s.db.QueryContext(ctx, "SELECT tenant_id,receipt_id,confirmed_amount_cents,confirmed_at FROM fin_customer_confirmations WHERE tenant_id IN ("+strings.Join(placeholders, ",")+")", ids...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var tenant, receipt int64
		var amount uint64
		var at sql.NullTime
		if err = rows.Scan(&tenant, &receipt, &amount, &at); err != nil {
			return nil, 0, err
		}
		i, ok := positions[tenant]
		if !ok {
			continue
		}
		items[i].ConfirmationReceiptID = &receipt
		items[i].ConfirmedAmountCents = amount
		if at.Valid {
			t := at.Time
			items[i].ConfirmedAt = &t
		}
	}
	return items, int64(total), rows.Err()
}

// The evidence is existing earnings from the referred customer's orders. Beneficiary
// and earning type are always shown; it is never assumed that every earning belongs
// to the inviter. This endpoint has no ledger writes or automatic qualification.
func (s *Store) FinanceInvitationEarnings(ctx context.Context, actor model.Actor, access model.StaffAccessContext, invitationID int64, page, size int) ([]model.FinanceInvitationEarning, int64, error) {
	if !model.CanReadFinanceInvitations(actor, access) {
		return nil, 0, ErrFinanceInvitationAccess
	}
	var tenant int64
	if err := s.db.QueryRowContext(ctx, "SELECT referred_tenant_id FROM crm_registration_referrals WHERE id=?", invitationID).Scan(&tenant); err != nil {
		return nil, 0, err
	}
	page, size = businessPage(page, size)
	from := ` FROM inc_earnings e JOIN biz_orders o ON o.id=e.source_order_id`
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*)"+from+" WHERE o.tenant_id=?", tenant).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.external_id,o.id,o.order_no,e.beneficiary_type,e.beneficiary_id,e.earning_type,e.amount_cents,e.currency,e.quota_seconds,e.status,e.program_version_id,e.rule_id,e.source_refund_id,e.reversal_of_earning_id,COALESCE(b.batch_no,''),COALESCE(b.status,''),e.created_at`+from+` LEFT JOIN inc_settlement_items si ON si.earning_id=e.id LEFT JOIN inc_settlement_batches b ON b.id=si.settlement_batch_id WHERE o.tenant_id=? ORDER BY e.id DESC LIMIT ? OFFSET ?`, tenant, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []model.FinanceInvitationEarning{}
	for rows.Next() {
		var v model.FinanceInvitationEarning
		if err = rows.Scan(&v.ID, &v.ExternalID, &v.OrderID, &v.OrderNo, &v.BeneficiaryType, &v.BeneficiaryID, &v.EarningType, &v.AmountCents, &v.Currency, &v.QuotaSeconds, &v.Status, &v.ProgramVersionID, &v.RuleID, &v.SourceRefundID, &v.ReversalOfEarningID, &v.SettlementBatchNo, &v.SettlementStatus, &v.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, v)
	}
	return items, total, rows.Err()
}
