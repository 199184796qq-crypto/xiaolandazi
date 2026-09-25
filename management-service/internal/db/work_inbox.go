package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/model"
	"net/url"
	"strings"
)

type inboxQuery struct {
	group                                                           model.InboxGroup
	source, from, where, id, reference, title, status, created, due string
	args                                                            []any
}

// Every predicate is server-owned. Visibility alone never gives a actionable badge.
// Shared queues are counted per eligible person, not assigned to every person.
func inboxQueries(sc model.InboxScope) []inboxQuery {
	out := []inboxQuery{}
	if !model.WorkInboxAvailable(sc.Actor.Role) {
		return out
	}
	internal := sc.Actor.IsInternalStaff()
	has := func(codes ...string) bool {
		if !internal {
			return false
		}
		if sc.Actor.IsPlatformAdmin() || sc.Access.IsSuperAdmin {
			return true
		}
		for _, code := range codes {
			found := false
			for _, p := range sc.Access.Permissions {
				if p == code || p == "*" {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	add := func(key, topic, dept, category, title, to, source, from, where, id, ref, name, status, created, due string, shared bool, args ...any) {
		out = append(out, inboxQuery{group: model.InboxGroup{Key: key, Topic: topic, Department: dept, Category: category, Title: title, To: to, SharedQueue: shared}, source: source, from: from, where: where, id: id, reference: ref, title: name, status: status, created: created, due: due, args: args})
	}
	if has("finance.dashboard.view", "finance.recharge.approve") {
		where := "r.status IN ('pending','posting_failed')"
		args := []any{}
		if sc.RequireDistinctReviewer {
			where += " AND r.requester_user_id<>? AND r.last_submitter_user_id<>?"
			args = append(args, sc.Actor.UserID, sc.Actor.UserID)
		}
		add("receipt_review", "finance", "财务", "review", "客户收款确认", "/staff/finance/receipts", "receipt", "fin_customer_receipts r", where, "r.id", "r.receipt_no", "r.receipt_no", "r.status", "r.created_at", "NULL", true, args...)
	}
	for _, op := range []struct{ code, permission, label string }{{"finance.recharge", "finance.recharge.approve", "充值申请"}, {"finance.refund", "finance.refund.approve", "退款申请"}, {"finance.reward", "finance.reward.approve", "奖励申请"}, {"finance.ai_time_grant", "finance.ai_time.approve", "时长申请"}} {
		if !has("finance.dashboard.view", op.permission) {
			continue
		}
		where := "t.status='pending' AND t.operation_code=?"
		args := []any{op.code}
		if sc.RequireDistinctReviewer {
			where += " AND t.requester_user_id<>?"
			args = append(args, sc.Actor.UserID)
		}
		add(strings.ReplaceAll(op.code, ".", "_"), "finance", "财务", "review", op.label, "/staff/finance/approvals", "approval", "staff_approval_tasks t", where, "t.id", "CAST(t.id AS CHAR)", "t.operation_code", "t.status", "t.created_at", "NULL", true, args...)
	}
	customerScope := model.CustomerBusinessScope{UserID: sc.Actor.UserID, Role: sc.Actor.Role}
	if sc.Actor.TenantID != nil {
		customerScope.TenantID = *sc.Actor.TenantID
	}
	if sc.Actor.Role == "customer" || sc.Actor.IsSalesStaff() {
		w, a := customerScopeSQL(customerScope, "r.tenant_id")
		path := "/finance/receipts"
		dept := "客户服务"
		if sc.Actor.IsSalesStaff() {
			path = "/sales/receipts"
			dept = "客资销售"
		}
		add("receipt_supplement", "finance", dept, "supplement", "收款资料待补充", path, "receipt", "fin_customer_receipts r", "r.status='needs_info' AND ("+w+")", "r.id", "r.receipt_no", "r.receipt_no", "r.status", "r.created_at", "NULL", false, a...)
	}
	ops := has("liveops.configure") || has("liveops.ticket.manage")
	if ops && !sc.Actor.IsSalesStaff() {
		add("support_accept", "support", "营销运维", "accept", "协助工单待接单", "/operations/support", "support", "crm_support_tickets k", "k.status='pending'", "k.id", "k.ticket_no", "k.title", "k.status", "k.created_at", "NULL", true)
		w := "k.status IN ('accepted','in_progress') AND k.assigned_user_id=?"
		args := []any{sc.Actor.UserID}
		if has("liveops.ticket.manage") {
			w = "k.status IN ('accepted','in_progress')"
			args = nil
		}
		add("support_process", "support", "营销运维", "process", "协助工单待处理", "/operations/support", "support", "crm_support_tickets k", w, "k.id", "k.ticket_no", "k.title", "k.status", "k.created_at", "NULL", false, args...)
	} else if sc.Actor.Role == "customer" || sc.Actor.IsSalesStaff() {
		w, a := customerScopeSQL(customerScope, "k.tenant_id")
		path, dept := "/support", "客户服务"
		if sc.Actor.IsSalesStaff() {
			path, dept = "/sales/support", "客资销售"
		}
		add("support_feedback", "support", dept, "supplement", "协助工单待反馈", path, "support", "crm_support_tickets k", "k.status='waiting_customer' AND ("+w+")", "k.id", "k.ticket_no", "k.title", "k.status", "k.created_at", "NULL", false, a...)
		add("support_confirm", "support", dept, "confirm", "处理结果待确认", path, "support", "crm_support_tickets k", "k.status='awaiting_confirmation' AND ("+w+")", "k.id", "k.ticket_no", "k.title", "k.status", "k.created_at", "NULL", false, a...)
	}
	if has("inventory.after_sales.view", "inventory.after_sales.manage") {
		add("repair_accept", "inventory", "仓储售后", "accept", "维修退换待受理", "/staff/after-sales", "rma", "inv_rmas r", "r.status='SUBMITTED'", "r.id", "r.rma_no", "r.rma_no", "r.status", "r.created_at", "NULL", true)
		// Courier transit and external repair are waiting on others, not local repair tasks.
		add("repair_process", "inventory", "仓储售后", "process", "维修退换待处理", "/staff/after-sales", "rma", "inv_rmas r", "r.status IN ('PROCESSING','REPAIRING')", "r.id", "r.rma_no", "r.rma_no", "r.status", "r.created_at", "NULL", true)
	}
	if has("inventory.view", "inventory.manage") {
		add("inventory_inbound", "inventory", "仓储售后", "process", "设备待入库", "/resources/inventory", "device", "inv_devices d", "d.lifecycle_status='INBOUND_PENDING'", "d.id", "d.sn", "d.sn", "d.lifecycle_status", "d.created_at", "NULL", true)
	}
	if has("inventory.view", "inventory.after_sales.view", "inventory.after_sales.manage") {
		add("inventory_scrap", "inventory", "仓储售后", "process", "报废设备待处置", "/staff/after-sales", "device", "inv_devices d", "d.lifecycle_status='SCRAP_PENDING'", "d.id", "d.sn", "d.sn", "d.lifecycle_status", "d.created_at", "NULL", true)
	}
	if has("logistics.view", "logistics.manage") {
		add("logistics_dispatch", "logistics", "仓储物流", "process", "物流待发货", "/resources/logistics", "shipment", "inv_shipments s", "s.status IN ('pending','ready_to_ship') AND s.shipment_type NOT IN ('return','rma_return','repair_return')", "s.id", "s.shipment_no", "s.shipment_no", "s.status", "s.created_at", "NULL", true)
		add("logistics_exception", "logistics", "仓储物流", "process", "物流异常待处理", "/resources/logistics", "shipment", "inv_shipments s", "s.status IN ('exception','returned')", "s.id", "s.shipment_no", "s.shipment_no", "s.status", "s.created_at", "NULL", true)
	}
	if sc.Actor.IsSalesStaff() {
		// One lead = one actionable task even if both its visit and follow-up are due.
		due := "CASE WHEN l.planned_visit_at IS NULL THEN l.next_followup_at WHEN l.next_followup_at IS NULL THEN l.planned_visit_at ELSE LEAST(l.planned_visit_at,l.next_followup_at) END"
		add("sales_lead_due", "sales", "客资销售", "process", "到期拜访与跟进", "/sales/leads", "lead", "crm_sales_leads l JOIN crm_sales_staff ss ON ss.id=l.owner_sales_staff_id", "ss.user_id=? AND ss.status='active' AND l.status='open' AND ("+due+")<=UTC_TIMESTAMP(3)", "l.id", "CAST(l.id AS CHAR)", "l.business_name", "l.status", "l.created_at", due, false, sc.Actor.UserID)
		w, a := customerScopeSQL(customerScope, "f.tenant_id")
		add("sales_contact_due", "sales", "客资销售", "process", "到期跟进与售后回访", "/sales/followups", "contact", "crm_sales_followups f", "f.next_followup_at<=UTC_TIMESTAMP(3) AND NOT EXISTS(SELECT 1 FROM crm_sales_followups newer WHERE newer.tenant_id=f.tenant_id AND newer.id>f.id) AND ("+w+")", "f.id", "CAST(f.tenant_id AS CHAR)", "CONCAT('客户 #',f.tenant_id)", "'due'", "f.created_at", "f.next_followup_at", false, a...)
	}
	return out
}

// Identical authorized SQL and result metadata share a cache. A user ID remains
// in the key whenever the real predicate depends on that person's assignment or self-review.
func InboxCacheIdentity(sc model.InboxScope, topic string) string {
	parts := []any{}
	for _, q := range inboxQueries(sc) {
		if q.group.Topic == topic {
			parts = append(parts, []any{q.group, q.from, q.where, q.due, q.args})
		}
	}
	raw, _ := json.Marshal(parts)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func InboxTopics(sc model.InboxScope) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, q := range inboxQueries(sc) {
		if !seen[q.group.Topic] {
			seen[q.group.Topic] = true
			out = append(out, q.group.Topic)
		}
	}
	return out
}
func (s *Store) InboxTopic(ctx context.Context, sc model.InboxScope, topic string) ([]model.InboxGroup, error) {
	groups := []model.InboxGroup{}
	queries := []string{}
	args := []any{}
	for _, q := range inboxQueries(sc) {
		if q.group.Topic != topic {
			continue
		}
		groups = append(groups, q.group)
		queries = append(queries, "SELECT ?,COUNT(*),COALESCE(SUM("+q.due+" < UTC_TIMESTAMP(3)),0) FROM "+q.from+" WHERE "+q.where)
		args = append(args, q.group.Key)
		args = append(args, q.args...)
	}
	if len(queries) == 0 {
		return groups, nil
	}
	rows, err := s.db.QueryContext(ctx, strings.Join(queries, " UNION ALL "), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	indexes := map[string]int{}
	for i, group := range groups {
		indexes[group.Key] = i
	}
	seen := map[string]bool{}
	for rows.Next() {
		var key string
		var count, due int64
		if err = rows.Scan(&key, &count, &due); err != nil {
			return nil, err
		}
		i, ok := indexes[key]
		if !ok || seen[key] || count < 0 || due < 0 {
			return nil, fmt.Errorf("inbox result mismatch")
		}
		seen[key] = true
		groups[i].Count, groups[i].DueCount = count, due
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(seen) != len(groups) {
		return nil, fmt.Errorf("inbox incomplete result")
	}
	return groups, nil
}
func (s *Store) InboxItems(ctx context.Context, sc model.InboxScope, key string, page, size int, dueOnly bool) (model.InboxPage, error) {
	var selected *inboxQuery
	for _, q := range inboxQueries(sc) {
		if q.group.Key == key {
			copy := q
			selected = &copy
			break
		}
	}
	if selected == nil {
		return model.InboxPage{}, sql.ErrNoRows
	}
	q := *selected
	page, size = businessPage(page, size)
	if dueOnly {
		q.where += " AND " + q.due + " < UTC_TIMESTAMP(3)"
	}
	result := model.InboxPage{Group: q.group, Items: []model.InboxItem{}, Page: page, PageSize: size}
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+q.from+" WHERE "+q.where, q.args...).Scan(&result.Total); err != nil {
		return result, err
	}
	result.TotalPages = (result.Total + int64(size) - 1) / int64(size)
	if result.TotalPages < 1 {
		result.TotalPages = 1
	}
	query := "SELECT " + strings.Join([]string{q.id, q.reference, q.title, q.status, q.created, q.due}, ",") + " FROM " + q.from + " WHERE " + q.where + " ORDER BY " + q.created + " ASC," + q.id + " ASC LIMIT ? OFFSET ?"
	args := append(append([]any{}, q.args...), size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item model.InboxItem
		var due sql.NullTime
		if err = rows.Scan(&item.ID, &item.Reference, &item.Title, &item.Status, &item.CreatedAt, &due); err != nil {
			return result, err
		}
		item.Key = fmt.Sprintf("%s:%d", q.source, item.ID)
		if q.source == "approval" {
			item.Title = q.group.Title + " #" + item.Reference
		}
		item.To = q.group.To
		if due.Valid {
			t := due.Time.UTC()
			item.DueAt = &t
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

// Explicit module links are not mutations and cannot bypass the module's own authorization.
func InboxListLink(key string) string { return "/work/inbox?group=" + url.QueryEscape(key) }
