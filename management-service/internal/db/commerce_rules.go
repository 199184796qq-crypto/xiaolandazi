package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"
)

func ValidateCommerceRewardRule(r model.CommerceRewardRule) error {
	if r.Channel != "sales" && r.Channel != "referral" {
		return errors.New("请选择销售提成或用户分佣")
	}
	if strings.TrimSpace(r.Name) == "" || len([]rune(r.Name)) > 120 {
		return errors.New("规则名称须为1-120字")
	}
	switch r.ScopeType {
	case "default":
		if r.TargetID != 0 || r.ProductType != "" {
			return errors.New("默认规则不能指定商品")
		}
	case "category":
		if r.TargetID != 0 {
			return errors.New("类别规则不能指定ID")
		}
	case "product", "campaign":
		if r.TargetID <= 0 {
			return errors.New("请选择对应商品或活动")
		}
	default:
		return errors.New("规则范围不支持")
	}
	if r.ScopeType == "category" || r.ScopeType == "product" {
		if r.ProductType != "membership" && r.ProductType != "time_card" && r.ProductType != "device" {
			return errors.New("商品类型不支持")
		}
	} else if r.ProductType != "" {
		return errors.New("活动/默认范围不能指定商品类型")
	}
	if r.RateBPS > 10000 || r.AmountCents > 10000000000 || r.CapCents > 10000000000 || r.MinimumPaidCents > 10000000000 || r.PendingDays > 365 {
		return errors.New("金额、比例或冻结期超出范围")
	}
	switch r.Mode {
	case "off":
	case "fixed":
		if r.AmountCents == 0 {
			return errors.New("固定金额必须大于0")
		}
	case "percent":
		if r.RateBPS == 0 {
			return errors.New("比例必须大于0")
		}
	case "tier":
		if len(r.Tiers) == 0 || len(r.Tiers) > 20 {
			return errors.New("阶梯须为1-20条")
		}
		var previous uint64
		for i, t := range r.Tiers {
			if t.RateBPS == 0 || t.RateBPS > 10000 || t.MinimumPaidCents > 10000000000 || (i > 0 && t.MinimumPaidCents <= previous) {
				return errors.New("阶梯门槛须递增且比例在0-100%之间")
			}
			previous = t.MinimumPaidCents
		}
	default:
		return errors.New("计提方式不支持")
	}
	if r.ScheduleMode != "immediate" && r.ScheduleMode != "monthly" {
		return errors.New("结算周期不支持")
	}
	if r.ScheduleMode == "monthly" && (r.ReleaseDay < 1 || r.ReleaseDay > 28 || r.MonthLag < 1 || r.MonthLag > 12) {
		return errors.New("每月开放日须为1-28日，账期延后须为1-12个月")
	}
	if r.WindowEndDay != 0 {
		return errors.New("采用解冻后持续可提现，不支持限定结束日")
	}
	return nil
}

func commissionMulDiv(a, b, d uint64) uint64 {
	n := new(big.Int).SetUint64(a)
	n.Mul(n, new(big.Int).SetUint64(b))
	n.Div(n, new(big.Int).SetUint64(d))
	if !n.IsUint64() {
		return math.MaxUint64
	}
	return n.Uint64()
}
func CommerceRewardAmount(r model.CommerceRewardRule, paid uint64) uint64 {
	// No reward on free orders; fixed amount is per paid line/bundle, not per unit.
	if paid == 0 || paid < r.MinimumPaidCents {
		return 0
	}
	var amount uint64
	switch r.Mode {
	case "fixed":
		amount = r.AmountCents
	case "percent":
		amount = commissionMulDiv(paid, uint64(r.RateBPS), 10000)
	case "tier":
		for _, t := range r.Tiers {
			if paid >= t.MinimumPaidCents {
				amount = commissionMulDiv(paid, uint64(t.RateBPS), 10000)
			}
		}
	}
	if amount > paid {
		amount = paid
	}
	if r.CapCents > 0 && amount > r.CapCents {
		amount = r.CapCents
	}
	return amount
}

