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

// Converted prospect history follows the current customer assignment, while
// original ownership/authorship stays immutable for attribution and audit.
const salesLeadVisibleWhere = `((l.converted_tenant_id IS NULL AND l.owner_sales_staff_id=?) OR
 (l.converted_tenant_id IS NOT NULL AND EXISTS(SELECT 1 FROM crm_customer_sales_assignments lead_assignment
 WHERE lead_assignment.tenant_id=l.converted_tenant_id AND lead_assignment.sales_staff_id=?
 AND lead_assignment.status='active' AND lead_assignment.effective_to IS NULL)))`

func salesLeadOrderBy(sortMode string) string {
	switch sortMode {
	case "created-asc":
		return "l.created_at ASC, l.id ASC"
	case "visit-asc":
		return "(l.planned_visit_at IS NULL) ASC, l.planned_visit_at ASC, l.id DESC"
	case "followup-asc":
		return "(l.next_followup_at IS NULL) ASC, l.next_followup_at ASC, l.id DESC"
	case "updated-desc":
		return "l.updated_at DESC, l.id DESC"
	case "name-asc":
		return "l.business_name ASC, l.id DESC"
	default:
		return "l.created_at DESC, l.id DESC"
	}
}

func scanSalesLead(scanner interface{ Scan(...any) error }) (model.SalesLead, error) {
	var item model.SalesLead
	var (
		plannedVisit, nextFollowup, latestActivity, convertedAt sql.NullTime
		convertedTenantID, convertedUserID                      sql.NullInt64
	)
	err := scanner.Scan(
		&item.ID,
		&item.LeadNo,
		&item.OwnerSalesStaffID,
		&item.BusinessName,
		&item.ContactName,
		&item.Phone,
		&item.Wechat,
		&item.Email,
		&item.IndustryName,
		&item.Province,
		&item.City,
		&item.District,
		&item.Address,
		&item.SourceType,
		&item.Stage,
		&item.Status,
		&plannedVisit,
		&nextFollowup,
		&latestActivity,
		&item.LostReason,
		&convertedTenantID,
		&convertedUserID,
		&convertedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return model.SalesLead{}, err
	}
	if plannedVisit.Valid {
		value := plannedVisit.Time
		item.PlannedVisitAt = &value
	}
	if nextFollowup.Valid {
		value := nextFollowup.Time
		item.NextFollowupAt = &value
	}
	if latestActivity.Valid {
		value := latestActivity.Time
		item.LatestActivityAt = &value
	}
	if convertedTenantID.Valid {
		value := convertedTenantID.Int64
		item.ConvertedTenantID = &value
	}
	if convertedUserID.Valid {
		value := convertedUserID.Int64
		item.ConvertedUserID = &value
	}
	if convertedAt.Valid {
		value := convertedAt.Time
		item.ConvertedAt = &value
	}
	return item, nil
}

func salesLeadSelectSQL() string {
	return `
		SELECT
			l.id, l.lead_no, l.owner_sales_staff_id,
			l.business_name, l.contact_name, l.phone, l.wechat, l.email,
			l.industry_name, l.province, l.city, l.district, l.address,
			l.source_type, l.stage, l.status,
			l.planned_visit_at, l.next_followup_at, l.latest_activity_at,
			l.lost_reason, l.converted_tenant_id, l.converted_user_id,
			l.converted_at, l.created_at, l.updated_at
		FROM crm_sales_leads l
	`
}

