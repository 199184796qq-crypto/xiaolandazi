package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

func normalizeBusinessPage(page, pageSize, maxPageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if maxPageSize <= 0 {
		maxPageSize = 100
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

func staffScopeSQL(scope model.StaffBusinessScope, userColumn string) (string, []any) {
	switch scope.Mode {
	case "all":
		return "1=1", nil
	case "groups":
		if len(scope.GroupIDs) == 0 {
			return "1=0", nil
		}
		placeholders := make([]string, 0, len(scope.GroupIDs))
		args := make([]any, 0, len(scope.GroupIDs))
		for _, id := range scope.GroupIDs {
			if id <= 0 {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, id)
		}
		if len(placeholders) == 0 {
			return "1=0", nil
		}
		return fmt.Sprintf(`EXISTS (
			SELECT 1
			FROM staff_employees scope_employee
			WHERE scope_employee.user_id=%s
			  AND scope_employee.employment_status='active'
			  AND scope_employee.primary_group_id IN (%s)
		)`, userColumn, strings.Join(placeholders, ",")), args
	default:
		if scope.ActorUserID <= 0 {
			return "1=0", nil
		}
		return userColumn + "=?", []any{scope.ActorUserID}
	}
}

func (s *Store) ListSalesStaffScoped(
	ctx context.Context,
	scope model.StaffBusinessScope,
	search string,
	page int,
	pageSize int,
) ([]model.SalesStaffSummary, int64, error) {
	page, pageSize = normalizeBusinessPage(page, pageSize, 100)
	scopeSQL, scopeArgs := staffScopeSQL(scope, "s.user_id")
	where := []string{"s.status='active'", "u.status='active'", scopeSQL}
	args := append([]any{}, scopeArgs...)

	search = strings.TrimSpace(search)
	if search != "" {
		like := "%" + search + "%"
		where = append(where, "(u.display_name LIKE ? OR u.username LIKE ? OR s.employee_code LIKE ? OR COALESCE(t.name,'') LIKE ?)")
		args = append(args, like, like, like, like)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM crm_sales_staff s
		INNER JOIN mgmt_users u ON u.id=s.user_id
		LEFT JOIN crm_sales_teams t ON t.id=s.team_id
		WHERE `+whereSQL,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			s.id,
			u.id,
			s.employee_code,
			u.username,
			u.display_name,
			u.phone,
			u.email,
			u.province,
			u.city,
			u.district,
			s.status,
			s.team_id,
			COALESCE(t.name, ''),
			(
				SELECT COUNT(*)
				FROM crm_customer_sales_assignments a
				WHERE a.sales_staff_id=s.id
				  AND a.status='active'
				  AND a.effective_to IS NULL
			) AS customer_count,
			u.created_at
		FROM crm_sales_staff s
		INNER JOIN mgmt_users u ON u.id=s.user_id
		LEFT JOIN crm_sales_teams t ON t.id=s.team_id
		WHERE `+whereSQL+`
		ORDER BY u.display_name ASC, s.id ASC
		LIMIT ? OFFSET ?
	`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]model.SalesStaffSummary, 0)
	for rows.Next() {
		var item model.SalesStaffSummary
		var teamID sql.NullInt64
		if err := rows.Scan(
			&item.StaffID,
			&item.UserID,
			&item.EmployeeCode,
			&item.Username,
			&item.DisplayName,
			&item.Phone,
			&item.Email,
			&item.Province,
			&item.City,
			&item.District,
			&item.Status,
			&teamID,
			&item.TeamName,
			&item.CustomerCount,
			&item.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		if teamID.Valid {
			value := teamID.Int64
			item.TeamID = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func customerScopedFromSQL() string {
	return `
		FROM mgmt_users u
		INNER JOIN mgmt_tenants customer_org ON customer_org.id=u.tenant_id
		LEFT JOIN mgmt_tenants parent ON parent.id=customer_org.parent_id
		LEFT JOIN crm_customer_profiles p ON p.tenant_id=u.tenant_id
		LEFT JOIN (
		  SELECT tenant_id, MAX(paid_at) AS last_recharge_at
		  FROM fin_recharge_orders
		  WHERE status='paid' AND paid_at IS NOT NULL
		  GROUP BY tenant_id
		) recharge ON recharge.tenant_id=u.tenant_id
		LEFT JOIN crm_registration_referrals rr ON rr.referred_user_id=u.id
		LEFT JOIN mgmt_users inviter ON inviter.id=rr.inviter_user_id
		LEFT JOIN crm_customer_sales_assignments sa
		  ON sa.tenant_id=u.tenant_id
		 AND sa.status='active'
		 AND sa.effective_to IS NULL
		LEFT JOIN crm_sales_staff ss ON ss.id=sa.sales_staff_id
		LEFT JOIN mgmt_users sales_user ON sales_user.id=ss.user_id
		LEFT JOIN live_policy_tenant_industries policy_industry
		  ON policy_industry.tenant_id=u.tenant_id
		LEFT JOIN live_policy_industries industry
		  ON industry.code=COALESCE(policy_industry.industry_code, 'general')
	`
}

func customerScopeWhere(
	scope model.StaffBusinessScope,
	selectedSalesStaffID int64,
	search string,
	status string,
	includeSearch bool,
) (string, []any) {
	scopeSQL, scopeArgs := staffScopeSQL(scope, "ss.user_id")
	where := []string{
		"u.role='customer'",
		"u.tenant_id IS NOT NULL",
		scopeSQL,
	}
	args := append([]any{}, scopeArgs...)

	if selectedSalesStaffID > 0 {
		where = append(where, "ss.id=?")
		args = append(args, selectedSalesStaffID)
	}
	if includeSearch {
		search = strings.TrimSpace(search)
		if search != "" {
			like := "%" + search + "%"
			where = append(where, `(
				u.display_name LIKE ? OR
				u.username LIKE ? OR
				u.phone LIKE ? OR
				COALESCE(parent.name,'') LIKE ? OR
				COALESCE(industry.name,'') LIKE ? OR
				COALESCE(policy_industry.industry_code,'general') LIKE ? OR
				COALESCE(inviter.display_name,'') LIKE ? OR
				COALESCE(inviter.username,'') LIKE ? OR
				COALESCE(sales_user.display_name,'') LIKE ? OR
				COALESCE(ss.employee_code,'') LIKE ?
			)`)
			for i := 0; i < 10; i++ {
				args = append(args, like)
			}
		}
		switch status {
		case "active":
			where = append(where, "u.status='active'")
		case "other":
			where = append(where, "u.status<>'active'")
		}
	}
	return strings.Join(where, " AND "), args
}

func customerOrderSQL(sortMode string) string {
	switch sortMode {
	case "created-asc":
		return "u.created_at ASC, u.id ASC"
	case "name-asc":
		return "u.display_name ASC, u.id ASC"
	case "name-desc":
		return "u.display_name DESC, u.id DESC"
	default:
		return "u.created_at DESC, u.id DESC"
	}
}

func (s *Store) ListAdminCustomersScoped(
	ctx context.Context,
	scope model.StaffBusinessScope,
	selectedSalesStaffID int64,
	search string,
	status string,
	sortMode string,
	page int,
	pageSize int,
) ([]model.AdminCustomer, int64, model.CustomerScopeSummary, error) {
	page, pageSize = normalizeBusinessPage(page, pageSize, 100)
	fromSQL := customerScopedFromSQL()
	whereSQL, args := customerScopeWhere(scope, selectedSalesStaffID, search, status, true)

	var total int64
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) "+fromSQL+" WHERE "+whereSQL,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, model.CustomerScopeSummary{}, err
	}

	summaryWhere, summaryArgs := customerScopeWhere(scope, selectedSalesStaffID, "", "all", false)
	var summary model.CustomerScopeSummary
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN u.status='active' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN parent.org_type='agent' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN p.source_type='referral' THEN 1 ELSE 0 END),0)
	`+fromSQL+" WHERE "+summaryWhere,
		summaryArgs...,
	).Scan(
		&summary.TotalCount,
		&summary.ActiveCount,
		&summary.AgentCount,
		&summary.ReferralCount,
	); err != nil {
		return nil, 0, model.CustomerScopeSummary{}, err
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			u.id,
			u.tenant_id,
			u.username,
			u.display_name,
			u.phone,
			u.email,
			u.status,
			COALESCE(p.source_type, 'unknown'),
			COALESCE(p.cooperation_status, 'cooperating'),
			COALESCE(p.cooperation_note, ''),
			p.cooperation_marked_at,
			COALESCE(p.cooperation_marked_by_user_id, 0),
			recharge.last_recharge_at,
			CASE
				WHEN COALESCE(recharge.last_recharge_at, u.created_at) <= DATE_SUB(UTC_TIMESTAMP(3), INTERVAL 90 DAY)
				THEN 1 ELSE 0
			END,
			COALESCE(parent.id, 0),
			COALESCE(parent.name, ''),
			COALESCE(parent.org_type, ''),
			COALESCE(rr.inviter_user_id, 0),
			COALESCE(inviter.username, ''),
			COALESCE(inviter.display_name, ''),
			COALESCE(sa.sales_staff_id, 0),
			COALESCE(sales_user.id, 0),
			COALESCE(ss.employee_code, ''),
			COALESCE(sales_user.username, ''),
			COALESCE(sales_user.display_name, ''),
			COALESCE(policy_industry.industry_code, 'general'),
			COALESCE(industry.name, '通用'),
			u.created_at
	`+fromSQL+`
		WHERE `+whereSQL+`
		ORDER BY `+customerOrderSQL(sortMode)+`
		LIMIT ? OFFSET ?
	`, queryArgs...)
	if err != nil {
		return nil, 0, model.CustomerScopeSummary{}, err
	}
	defer rows.Close()

	items := make([]model.AdminCustomer, 0)
	for rows.Next() {
		var item model.AdminCustomer
		if err := rows.Scan(
			&item.UserID,
			&item.TenantID,
			&item.Username,
			&item.DisplayName,
			&item.Phone,
			&item.Email,
			&item.Status,
			&item.SourceType,
			&item.CooperationStatus,
			&item.CooperationNote,
			&item.CooperationMarkedAt,
			&item.CooperationMarkedByUserID,
			&item.LastRechargeAt,
			&item.RechargeDormant90Days,
			&item.ParentOrgID,
			&item.ParentOrgName,
			&item.ParentOrgType,
			&item.InviterUserID,
			&item.InviterUsername,
			&item.InviterDisplayName,
			&item.SalesStaffID,
			&item.SalesUserID,
			&item.SalesEmployeeCode,
			&item.SalesUsername,
			&item.SalesDisplayName,
			&item.IndustryCode,
			&item.IndustryName,
			&item.CreatedAt,
		); err != nil {
			return nil, 0, model.CustomerScopeSummary{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, model.CustomerScopeSummary{}, err
	}
	return items, total, summary, nil
}

func (s *Store) AdminCustomerVisibleToScope(
	ctx context.Context,
	scope model.StaffBusinessScope,
	userID int64,
) (bool, error) {
	whereSQL, args := customerScopeWhere(scope, 0, "", "all", false)
	args = append(args, userID)
	var count int
	err := s.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) "+customerScopedFromSQL()+" WHERE "+whereSQL+" AND u.id=?",
		args...,
	).Scan(&count)
	return count > 0, err
}