var commerceLocation = time.FixedZone("Asia/Shanghai", 8*3600)

func CommerceRewardAvailableAt(r model.CommerceRewardRule, paid time.Time) time.Time {
	mature := paid.AddDate(0, 0, int(r.PendingDays))
	if r.ScheduleMode != "monthly" {
		return mature.UTC()
	}
	local := paid.In(commerceLocation)
	open := time.Date(local.Year(), local.Month()+time.Month(r.MonthLag), int(r.ReleaseDay), 0, 0, 0, 0, commerceLocation)
	// If freezing ends after a release date, carry the earning to the next run.
	for open.Before(mature) {
		open = open.AddDate(0, 1, 0)
	}
	return open.UTC()
}
func CommerceWithdrawalWindowOpen(r model.CommerceRewardRule, now time.Time) bool {
	if r.WindowEndDay == 0 {
		return true
	}
	day := uint32(now.In(commerceLocation).Day())
	return day >= r.ReleaseDay && day <= r.WindowEndDay
}

func (s *Store) ListCommerceRewardRules(ctx context.Context) ([]model.CommerceRewardRule, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT v.id,v.head_id,CAST(v.config_json AS CHAR),v.status,v.created_by,v.published_by,v.created_at,v.published_at FROM fin_commerce_rule_versions v JOIN fin_commerce_rule_heads h ON h.id=v.head_id WHERE v.status='draft' OR v.id=h.published_version_id ORDER BY v.id DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	items := []model.CommerceRewardRule{}
	for rows.Next() {
		var r model.CommerceRewardRule
		var raw string
		var publisher sql.NullInt64
		var published sql.NullTime
		if e = rows.Scan(&r.ID, &r.HeadID, &raw, &r.Status, &r.CreatedBy, &publisher, &r.CreatedAt, &published); e != nil {
			return nil, e
		}
		id, head, status, creator, created := r.ID, r.HeadID, r.Status, r.CreatedBy, r.CreatedAt
		if e = json.Unmarshal([]byte(raw), &r); e != nil {
			return nil, e
		}
		r.ID = id
		r.HeadID = head
		r.Status = status
		r.CreatedBy = creator
		r.CreatedAt = created
		if publisher.Valid {
			r.PublishedBy = &publisher.Int64
		}
		if published.Valid {
			r.PublishedAt = &published.Time
		}
		items = append(items, r)
	}
	return items, rows.Err()
}
func (s *Store) SaveCommerceRewardDraft(ctx context.Context, user int64, r model.CommerceRewardRule) (int64, error) {
	if e := ValidateCommerceRewardRule(r); e != nil {
		return 0, e
	}
	r.ID = 0
	r.HeadID = 0
	r.Status = ""
	r.CreatedBy = 0
	r.PublishedBy = nil
	r.PublishedAt = nil
	r.CreatedAt = time.Time{}
	raw, e := json.Marshal(r)
	if e != nil {
		return 0, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	if e = validateCommerceTargetTx(ctx, tx, r); e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT IGNORE INTO fin_commerce_rule_heads(channel,scope_type,product_type,target_id) VALUES (?,?,?,?)`, r.Channel, r.ScopeType, r.ProductType, r.TargetID); e != nil {
		return 0, e
	}
	var head int64
	if e = tx.QueryRowContext(ctx, `SELECT id FROM fin_commerce_rule_heads WHERE channel=? AND scope_type=? AND product_type=? AND target_id=? FOR UPDATE`, r.Channel, r.ScopeType, r.ProductType, r.TargetID).Scan(&head); e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE fin_commerce_rule_versions SET status='superseded' WHERE head_id=? AND status='draft'`, head); e != nil {
		return 0, e
	}
	result, e := tx.ExecContext(ctx, `INSERT INTO fin_commerce_rule_versions(head_id,config_json,created_by) VALUES (?,CAST(? AS JSON),?)`, head, string(raw), user)
	if e != nil {
		return 0, e
	}
	id, e := result.LastInsertId()
	if e != nil {
		return 0, e
	}
	return id, tx.Commit()
}
func (s *Store) PublishCommerceRewardRule(ctx context.Context, user, id int64) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var head int64
	if e = tx.QueryRowContext(ctx, `SELECT head_id FROM fin_commerce_rule_versions WHERE id=?`, id).Scan(&head); e != nil {
		return e
	}
	var locked int64
	if e = tx.QueryRowContext(ctx, `SELECT id FROM fin_commerce_rule_heads WHERE id=? FOR UPDATE`, head).Scan(&locked); e != nil {
		return e
	}
	var status, raw string
	if e = tx.QueryRowContext(ctx, `SELECT status,CAST(config_json AS CHAR) FROM fin_commerce_rule_versions WHERE id=? FOR UPDATE`, id).Scan(&status, &raw); e != nil {
		return e
	}
	if status != "draft" {
		return errors.New("只有当前草稿可以发布")
	}
	var r model.CommerceRewardRule
	if e = json.Unmarshal([]byte(raw), &r); e != nil {
		return e
	}
	if e = ValidateCommerceRewardRule(r); e != nil {
		return e
	}
	if e = validateCommerceTargetTx(ctx, tx, r); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE fin_commerce_rule_versions SET status='superseded' WHERE head_id=? AND status='published'`, head); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE fin_commerce_rule_versions SET status='published',published_by=?,published_at=CURRENT_TIMESTAMP(3) WHERE id=?`, user, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE fin_commerce_rule_heads SET published_version_id=? WHERE id=?`, id, head); e != nil {
		return e
	}
	return tx.Commit()
}

func selectCommerceRule(rules []model.CommerceRewardRule, channel, product string, productID, campaignID int64) *model.CommerceRewardRule {
	priority := map[string]int{"default": 1, "category": 2, "product": 3, "campaign": 4}
	var selected *model.CommerceRewardRule
	for i := range rules {
		r := &rules[i]
		if r.Channel != channel {
			continue
		}
		matches := r.ScopeType == "default" || (r.ScopeType == "category" && r.ProductType == product) || (r.ScopeType == "product" && r.ProductType == product && r.TargetID == productID) || (r.ScopeType == "campaign" && r.TargetID == campaignID && campaignID > 0)
		if matches && (selected == nil || priority[r.ScopeType] > priority[selected.ScopeType]) {
			selected = r
		}
	}
	return selected
}

func snapshotCommerceRewardsTx(ctx context.Context, tx *sql.Tx, orderID int64) error {
	rows, e := tx.QueryContext(ctx, `SELECT v.id,CAST(v.config_json AS CHAR) FROM fin_commerce_rule_heads h JOIN fin_commerce_rule_versions v ON v.id=h.published_version_id ORDER BY h.id`)
	if e != nil {
		return e
	}
	rules := []model.CommerceRewardRule{}
	for rows.Next() {
		var id int64
		var raw string
		if e = rows.Scan(&id, &raw); e != nil {
			rows.Close()
			return e
		}
		var r model.CommerceRewardRule
		if e = json.Unmarshal([]byte(raw), &r); e != nil {
			rows.Close()
			return e
		}
		r.ID = id
		rules = append(rules, r)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	rows.Close()
	rows, e = tx.QueryContext(ctx, `SELECT i.id,i.product_type,i.product_id,i.product_version_id,COALESCE(s.campaign_id,0) FROM biz_order_items i LEFT JOIN mkt_campaign_order_snapshots s ON s.order_id=i.order_id WHERE i.order_id=? ORDER BY i.id`, orderID)
	if e != nil {
		return e
	}
	type line struct {
		id, productID, versionID, campaignID int64
		kind                                 string
	}
	lines := []line{}
	for rows.Next() {
		var l line
		if e = rows.Scan(&l.id, &l.kind, &l.productID, &l.versionID, &l.campaignID); e != nil {
			rows.Close()
			return e
		}
		lines = append(lines, l)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	rows.Close()
	tables := map[string]string{"membership": "catalog_membership_plan_versions", "time_card": "catalog_time_card_versions", "device": "catalog_device_versions"}
	for _, l := range lines {
		table := tables[l.kind]
		if table == "" {
			continue
		}
		var sales, referral bool
		if l.kind == "membership" {
			sales = true // Membership sales participation is controlled by explicit finance rules.
			e = tx.QueryRowContext(ctx, "SELECT participates_referral FROM catalog_membership_plan_versions WHERE id=?", l.versionID).Scan(&referral)
		} else {
			e = tx.QueryRowContext(ctx, `SELECT participates_sales_commission,participates_referral FROM `+table+` WHERE id=?`, l.versionID).Scan(&sales, &referral)
		}
		if e != nil {
			return e
		}
		for _, channel := range []string{"sales", "referral"} {
			selected := selectCommerceRule(rules, channel, l.kind, l.productID, l.campaignID)
			r := model.CommerceRewardRule{Mode: "legacy", Channel: channel}
			var version any
			if selected != nil {
				r = *selected
				version = r.ID
			}
			if selected == nil && ((channel == "sales" && !sales) || (channel == "referral" && !referral)) {
				r.Mode = "off"
			}
			raw, e := json.Marshal(r)
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO fin_order_reward_snapshots(order_id,order_item_id,channel,rule_version_id,config_json) VALUES (?,?,?,?,CAST(? AS JSON))`, orderID, l.id, channel, version, string(raw)); e != nil {
				return e
			}
		}
	}
	return nil
}