func (s *Store) ListSalesLeadsByUser(
	ctx context.Context,
	salesUserID int64,
	search string,
	status string,
	stage string,
	sortMode string,
	page int,
	pageSize int,
) ([]model.SalesLead, int64, model.SalesLeadSummary, error) {
	staffID, err := s.SalesStaffIDByUser(ctx, salesUserID)
	if err != nil {
		return nil, 0, model.SalesLeadSummary{}, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 12
	}

	where := []string{salesLeadVisibleWhere}
	args := []any{staffID, staffID}
	search = strings.TrimSpace(search)
	if search != "" {
		like := "%" + search + "%"
		where = append(where, "(l.business_name LIKE ? OR l.contact_name LIKE ? OR l.phone LIKE ? OR l.wechat LIKE ? OR l.industry_name LIKE ? OR l.address LIKE ?)")
		args = append(args, like, like, like, like, like, like)
	}
	status = strings.TrimSpace(status)
	if status != "" && status != "all" {
		where = append(where, "l.status=?")
		args = append(args, status)
	}
	stage = strings.TrimSpace(stage)
	if stage != "" && stage != "all" {
		where = append(where, "l.stage=?")
		args = append(args, stage)
	}
	whereSQL := " WHERE " + strings.Join(where, " AND ")

	var total int64
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM crm_sales_leads l"+whereSQL,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, model.SalesLeadSummary{}, err
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(
		ctx,
		salesLeadSelectSQL()+whereSQL+" ORDER BY "+salesLeadOrderBy(sortMode)+" LIMIT ? OFFSET ?",
		queryArgs...,
	)
	if err != nil {
		return nil, 0, model.SalesLeadSummary{}, err
	}
	defer rows.Close()

	items := make([]model.SalesLead, 0)
	for rows.Next() {
		item, err := scanSalesLead(rows)
		if err != nil {
			return nil, 0, model.SalesLeadSummary{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, model.SalesLeadSummary{}, err
	}

	var summary model.SalesLeadSummary
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(status='open'), 0),
			COALESCE(SUM(status='open' AND planned_visit_at IS NOT NULL
				AND planned_visit_at < DATE_ADD(CURRENT_TIMESTAMP(3), INTERVAL 1 DAY)), 0),
			COALESCE(SUM(status='open' AND next_followup_at IS NOT NULL
				AND next_followup_at < DATE_ADD(CURRENT_TIMESTAMP(3), INTERVAL 1 DAY)), 0),
			COALESCE(SUM(EXISTS(SELECT 1 FROM fin_customer_confirmations c WHERE c.tenant_id=l.converted_tenant_id)), 0),
			COALESCE(SUM(status='lost'), 0)
		FROM crm_sales_leads l
		WHERE `+salesLeadVisibleWhere, staffID, staffID).Scan(
		&summary.TotalCount,
		&summary.OpenCount,
		&summary.VisitDueCount,
		&summary.FollowupDueCount,
		&summary.WonCount,
		&summary.LostCount,
	); err != nil {
		return nil, 0, model.SalesLeadSummary{}, err
	}

	return items, total, summary, nil
}

func (s *Store) CreateSalesLeadByUser(
	ctx context.Context,
	salesUserID int64,
	input model.SalesLeadInput,
) (model.SalesLead, error) {
	staffID, err := s.SalesStaffIDByUser(ctx, salesUserID)
	if err != nil {
		return model.SalesLead{}, err
	}
	leadNo := fmt.Sprintf("LEAD-%d", time.Now().UTC().UnixNano())
	stage := strings.TrimSpace(input.Stage)
	if stage == "" {
		if input.PlannedVisitAt != nil {
			stage = "visit_planned"
		} else {
			stage = "new"
		}
	}
	sourceType := strings.TrimSpace(input.SourceType)
	if sourceType == "" {
		sourceType = "self_developed"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SalesLead{}, err
	}
	defer tx.Rollback()
	if err := lockSalesWriterTx(ctx, tx, staffID); err != nil {
		return model.SalesLead{}, err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO crm_sales_leads (
			lead_no, owner_sales_staff_id, business_name, contact_name,
			phone, wechat, email, industry_name, province, city, district,
			address, source_type, stage, status, planned_visit_at,
			next_followup_at, latest_activity_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?, CURRENT_TIMESTAMP(3))
	`,
		leadNo,
		staffID,
		strings.TrimSpace(input.BusinessName),
		strings.TrimSpace(input.ContactName),
		strings.TrimSpace(input.Phone),
		strings.TrimSpace(input.Wechat),
		strings.TrimSpace(input.Email),
		strings.TrimSpace(input.IndustryName),
		strings.TrimSpace(input.Province),
		strings.TrimSpace(input.City),
		strings.TrimSpace(input.District),
		strings.TrimSpace(input.Address),
		sourceType,
		stage,
		input.PlannedVisitAt,
		input.NextFollowupAt,
	)
	if err != nil {
		return model.SalesLead{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.SalesLead{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crm_sales_lead_activities (
			lead_id, sales_staff_id, activity_type, outcome, content,
			occurred_at, next_followup_at
		)
		VALUES (?, ?, 'created', '', '建立意向顾客', CURRENT_TIMESTAMP(3), ?)
	`, id, staffID, input.NextFollowupAt); err != nil {
		return model.SalesLead{}, err
	}
	item, err := scanSalesLead(tx.QueryRowContext(ctx, salesLeadSelectSQL()+" WHERE l.id=? AND l.owner_sales_staff_id=?", id, staffID))
	if err != nil {
		return model.SalesLead{}, err
	}
	if err := s.commitInboxTx(ctx, tx, "sales"); err != nil {
		return model.SalesLead{}, err
	}
	return item, nil
}

func (s *Store) GetSalesLeadByUser(
	ctx context.Context,
	salesUserID int64,
	leadID int64,
) (model.SalesLead, error) {
	staffID, err := s.SalesStaffIDByUser(ctx, salesUserID)
	if err != nil {
		return model.SalesLead{}, err
	}
	return scanSalesLead(s.db.QueryRowContext(
		ctx,
		salesLeadSelectSQL()+" WHERE l.id=? AND "+salesLeadVisibleWhere+" LIMIT 1",
		leadID,
		staffID, staffID,
	))
}

func (s *Store) ListSalesLeadActivitiesByUser(
	ctx context.Context,
	salesUserID int64,
	leadID int64,
	page int,
	pageSize int,
) ([]model.SalesLeadActivity, int64, error) {
	staffID, err := s.SalesStaffIDByUser(ctx, salesUserID)
	if err != nil {
		return nil, 0, err
	}
	var owned int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM crm_sales_leads l WHERE l.id=? AND "+salesLeadVisibleWhere,
		leadID, staffID, staffID,
	).Scan(&owned); err != nil {
		return nil, 0, err
	}
	if owned != 1 {
		return nil, 0, sql.ErrNoRows
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 12
	}

	var total int64
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM crm_sales_lead_activities WHERE lead_id=?",
		leadID,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.lead_id, a.sales_staff_id,
		       COALESCE(u.display_name, u.username, ''),
		       a.activity_type, a.outcome,
		       a.content, a.occurred_at, a.next_followup_at, a.created_at
		FROM crm_sales_lead_activities a
		LEFT JOIN crm_sales_staff ss ON ss.id=a.sales_staff_id
		LEFT JOIN mgmt_users u ON u.id=ss.user_id
		WHERE a.lead_id=?
		ORDER BY a.occurred_at DESC, a.id DESC
		LIMIT ? OFFSET ?
	`, leadID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]model.SalesLeadActivity, 0)
	for rows.Next() {
		var item model.SalesLeadActivity
		var next sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.LeadID,
			&item.SalesStaffID,
			&item.SalesDisplayName,
			&item.ActivityType,
			&item.Outcome,
			&item.Content,
			&item.OccurredAt,
			&next,
			&item.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		if next.Valid {
			value := next.Time
			item.NextFollowupAt = &value
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Store) CreateSalesLeadActivityByUser(
	ctx context.Context,
	salesUserID int64,
	leadID int64,
	input model.SalesLeadActivityInput,
) (model.SalesLeadActivity, error) {
	staffID, err := s.SalesStaffIDByUser(ctx, salesUserID)
	if err != nil {
		return model.SalesLeadActivity{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SalesLeadActivity{}, err
	}
	defer tx.Rollback()

	var status string
	if err := lockSalesWriterTx(ctx, tx, staffID); err != nil {
		return model.SalesLeadActivity{}, err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT status
		FROM crm_sales_leads
		WHERE id=? AND owner_sales_staff_id=?
		FOR UPDATE
	`, leadID, staffID).Scan(&status); err != nil {
		return model.SalesLeadActivity{}, err
	}
	if status != "open" {
		return model.SalesLeadActivity{}, fmt.Errorf("lead is closed")
	}

	occurredAt := time.Now().UTC()
	if input.OccurredAt != nil {
		occurredAt = input.OccurredAt.UTC()
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO crm_sales_lead_activities (
			lead_id, sales_staff_id, activity_type, outcome, content,
			occurred_at, next_followup_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		leadID,
		staffID,
		strings.TrimSpace(input.ActivityType),
		strings.TrimSpace(input.Outcome),
		strings.TrimSpace(input.Content),
		occurredAt,
		input.NextFollowupAt,
	)
	if err != nil {
		return model.SalesLeadActivity{}, err
	}
	activityID, err := result.LastInsertId()
	if err != nil {
		return model.SalesLeadActivity{}, err
	}

	stage := strings.TrimSpace(input.Stage)
	if stage != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE crm_sales_leads
			SET stage=?, planned_visit_at=CASE WHEN ?='visit' THEN NULL ELSE planned_visit_at END, next_followup_at=?, latest_activity_at=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=? AND owner_sales_staff_id=?
		`, stage, input.ActivityType, input.NextFollowupAt, occurredAt, leadID, staffID); err != nil {
			return model.SalesLeadActivity{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE crm_sales_leads
			SET planned_visit_at=CASE WHEN ?='visit' THEN NULL ELSE planned_visit_at END, next_followup_at=?, latest_activity_at=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=? AND owner_sales_staff_id=?
		`, input.ActivityType, input.NextFollowupAt, occurredAt, leadID, staffID); err != nil {
			return model.SalesLeadActivity{}, err
		}
	}
	if err := s.commitInboxTx(ctx, tx, "sales"); err != nil {
		return model.SalesLeadActivity{}, err
	}

	var item model.SalesLeadActivity
	var next sql.NullTime
	err = s.db.QueryRowContext(ctx, `
		SELECT a.id, a.lead_id, a.sales_staff_id,
		       COALESCE(u.display_name, u.username, ''),
		       a.activity_type, a.outcome,
		       a.content, a.occurred_at, a.next_followup_at, a.created_at
		FROM crm_sales_lead_activities a
		LEFT JOIN crm_sales_staff ss ON ss.id=a.sales_staff_id
		LEFT JOIN mgmt_users u ON u.id=ss.user_id
		WHERE a.id=? AND a.sales_staff_id=?
		LIMIT 1
	`, activityID, staffID).Scan(
		&item.ID,
		&item.LeadID,
		&item.SalesStaffID,
		&item.SalesDisplayName,
		&item.ActivityType,
		&item.Outcome,
		&item.Content,
		&item.OccurredAt,
		&next,
		&item.CreatedAt,
	)
	if err != nil {
		return model.SalesLeadActivity{}, err
	}
	if next.Valid {
		value := next.Time
		item.NextFollowupAt = &value
	}
	return item, nil
}

func (s *Store) CloseSalesLeadLostByUser(
	ctx context.Context,
	salesUserID int64,
	leadID int64,
	reason string,
) (model.SalesLead, error) {
	staffID, err := s.SalesStaffIDByUser(ctx, salesUserID)
	if err != nil {
		return model.SalesLead{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SalesLead{}, err
	}
	defer tx.Rollback()
	if err := lockSalesWriterTx(ctx, tx, staffID); err != nil {
		return model.SalesLead{}, err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE crm_sales_leads
		SET status='lost', stage='lost', lost_reason=?,
		    next_followup_at=NULL, latest_activity_at=CURRENT_TIMESTAMP(3),
		    updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND owner_sales_staff_id=? AND status='open'
	`, strings.TrimSpace(reason), leadID, staffID)
	if err != nil {
		return model.SalesLead{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.SalesLead{}, err
	}
	if affected != 1 {
		return model.SalesLead{}, sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crm_sales_lead_activities (
			lead_id, sales_staff_id, activity_type, outcome, content,
			occurred_at
		)
		VALUES (?, ?, 'close', 'lost', ?, CURRENT_TIMESTAMP(3))
	`, leadID, staffID, "未成交："+strings.TrimSpace(reason)); err != nil {
		return model.SalesLead{}, err
	}
	if err := s.commitInboxTx(ctx, tx, "sales"); err != nil {
		return model.SalesLead{}, err
	}
	return s.GetSalesLeadByUser(ctx, salesUserID, leadID)
}

func (s *Store) ConvertSalesLeadByUser(
	ctx context.Context,
	salesUserID int64,
	leadID int64,
	input model.ConvertSalesLeadInput,
	passwordHash string,
) (model.SalesLead, model.User, model.CustomerHandoff, error) {
	staffID, err := s.SalesStaffIDByUser(ctx, salesUserID)
	if err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}
	platformID, err := s.PlatformOrganizationID(ctx)
	if err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}
	defer tx.Rollback()

	var leadStatus string
	if err := lockSalesWriterTx(ctx, tx, staffID); err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT status
		FROM crm_sales_leads
		WHERE id=? AND owner_sales_staff_id=?
		FOR UPDATE
	`, leadID, staffID).Scan(&leadStatus); err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}
	if leadStatus != "open" {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, fmt.Errorf("lead is closed")
	}

	tenantCode, err := randomTenantCode()
	if err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}
	displayName := strings.TrimSpace(input.DisplayName)
	result, err := tx.ExecContext(ctx, `
		INSERT INTO mgmt_tenants (
			parent_id, org_type, level, code, name, status
		)
		VALUES (?, 'customer', 1, ?, ?, 'active')
	`, platformID, tenantCode, displayName)
	if err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, normalizeDuplicate(err)
	}
	tenantID, err := result.LastInsertId()
	if err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}

	result, err = tx.ExecContext(ctx, `
		INSERT INTO mgmt_users (
			tenant_id, username, password_hash, must_change_password,
			display_name, phone, email, province, city, district, address,
			role, status
		)
		VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, 'customer', 'active')
	`,
		tenantID,
		strings.TrimSpace(input.Username),
		passwordHash,
		displayName,
		strings.TrimSpace(input.Phone),
		strings.TrimSpace(input.Email),
		strings.TrimSpace(input.Province),
		strings.TrimSpace(input.City),
		strings.TrimSpace(input.District),
		strings.TrimSpace(input.Address),
	)
	if err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, normalizeDuplicate(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}

	if err := ensureOrganizationResourceAccountsTx(ctx, tx, tenantID, "customer"); err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}
	customerTenantID := tenantID
	if err := ensureUserInviteCodeTx(ctx, tx, userID, &customerTenantID, "customer"); err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}
	// Keep original prospect developer separate from the current servicing seller.
	var developerID int64
	if err := tx.QueryRowContext(ctx, `SELECT sales_staff_id FROM crm_sales_lead_activities WHERE lead_id=? AND activity_type='created' ORDER BY id LIMIT 1`, leadID).Scan(&developerID); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
		}
		developerID = staffID
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crm_customer_profiles (
			tenant_id, source_type, source_sales_staff_id, source_note
		)
		VALUES (?, 'sales_lead', ?, ?)
	`, tenantID, developerID, "意向顾客预开户，付费认定以财务确认记录为准"); err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fin_wallet_accounts (
			tenant_id, account_type, currency, balance_cents, status
		)
		VALUES
			(?, 'cash', 'CNY', 0, 'active'),
			(?, 'reward', 'CNY', 0, 'active')
	`, tenantID, tenantID); err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crm_customer_sales_assignments (
			tenant_id, sales_staff_id, status, effective_from,
			assigned_by_user_id, reason
		)
		VALUES (?, ?, 'active', CURRENT_TIMESTAMP(3), ?, 'sales lead converted')
	`, tenantID, staffID, salesUserID); err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE crm_sales_leads
		SET status='registered', stage='registered', lost_reason='',
		    converted_tenant_id=?, converted_user_id=?, converted_at=?,
		    next_followup_at=NULL, latest_activity_at=?,
		    updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND owner_sales_staff_id=? AND status='open'
	`, tenantID, userID, now, now, leadID, staffID); err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crm_sales_lead_activities (
			lead_id, sales_staff_id, activity_type, outcome, content,
			occurred_at
		)
		VALUES (?, ?, 'conversion', 'registered', '客户预开户，待财务审核收款；未认定成交，未自动派运维', ?)
	`, leadID, staffID, now); err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}

	// Registration is not financial qualification and never creates a support assignment.
	if note := strings.TrimSpace(input.HandoffSummary); note != "" {
		if _, err = tx.ExecContext(ctx, "INSERT INTO crm_sales_lead_activities(lead_id,sales_staff_id,activity_type,outcome,content,occurred_at) VALUES(?,?,'note','registration',?,?)", leadID, staffID, note, now); err != nil {
			return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
		}
	}

	lead, err := scanSalesLead(tx.QueryRowContext(ctx, salesLeadSelectSQL()+" WHERE l.id=? AND l.owner_sales_staff_id=?", leadID, staffID))
	if err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}
	user := model.User{
		ID:                 userID,
		TenantID:           &tenantID,
		Username:           strings.TrimSpace(input.Username),
		MustChangePassword: true,
		DisplayName:        displayName,
		Phone:              strings.TrimSpace(input.Phone),
		Province:           strings.TrimSpace(input.Province),
		City:               strings.TrimSpace(input.City),
		District:           strings.TrimSpace(input.District),
		Role:               "customer",
		Status:             "active",
		CreatedAt:          now,
	}
	handoff := model.CustomerHandoff{}
	if err := s.commitInboxTx(ctx, tx, "sales", "access"); err != nil {
		return model.SalesLead{}, model.User{}, model.CustomerHandoff{}, err
	}
	return lead, user, handoff, nil
}

func scanCustomerHandoff(scanner interface{ Scan(...any) error }) (model.CustomerHandoff, error) {
	var item model.CustomerHandoff
	var leadID, acceptedBy, completedBy sql.NullInt64
	var acceptedAt, completedAt sql.NullTime
	err := scanner.Scan(
		&item.ID,
		&item.HandoffNo,
		&leadID,
		&item.TenantID,
		&item.CustomerUserID,
		&item.CustomerUsername,
		&item.CustomerName,
		&item.CustomerPhone,
		&item.SalesStaffID,
		&item.SalesDisplayName,
		&item.TargetGroupCode,
		&item.Status,
		&item.Summary,
		&acceptedBy,
		&item.AcceptedByName,
		&acceptedAt,
		&completedBy,
		&item.CompletedByName,
		&completedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return model.CustomerHandoff{}, err
	}
	if leadID.Valid {
		value := leadID.Int64
		item.LeadID = &value
	}
	if acceptedBy.Valid {
		value := acceptedBy.Int64
		item.AcceptedByUserID = &value
	}
	if completedBy.Valid {
		value := completedBy.Int64
		item.CompletedByUserID = &value
	}
	if acceptedAt.Valid {
		value := acceptedAt.Time
		item.AcceptedAt = &value
	}
	if completedAt.Valid {
		value := completedAt.Time
		item.CompletedAt = &value
	}
	return item, nil
}

func customerHandoffSelectSQL() string {
	return `
		SELECT
			h.id, h.handoff_no, h.lead_id, h.tenant_id, h.customer_user_id,
			COALESCE(cu.username, ''), COALESCE(cu.display_name, ''),
			COALESCE(cu.phone, ''), h.sales_staff_id,
			COALESCE(su.display_name, su.username, ''),
			h.target_group_code, h.status, h.summary,
			h.accepted_by_user_id, COALESCE(au.display_name, au.username, ''),
			h.accepted_at, h.completed_by_user_id,
			COALESCE(du.display_name, du.username, ''), h.completed_at,
			h.created_at, h.updated_at
		FROM crm_customer_handoffs h
		LEFT JOIN mgmt_users cu ON cu.id=h.customer_user_id
		LEFT JOIN crm_sales_staff ss ON ss.id=h.sales_staff_id
		LEFT JOIN mgmt_users su ON su.id=ss.user_id
		LEFT JOIN mgmt_users au ON au.id=h.accepted_by_user_id
		LEFT JOIN mgmt_users du ON du.id=h.completed_by_user_id
	`
}

func (s *Store) GetCustomerHandoff(
	ctx context.Context,
	handoffID int64,
) (model.CustomerHandoff, error) {
	return scanCustomerHandoff(s.db.QueryRowContext(
		ctx,
		customerHandoffSelectSQL()+" WHERE h.id=? LIMIT 1",
		handoffID,
	))
}

func (s *Store) ListCustomerHandoffs(
	ctx context.Context,
	viewerID int64,
	manager bool,
	search string,
	status string,
	sortMode string,
	page int,
	pageSize int,
) ([]model.CustomerHandoff, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 12
	}
	where := []string{"h.target_group_code='live_operations'"}
	args := []any{}
	if !manager {
		where = append(where, "(h.status='pending' OR h.accepted_by_user_id=?)")
		args = append(args, viewerID)
	}
	search = strings.TrimSpace(search)
	if search != "" {
		like := "%" + search + "%"
		where = append(where, "(h.handoff_no LIKE ? OR cu.display_name LIKE ? OR cu.username LIKE ? OR cu.phone LIKE ? OR su.display_name LIKE ? OR h.summary LIKE ?)")
		args = append(args, like, like, like, like, like, like)
	}
	status = strings.TrimSpace(status)
	if status != "" && status != "all" {
		where = append(where, "h.status=?")
		args = append(args, status)
	}
	whereSQL := " WHERE " + strings.Join(where, " AND ")
	var total int64
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM crm_customer_handoffs h LEFT JOIN mgmt_users cu ON cu.id=h.customer_user_id LEFT JOIN crm_sales_staff ss ON ss.id=h.sales_staff_id LEFT JOIN mgmt_users su ON su.id=ss.user_id"+whereSQL,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	orderBy := "h.created_at DESC, h.id DESC"
	switch sortMode {
	case "created-asc":
		orderBy = "h.created_at ASC, h.id ASC"
	case "status-asc":
		orderBy = "h.status ASC, h.created_at DESC"
	case "customer-asc":
		orderBy = "cu.display_name ASC, h.id DESC"
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx,
		customerHandoffSelectSQL()+whereSQL+" ORDER BY "+orderBy+" LIMIT ? OFFSET ?",
		queryArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]model.CustomerHandoff, 0)
	for rows.Next() {
		item, err := scanCustomerHandoff(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Store) UpdateCustomerHandoffStatus(
	ctx context.Context,
	manager bool,
	note string,
	handoffID int64,
	operatorUserID int64,
	status string,
) (model.CustomerHandoff, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CustomerHandoff{}, err
	}
	defer tx.Rollback()

	var current string
	var accepted sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT status,accepted_by_user_id
		FROM crm_customer_handoffs
		WHERE id=? AND target_group_code='live_operations'
		FOR UPDATE
	`, handoffID).Scan(&current, &accepted); err != nil {
		return model.CustomerHandoff{}, err
	}

	if !manager && accepted.Valid && accepted.Int64 != operatorUserID {
		return model.CustomerHandoff{}, sql.ErrNoRows
	}
	if !validCustomerHandoffTransition(current, status) {
		return model.CustomerHandoff{}, fmt.Errorf("invalid handoff transition")
	}
	if current == status {
		return scanCustomerHandoff(tx.QueryRowContext(ctx, customerHandoffSelectSQL()+" WHERE h.id=?", handoffID))
	}
	if status == "completed" && strings.TrimSpace(note) == "" {
		return model.CustomerHandoff{}, fmt.Errorf("invalid handoff: completion note required")
	}
	switch status {
	case "accepted":
		if current != "pending" && current != "accepted" {
			return model.CustomerHandoff{}, fmt.Errorf("invalid handoff transition")
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE crm_customer_handoffs
			SET status='accepted', accepted_by_user_id=COALESCE(accepted_by_user_id, ?),
			    accepted_at=COALESCE(accepted_at, CURRENT_TIMESTAMP(3))
			WHERE id=?
		`, operatorUserID, handoffID); err != nil {
			return model.CustomerHandoff{}, err
		}
	case "in_progress":
		if current != "pending" && current != "accepted" && current != "in_progress" {
			return model.CustomerHandoff{}, fmt.Errorf("invalid handoff transition")
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE crm_customer_handoffs
			SET status='in_progress',
			    accepted_by_user_id=COALESCE(accepted_by_user_id, ?),
			    accepted_at=COALESCE(accepted_at, CURRENT_TIMESTAMP(3))
			WHERE id=?
		`, operatorUserID, handoffID); err != nil {
			return model.CustomerHandoff{}, err
		}
	case "completed":
		if current == "completed" {
			break
		}
		if current != "accepted" && current != "in_progress" {
			return model.CustomerHandoff{}, fmt.Errorf("invalid handoff transition")
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE crm_customer_handoffs
			SET status='completed',
			    completed_by_user_id=?,
			    completed_at=CURRENT_TIMESTAMP(3)
			WHERE id=?
		`, operatorUserID, handoffID); err != nil {
			return model.CustomerHandoff{}, err
		}
	default:
		return model.CustomerHandoff{}, fmt.Errorf("invalid handoff status")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO crm_customer_handoff_events(handoff_id,operator_user_id,status,content) VALUES(?,?,?,?)`, handoffID, operatorUserID, status, strings.TrimSpace(note)); err != nil {
		return model.CustomerHandoff{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CustomerHandoff{}, err
	}
	return s.GetCustomerHandoff(ctx, handoffID)
}

func isSalesLeadDuplicateError(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "duplicate:") || errors.Is(err, sql.ErrNoRows))
}
