package db

import (
	"context"
	"database/sql"
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
)

// All sales writes serialize with a portfolio transfer on the same sales row.
func lockSalesWriterTx(ctx context.Context, tx *sql.Tx, staffID int64) error {
	var id int64
	return tx.QueryRowContext(ctx, `SELECT ss.id FROM crm_sales_staff ss JOIN mgmt_users u ON u.id=ss.user_id WHERE ss.id=? AND ss.status='active' AND u.status='active' FOR UPDATE`, staffID).Scan(&id)
}

func (s *Store) UpdateSalesLeadByUser(ctx context.Context, userID, leadID int64, p model.SalesLeadInput) (model.SalesLead, error) {
	staffID, err := s.SalesStaffIDByUser(ctx, userID)
	if err != nil {
		return model.SalesLead{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SalesLead{}, err
	}
	defer tx.Rollback()
	if err = lockSalesWriterTx(ctx, tx, staffID); err != nil {
		return model.SalesLead{}, err
	}
	var state string
	if err = tx.QueryRowContext(ctx, "SELECT status FROM crm_sales_leads WHERE id=? AND owner_sales_staff_id=? FOR UPDATE", leadID, staffID).Scan(&state); err != nil {
		return model.SalesLead{}, err
	}
	if state != "open" {
		return model.SalesLead{}, fmt.Errorf("lead is closed")
	}
	if p.Stage == "" {
		p.Stage = "new"
	}
	if p.SourceType == "" {
		p.SourceType = "self_developed"
	}
	_, err = tx.ExecContext(ctx, `UPDATE crm_sales_leads SET business_name=?,contact_name=?,phone=?,wechat=?,email=?,industry_name=?,province=?,city=?,district=?,address=?,source_type=?,stage=?,planned_visit_at=?,next_followup_at=? WHERE id=? AND owner_sales_staff_id=?`, strings.TrimSpace(p.BusinessName), strings.TrimSpace(p.ContactName), strings.TrimSpace(p.Phone), strings.TrimSpace(p.Wechat), strings.TrimSpace(p.Email), strings.TrimSpace(p.IndustryName), strings.TrimSpace(p.Province), strings.TrimSpace(p.City), strings.TrimSpace(p.District), strings.TrimSpace(p.Address), p.SourceType, p.Stage, p.PlannedVisitAt, p.NextFollowupAt, leadID, staffID)
	if err != nil {
		return model.SalesLead{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO crm_sales_lead_activities(lead_id,sales_staff_id,activity_type,content,next_followup_at) VALUES(?,?,'updated','更新顾客资料或拜访计划',?)`, leadID, staffID, p.NextFollowupAt)
	if err != nil {
		return model.SalesLead{}, err
	}
	item, err := scanSalesLead(tx.QueryRowContext(ctx, salesLeadSelectSQL()+" WHERE l.id=? AND l.owner_sales_staff_id=?", leadID, staffID))
	if err != nil {
		return model.SalesLead{}, err
	}
	if err = s.commitInboxTx(ctx, tx, "sales"); err != nil {
		return model.SalesLead{}, err
	}
	return item, nil
}

// Keep department boundaries while allowing managers to recover disabled sellers.
func salesHandoverScopeSQL(scope model.StaffBusinessScope, col string) (string, []any) {
	switch scope.Mode {
	case "all":
		return "1=1", nil
	case "groups":
		marks := []string{}
		args := []any{}
		for _, id := range scope.GroupIDs {
			if id > 0 {
				marks = append(marks, "?")
				args = append(args, id)
			}
		}
		if len(marks) == 0 {
			return "1=0", nil
		}
		return fmt.Sprintf("EXISTS(SELECT 1 FROM staff_employees se_scope WHERE se_scope.user_id=%s AND se_scope.primary_group_id IN (%s))", col, strings.Join(marks, ",")), args
	default:
		return "1=0", nil
	}
}

func (s *Store) CanManageSalesHandover(ctx context.Context, scope model.StaffBusinessScope, id int64) error {
	where, args := salesHandoverScopeSQL(scope, "ss.user_id")
	args = append(args, id)
	var found int64
	return s.db.QueryRowContext(ctx, "SELECT ss.id FROM crm_sales_staff ss WHERE "+where+" AND ss.id=?", args...).Scan(&found)
}

type SalesHandoverPerson struct {
	ID            int64  `json:"staff_id"`
	UserID        int64  `json:"user_id"`
	Name          string `json:"display_name"`
	Username      string `json:"username"`
	Status        string `json:"status"`
	CustomerCount int64  `json:"customer_count"`
	LeadCount     int64  `json:"lead_count"`
}

func (s *Store) ListSalesHandoverPeople(ctx context.Context, scope model.StaffBusinessScope, search, status string, page, size int) ([]SalesHandoverPerson, int64, error) {
	page, size = normalizeBusinessPage(page, size, 100)
	where, args := salesHandoverScopeSQL(scope, "ss.user_id")
	if search = strings.TrimSpace(search); search != "" {
		where += " AND (u.display_name LIKE ? OR u.username LIKE ? OR ss.employee_code LIKE ?)"
		like := "%" + search + "%"
		args = append(args, like, like, like)
	}
	if status == "active" {
		where += " AND ss.status='active' AND u.status='active'"
	} else if status == "disabled" {
		where += " AND (ss.status<>'active' OR u.status<>'active')"
	}
	from := " FROM crm_sales_staff ss JOIN mgmt_users u ON u.id=ss.user_id WHERE " + where
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*)"+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx, `SELECT ss.id,u.id,u.display_name,u.username,CASE WHEN ss.status='active' AND u.status='active' THEN 'active' ELSE 'disabled' END,
 (SELECT COUNT(*) FROM crm_customer_sales_assignments a WHERE a.sales_staff_id=ss.id AND a.status='active' AND a.effective_to IS NULL),
 (SELECT COUNT(*) FROM crm_sales_leads l WHERE l.owner_sales_staff_id=ss.id AND l.status='open')`+from+" ORDER BY ss.id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []SalesHandoverPerson{}
	for rows.Next() {
		var v SalesHandoverPerson
		if err := rows.Scan(&v.ID, &v.UserID, &v.Name, &v.Username, &v.Status, &v.CustomerCount, &v.LeadCount); err != nil {
			return nil, 0, err
		}
		items = append(items, v)
	}
	return items, total, rows.Err()
}

func (s *Store) ListSalesHandoverHistory(ctx context.Context, scope model.StaffBusinessScope, page, size int) ([]model.SalesPortfolioHandover, int64, error) {
	page, size = normalizeBusinessPage(page, size, 100)
	a, aa := salesHandoverScopeSQL(scope, "fs.user_id")
	b, ba := salesHandoverScopeSQL(scope, "ts.user_id")
	args := append(aa, ba...)
	from := ` FROM crm_sales_handover_batches h JOIN crm_sales_staff fs ON fs.id=h.from_sales_staff_id JOIN mgmt_users fu ON fu.id=fs.user_id JOIN crm_sales_staff ts ON ts.id=h.to_sales_staff_id JOIN mgmt_users tu ON tu.id=ts.user_id WHERE (` + a + " OR " + b + ")"
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*)"+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx, `SELECT h.id,h.handover_no,h.from_sales_staff_id,fu.display_name,h.to_sales_staff_id,tu.display_name,h.customer_count,h.lead_count,h.reason,h.status,h.created_by_user_id,h.created_at`+from+" ORDER BY h.id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []model.SalesPortfolioHandover{}
	for rows.Next() {
		var v model.SalesPortfolioHandover
		if err := rows.Scan(&v.ID, &v.HandoverNo, &v.FromSalesStaffID, &v.FromDisplayName, &v.ToSalesStaffID, &v.ToDisplayName, &v.CustomerCount, &v.LeadCount, &v.Reason, &v.Status, &v.CreatedByUserID, &v.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, v)
	}
	return items, total, rows.Err()
}