func accrueCommerceRewardsTx(ctx context.Context, tx *sql.Tx, orderID int64) error {
	var tenant int64
	var sales, referrer sql.NullInt64
	var paid uint64
	var paidAt sql.NullTime
	if e := tx.QueryRowContext(ctx, `SELECT tenant_id,sales_staff_id_snapshot,referrer_tenant_id_snapshot,paid_amount_cents,paid_at FROM biz_orders WHERE id=? FOR UPDATE`, orderID).Scan(&tenant, &sales, &referrer, &paid, &paidAt); e != nil {
		return e
	}
	if paid == 0 || !paidAt.Valid {
		return nil
	}
	rows, e := tx.QueryContext(ctx, `SELECT s.order_item_id,s.channel,CAST(s.config_json AS CHAR),i.unit_paid_price_cents,i.quantity FROM fin_order_reward_snapshots s JOIN biz_order_items i ON i.id=s.order_item_id WHERE s.order_id=? ORDER BY s.order_item_id,s.channel`, orderID)
	if e != nil {
		return e
	}
	type line struct {
		id           int64
		channel, raw string
		unit         uint64
		qty          uint32
	}
	lines := []line{}
	for rows.Next() {
		var l line
		if e = rows.Scan(&l.id, &l.channel, &l.raw, &l.unit, &l.qty); e != nil {
			rows.Close()
			return e
		}
		lines = append(lines, l)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	rows.Close()
	itemIDs := map[int64]bool{}
	for _, l := range lines {
		itemIDs[l.id] = true
	}
	for _, l := range lines {
		var r model.CommerceRewardRule
		if e = json.Unmarshal([]byte(l.raw), &r); e != nil {
			return e
		}
		beneficiary, kind := sales, "sales_staff"
		if l.channel == "referral" {
			beneficiary = referrer
			kind = customerReferralBeneficiaryType
			if beneficiary.Int64 == tenant {
				continue
			}
		}
		if !beneficiary.Valid || beneficiary.Int64 <= 0 {
			continue
		}
		linePaid := commissionMulDiv(l.unit, uint64(l.qty), 1)
		if len(itemIDs) == 1 {
			linePaid = paid
		}
		if linePaid > paid {
			linePaid = paid
		}
		amount := CommerceRewardAmount(r, linePaid)
		if amount == 0 {
			continue
		}
		open := CommerceRewardAvailableAt(r, paidAt.Time)
		status := "pending"
		available, frozen := int64(0), int64(amount)
		if !open.After(time.Now().UTC()) {
			status = "available"
			available, frozen = int64(amount), 0
		}
		snapshot := map[string]any{"commerce_rule": r, "order_item_id": l.id, "line_paid_cents": linePaid, "refund_reversal": true}
		raw, e := json.Marshal(snapshot)
		if e != nil {
			return e
		}
		external := fmt.Sprintf("COM-EARN-%d-%d-%s", orderID, l.id, l.channel)
		result, e := tx.ExecContext(ctx, `INSERT IGNORE INTO inc_earnings(external_id,beneficiary_type,beneficiary_id,earning_type,source_order_id,currency,amount_cents,quota_seconds,status,available_at,calculation_snapshot_json,idempotency_key) VALUES (?,?,?,?,?,'CNY',?,0,?,?,CAST(? AS JSON),?)`, external, kind, beneficiary.Int64, "commerce_"+l.channel, orderID, amount, status, open, string(raw), external)
		if e != nil {
			return e
		}
		count, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if count == 0 {
			continue
		}
		id, e := result.LastInsertId()
		if e != nil {
			return e
		}
		if e = applyBeneficiaryWalletDeltaTx(ctx, tx, kind, beneficiary.Int64, external+"-wallet", "commerce_accrual", "earning", &id, available, frozen, nil, r.Name); e != nil {
			return e
		}
	}
	return nil
}

func releaseCommerceEarningsTx(ctx context.Context, tx *sql.Tx, kind string, beneficiary int64) error {
	if _, err := ensureBeneficiaryWalletTx(ctx, tx, kind, beneficiary); err != nil {
		return err
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,amount_cents FROM inc_earnings WHERE beneficiary_type=? AND beneficiary_id=? AND LEFT(earning_type,9)='commerce_' AND status='pending' AND available_at<=CURRENT_TIMESTAMP(3) ORDER BY amount_cents,id FOR UPDATE`, kind, beneficiary)
	if e != nil {
		return e
	}
	type entry struct{ id, amount int64 }
	items := []entry{}
	for rows.Next() {
		var x entry
		if e = rows.Scan(&x.id, &x.amount); e != nil {
			rows.Close()
			return e
		}
		items = append(items, x)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	rows.Close()
	for _, x := range items {
		if e = applyBeneficiaryWalletDeltaTx(ctx, tx, kind, beneficiary, fmt.Sprintf("commerce-release-%d", x.id), "commerce_release", "earning", &x.id, x.amount, -x.amount, nil, "冻结期和结算周期结束，可提现"); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE inc_earnings SET status='available' WHERE id=? AND status='pending'`, x.id); e != nil {
			return e
		}
	}
	return nil
}

func (s *Store) ReleaseCommerceEarnings(ctx context.Context) error {
	rows, e := s.db.QueryContext(ctx, `SELECT DISTINCT beneficiary_type,beneficiary_id FROM inc_earnings WHERE LEFT(earning_type,9)='commerce_' AND status='pending' AND available_at<=CURRENT_TIMESTAMP(3) LIMIT 200`)
	if e != nil {
		return e
	}
	type beneficiary struct {
		kind string
		id   int64
	}
	items := []beneficiary{}
	for rows.Next() {
		var b beneficiary
		if e = rows.Scan(&b.kind, &b.id); e != nil {
			rows.Close()
			return e
		}
		items = append(items, b)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	rows.Close()
	sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })
	for _, b := range items {
		tx, e := s.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		if _, e = ensureBeneficiaryWalletTx(ctx, tx, b.kind, b.id); e == nil {
			e = releaseCommerceEarningsTx(ctx, tx, b.kind, b.id)
		}
		if e != nil {
			tx.Rollback()
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
	}
	return nil
}
