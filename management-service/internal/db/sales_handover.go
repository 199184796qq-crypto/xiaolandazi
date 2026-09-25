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

var ErrSalesPortfolioNotTransferred = errors.New("sales portfolio not transferred")

func (s *Store) SalesPortfolioHandoverPreview(
	ctx context.Context,
	fromSalesStaffID int64,
) (model.SalesPortfolioHandoverPreview, error) {
	var preview model.SalesPortfolioHandoverPreview
	preview.FromSalesStaffID = fromSalesStaffID
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(u.display_name, u.username, '')
		FROM crm_sales_staff ss
		INNER JOIN mgmt_users u ON u.id=ss.user_id
		WHERE ss.id=?
		LIMIT 1
	`, fromSalesStaffID).Scan(&preview.FromDisplayName); err != nil {
		return model.SalesPortfolioHandoverPreview{}, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM crm_customer_sales_assignments
		WHERE sales_staff_id=?
		  AND status='active'
		  AND effective_to IS NULL
	`, fromSalesStaffID).Scan(&preview.CustomerCount); err != nil {
		return model.SalesPortfolioHandoverPreview{}, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM crm_sales_leads
		WHERE owner_sales_staff_id=? AND status='open'
	`, fromSalesStaffID).Scan(&preview.OpenLeadCount); err != nil {
		return model.SalesPortfolioHandoverPreview{}, err
	}
	return preview, nil
}

func (s *Store) TransferSalesPortfolio(
	ctx context.Context,
	scope model.StaffBusinessScope,
	fromSalesStaffID int64,
	toSalesStaffID int64,
	createdByUserID int64,
	reason string,
	expectedCustomerCount, expectedLeadCount *int64,
) (model.SalesPortfolioHandover, error) {

	reason = strings.TrimSpace(reason)
	if fromSalesStaffID <= 0 || toSalesStaffID <= 0 || fromSalesStaffID == toSalesStaffID {
		return model.SalesPortfolioHandover{}, fmt.Errorf("invalid sales handover target")
	}
	if reason == "" {
		return model.SalesPortfolioHandover{}, fmt.Errorf("handover reason required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SalesPortfolioHandover{}, err
	}
	defer tx.Rollback()

	type staffInfo struct {
		id                       int64
		name, status, userStatus string
	}
	loadStaff := func(id int64) (staffInfo, error) {
		out := staffInfo{id: id}
		where, args := salesHandoverScopeSQL(scope, "ss.user_id")
		args = append(args, id)
		err := tx.QueryRowContext(ctx, `SELECT COALESCE(u.display_name,u.username,''),ss.status,u.status FROM crm_sales_staff ss JOIN mgmt_users u ON u.id=ss.user_id WHERE `+where+` AND ss.id=? FOR UPDATE`, args...).Scan(&out.name, &out.status, &out.userStatus)
		return out, err
	}
	first, second := fromSalesStaffID, toSalesStaffID
	if first > second {
		first, second = second, first
	}
	a, err := loadStaff(first)
	if err != nil {
		return model.SalesPortfolioHandover{}, err
	}
	b, err := loadStaff(second)
	if err != nil {
		return model.SalesPortfolioHandover{}, err
	}
	fromStaff, toStaff := a, b
	if a.id != fromSalesStaffID {
		fromStaff, toStaff = b, a
	}
	if toStaff.status != "active" || toStaff.userStatus != "active" {
		return model.SalesPortfolioHandover{}, sql.ErrNoRows
	}

	customerRows, err := tx.QueryContext(ctx, `
		SELECT id, tenant_id
		FROM crm_customer_sales_assignments
		WHERE sales_staff_id=?
		  AND status='active'
		  AND effective_to IS NULL
		ORDER BY id
		FOR UPDATE
	`, fromSalesStaffID)
	if err != nil {
		return model.SalesPortfolioHandover{}, err
	}
	type customerRef struct {
		assignmentID int64
		tenantID     int64
	}
	customers := make([]customerRef, 0)
	for customerRows.Next() {
		var item customerRef
		if err := customerRows.Scan(&item.assignmentID, &item.tenantID); err != nil {
			customerRows.Close()
			return model.SalesPortfolioHandover{}, err
		}
		customers = append(customers, item)
	}
	if err := customerRows.Err(); err != nil {
		customerRows.Close()
		return model.SalesPortfolioHandover{}, err
	}
	customerRows.Close()

	leadRows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM crm_sales_leads
		WHERE owner_sales_staff_id=? AND status='open'
		ORDER BY id
		FOR UPDATE
	`, fromSalesStaffID)
	if err != nil {
		return model.SalesPortfolioHandover{}, err
	}
	leadIDs := make([]int64, 0)
	for leadRows.Next() {
		var id int64
		if err := leadRows.Scan(&id); err != nil {
			leadRows.Close()
			return model.SalesPortfolioHandover{}, err
		}
		leadIDs = append(leadIDs, id)
	}
	if err := leadRows.Err(); err != nil {
		leadRows.Close()
		return model.SalesPortfolioHandover{}, err
	}
	leadRows.Close()

	if expectedCustomerCount != nil && int64(len(customers)) != *expectedCustomerCount {
		return model.SalesPortfolioHandover{}, fmt.Errorf("portfolio changed: customer count")
	}
	if expectedLeadCount != nil && int64(len(leadIDs)) != *expectedLeadCount {
		return model.SalesPortfolioHandover{}, fmt.Errorf("portfolio changed: lead count")
	}
	if len(customers) == 0 && len(leadIDs) == 0 {
		return model.SalesPortfolioHandover{}, fmt.Errorf("portfolio changed: nothing to transfer")
	}
	effectiveAt := time.Now().UTC()
	handoverNo := fmt.Sprintf("SHO-%d", time.Now().UTC().UnixNano())
	result, err := tx.ExecContext(ctx, `
		INSERT INTO crm_sales_handover_batches (
			handover_no, from_sales_staff_id, to_sales_staff_id,
			customer_count, lead_count, reason, status, created_by_user_id
		)
		VALUES (?, ?, ?, ?, ?, ?, 'completed', ?)
	`,
		handoverNo,
		fromSalesStaffID,
		toSalesStaffID,
		len(customers),
		len(leadIDs),
		reason,
		createdByUserID,
	)
	if err != nil {
		return model.SalesPortfolioHandover{}, err
	}
	batchID, err := result.LastInsertId()
	if err != nil {
		return model.SalesPortfolioHandover{}, err
	}

	handoverReason := "销售交接 " + handoverNo + "（完整原因见交接单）"
	for _, customer := range customers {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO crm_sales_handover_items (
				batch_id, item_type, reference_id, tenant_id
			)
			VALUES (?, 'customer', ?, ?)
		`, batchID, customer.assignmentID, customer.tenantID); err != nil {
			return model.SalesPortfolioHandover{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE crm_customer_sales_assignments
			SET status='ended', effective_to=?
			WHERE id=?
			  AND sales_staff_id=?
			  AND status='active'
			  AND effective_to IS NULL
		`, effectiveAt, customer.assignmentID, fromSalesStaffID); err != nil {
			return model.SalesPortfolioHandover{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO crm_customer_sales_assignments (
				tenant_id, sales_staff_id, status, effective_from,
				assigned_by_user_id, reason
			)
			VALUES (?, ?, 'active', ?, ?, ?)
		`, customer.tenantID, toSalesStaffID, effectiveAt, createdByUserID, handoverReason); err != nil {
			return model.SalesPortfolioHandover{}, err
		}
	}

	for _, leadID := range leadIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO crm_sales_handover_items (
				batch_id, item_type, reference_id, lead_id
			)
			VALUES (?, 'lead', ?, ?)
		`, batchID, leadID, leadID); err != nil {
			return model.SalesPortfolioHandover{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE crm_sales_leads
			SET owner_sales_staff_id=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=? AND owner_sales_staff_id=? AND status='open'
		`, toSalesStaffID, leadID, fromSalesStaffID); err != nil {
			return model.SalesPortfolioHandover{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO crm_sales_lead_activities (
				lead_id, sales_staff_id, activity_type, outcome,
				content, occurred_at
			)
			VALUES (?, ?, 'handover', 'transferred', ?, CURRENT_TIMESTAMP(3))
		`, leadID, toSalesStaffID, handoverReason); err != nil {
			return model.SalesPortfolioHandover{}, err
		}
	}

	if err := s.commitInboxTx(ctx, tx, "access", "sales", "support", "finance"); err != nil {
		return model.SalesPortfolioHandover{}, err
	}

	return model.SalesPortfolioHandover{
		ID:               batchID,
		HandoverNo:       handoverNo,
		FromSalesStaffID: fromSalesStaffID,
		FromDisplayName:  fromStaff.name,
		ToSalesStaffID:   toSalesStaffID,
		ToDisplayName:    toStaff.name,
		CustomerCount:    int64(len(customers)),
		LeadCount:        int64(len(leadIDs)),
		Reason:           reason,
		Status:           "completed",
		CreatedByUserID:  createdByUserID,
		CreatedAt:        time.Now().UTC(),
	}, nil
}

func (s *Store) SalesPortfolioPendingByEmployee(
	ctx context.Context,
	employeeID int64,
) (customerCount int64, leadCount int64, err error) {
	var salesStaffID int64
	err = s.db.QueryRowContext(ctx, `
		SELECT ss.id
		FROM staff_employees se
		INNER JOIN crm_sales_staff ss ON ss.user_id=se.user_id
		WHERE se.id=?
		LIMIT 1
	`, employeeID).Scan(&salesStaffID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}

	if err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM crm_customer_sales_assignments
		WHERE sales_staff_id=?
		  AND status='active'
		  AND effective_to IS NULL
	`, salesStaffID).Scan(&customerCount); err != nil {
		return 0, 0, err
	}
	if err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM crm_sales_leads
		WHERE owner_sales_staff_id=? AND status='open'
	`, salesStaffID).Scan(&leadCount); err != nil {
		return 0, 0, err
	}
	return customerCount, leadCount, nil
}
