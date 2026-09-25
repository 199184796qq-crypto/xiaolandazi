package db

import (
	"context"
	"database/sql"
	"strings"

	"livecompanion/management/internal/model"
)

func (s *Store) ListSalesFollowupsByUser(
	ctx context.Context,
	salesUserID int64,
) ([]model.SalesFollowup, model.SalesFollowupSummary, error) {
	staffID, err := s.SalesStaffIDByUser(ctx, salesUserID)
	if err != nil {
		return nil, model.SalesFollowupSummary{}, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			f.id,
			f.sales_staff_id,
			COALESCE(author_user.display_name, author_user.username, ''),
			f.tenant_id,
			COALESCE(u.id, 0),
			COALESCE(u.username, ''),
			COALESCE(u.display_name, u.username, ''),
			COALESCE(u.phone, ''),
			f.followup_type,
			f.content,
			f.next_followup_at,
			f.created_at
		FROM crm_sales_followups f
		INNER JOIN crm_customer_sales_assignments current_assignment
			ON current_assignment.tenant_id=f.tenant_id
		   AND current_assignment.sales_staff_id=?
		   AND current_assignment.status='active'
		   AND current_assignment.effective_to IS NULL
		LEFT JOIN crm_sales_staff author_staff ON author_staff.id=f.sales_staff_id
		LEFT JOIN mgmt_users author_user ON author_user.id=author_staff.user_id
		LEFT JOIN mgmt_users u
			ON u.tenant_id=f.tenant_id
		   AND u.role='customer'
		ORDER BY f.created_at DESC, f.id DESC
		LIMIT 300
	`, staffID)
	if err != nil {
		return nil, model.SalesFollowupSummary{}, err
	}
	defer rows.Close()

	items := make([]model.SalesFollowup, 0)
	for rows.Next() {
		var item model.SalesFollowup
		var next sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.SalesStaffID,
			&item.SalesDisplayName,
			&item.TenantID,
			&item.CustomerUserID,
			&item.CustomerUsername,
			&item.CustomerName,
			&item.CustomerPhone,
			&item.FollowupType,
			&item.Content,
			&next,
			&item.CreatedAt,
		); err != nil {
			return nil, model.SalesFollowupSummary{}, err
		}
		if next.Valid {
			value := next.Time
			item.NextFollowupAt = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, model.SalesFollowupSummary{}, err
	}

	var summary model.SalesFollowupSummary
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE
				WHEN f.next_followup_at >= CURRENT_TIMESTAMP(3)
				 AND f.next_followup_at < DATE_ADD(CURRENT_TIMESTAMP(3), INTERVAL 1 DAY)
				THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE
				WHEN f.next_followup_at < CURRENT_TIMESTAMP(3)
				THEN 1 ELSE 0 END), 0)
		FROM crm_sales_followups f
		INNER JOIN (
			SELECT f2.tenant_id, MAX(f2.id) AS latest_id
			FROM crm_sales_followups f2
			INNER JOIN crm_customer_sales_assignments current_assignment
				ON current_assignment.tenant_id=f2.tenant_id
			   AND current_assignment.sales_staff_id=?
			   AND current_assignment.status='active'
			   AND current_assignment.effective_to IS NULL
			GROUP BY f2.tenant_id
		) latest ON latest.latest_id=f.id
	`, staffID).Scan(
		&summary.TotalCount,
		&summary.DueCount,
		&summary.OverdueCount,
	); err != nil {
		return nil, model.SalesFollowupSummary{}, err
	}

	return items, summary, nil
}

func (s *Store) CreateSalesFollowupByUser(
	ctx context.Context,
	salesUserID int64,
	input model.SalesFollowupInput,
) (model.SalesFollowup, error) {
	staffID, err := s.SalesStaffIDByUser(ctx, salesUserID)
	if err != nil {
		return model.SalesFollowup{}, err
	}

	var assignmentCount int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM crm_customer_sales_assignments
		WHERE sales_staff_id=?
		  AND tenant_id=?
		  AND status='active'
		  AND effective_to IS NULL
	`, staffID, input.TenantID).Scan(&assignmentCount); err != nil {
		return model.SalesFollowup{}, err
	}
	if assignmentCount != 1 {
		return model.SalesFollowup{}, sql.ErrNoRows
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO crm_sales_followups (
			sales_staff_id,
			tenant_id,
			followup_type,
			content,
			next_followup_at
		)
		VALUES (?, ?, ?, ?, ?)
	`,
		staffID,
		input.TenantID,
		strings.TrimSpace(input.FollowupType),
		strings.TrimSpace(input.Content),
		input.NextFollowupAt,
	)
	if err != nil {
		return model.SalesFollowup{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.SalesFollowup{}, err
	}

	var item model.SalesFollowup
	var next sql.NullTime
	err = s.db.QueryRowContext(ctx, `
		SELECT
			f.id,
			f.sales_staff_id,
			COALESCE(author_user.display_name, author_user.username, ''),
			f.tenant_id,
			COALESCE(u.id, 0),
			COALESCE(u.username, ''),
			COALESCE(u.display_name, u.username, ''),
			COALESCE(u.phone, ''),
			f.followup_type,
			f.content,
			f.next_followup_at,
			f.created_at
		FROM crm_sales_followups f
		LEFT JOIN crm_sales_staff author_staff ON author_staff.id=f.sales_staff_id
		LEFT JOIN mgmt_users author_user ON author_user.id=author_staff.user_id
		LEFT JOIN mgmt_users u
			ON u.tenant_id=f.tenant_id
		   AND u.role='customer'
		WHERE f.id=? AND f.sales_staff_id=?
		LIMIT 1
	`, id, staffID).Scan(
		&item.ID,
		&item.SalesStaffID,
		&item.SalesDisplayName,
		&item.TenantID,
		&item.CustomerUserID,
		&item.CustomerUsername,
		&item.CustomerName,
		&item.CustomerPhone,
		&item.FollowupType,
		&item.Content,
		&next,
		&item.CreatedAt,
	)
	if err != nil {
		return model.SalesFollowup{}, err
	}
	if next.Valid {
		value := next.Time
		item.NextFollowupAt = &value
	}
	return item, nil
}