func auditResultSQL(result string) string {
	switch strings.TrimSpace(result) {
	case "success":
		return "(a.result LIKE 'http_2%' OR a.result='success')"
	case "denied":
		return "a.result LIKE 'http_4%'"
	case "failed":
		return "(a.result LIKE 'http_4%' OR a.result LIKE 'http_5%' OR a.result='failed')"
	default:
		return "1=1"
	}
}

func (s *Store) ListAdminAuditsScoped(
	ctx context.Context,
	scope model.StaffBusinessScope,
	search string,
	action string,
	resultFilter string,
	source string,
	objectType string,
	roomID int64,
	tenantID int64,
	from *time.Time,
	to *time.Time,
	beforeID int64,
	limit int,
) ([]model.AdminAuditLog, int64, bool, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	scopeSQL, scopeArgs := staffScopeSQL(scope, "a.actor_user_id")
	where := []string{scopeSQL}
	args := append([]any{}, scopeArgs...)

	search = strings.TrimSpace(search)
	if search != "" {
		like := "%" + search + "%"
		where = append(where, `(
			a.actor_username LIKE ? OR
			COALESCE(a.actor_role,'') LIKE ? OR
			COALESCE(a.source,'') LIKE ? OR
			a.action LIKE ? OR
			COALESCE(a.target_username,'') LIKE ? OR
			COALESCE(a.object_type,'') LIKE ? OR
			COALESCE(a.object_id,'') LIKE ? OR
			COALESCE(a.object_name,'') LIKE ? OR
			COALESCE(a.reason,'') LIKE ? OR
			COALESCE(a.path,'') LIKE ? OR
			COALESCE(a.client_ip,'') LIKE ?
		)`)
		args = append(args, like, like, like, like, like, like, like, like, like, like, like)
	}
	action = strings.TrimSpace(action)
	if action != "" && action != "all" {
		where = append(where, "a.action=?")
		args = append(args, action)
	}
	source = strings.TrimSpace(source)
	if source != "" && source != "all" {
		where = append(where, "a.source=?")
		args = append(args, source)
	}
	objectType = strings.TrimSpace(objectType)
	if objectType != "" && objectType != "all" {
		where = append(where, "a.object_type=?")
		args = append(args, objectType)
	}
	if roomID > 0 {
		where = append(where, "a.target_room_id=?")
		args = append(args, roomID)
	}
	if tenantID > 0 {
		where = append(where, "a.target_tenant_id=?")
		args = append(args, tenantID)
	}
	if from != nil {
		where = append(where, "a.occurred_at>=?")
		args = append(args, from.UTC())
	}
	if to != nil {
		where = append(where, "a.occurred_at<=?")
		args = append(args, to.UTC())
	}
	where = append(where, auditResultSQL(resultFilter))
	if beforeID > 0 {
		where = append(where, "a.id<?")
		args = append(args, beforeID)
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit+1)
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			a.id,
			a.occurred_at,
			a.actor_user_id,
			a.actor_username,
			a.actor_role,
			a.actor_type,
			a.source,
			a.action,
			a.target_user_id,
			a.target_username,
			a.target_tenant_id,
			a.target_room_id,
			a.object_type,
			a.object_id,
			a.object_name,
			a.reason,
			COALESCE(a.before_state,''),
			COALESCE(a.after_state,''),
			a.runtime_session_id,
			a.core_boot_id,
			a.request_id,
			COALESCE(a.detail_json,''),
			a.http_method,
			a.path,
			a.client_ip,
			a.result
		FROM mgmt_admin_audit_ledger a
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY a.id DESC
		LIMIT ?
	`, queryArgs...)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()

	items := make([]model.AdminAuditLog, 0, limit)
	ids := make([]int64, 0, limit+1)
	for rows.Next() {
		var item model.AdminAuditLog
		var id int64
		if err := rows.Scan(
			&id,
			&item.OccurredAt,
			&item.ActorUserID,
			&item.ActorUsername,
			&item.ActorRole,
			&item.ActorType,
			&item.Source,
			&item.Action,
			&item.TargetUserID,
			&item.TargetUsername,
			&item.TargetTenantID,
			&item.TargetRoomID,
			&item.ObjectType,
			&item.ObjectID,
			&item.ObjectName,
			&item.Reason,
			&item.BeforeState,
			&item.AfterState,
			&item.RuntimeSessionID,
			&item.CoreBootID,
			&item.RequestID,
			&item.DetailJSON,
			&item.HTTPMethod,
			&item.Path,
			&item.ClientIP,
			&item.Result,
		); err != nil {
			return nil, 0, false, err
		}
		item.ID = fmt.Sprint(id)
		items = append(items, item)
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, false, err
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
		ids = ids[:limit]
	}
	var nextCursor int64
	if hasMore && len(ids) > 0 {
		nextCursor = ids[len(ids)-1]
	}
	return items, nextCursor, hasMore, nil
}
