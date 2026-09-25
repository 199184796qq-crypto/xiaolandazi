package db

import (
	"context"
	"database/sql"
	"errors"
	"livecompanion/management/internal/model"
	"strings"
	"time"
	"unicode/utf8"
)

func ValidateSupportTicket(p *model.SupportTicketInput) error {
	p.Title = strings.TrimSpace(p.Title)
	p.Description = strings.TrimSpace(p.Description)
	p.ContactName = strings.TrimSpace(p.ContactName)
	p.ContactPhone = strings.TrimSpace(p.ContactPhone)
	p.IdempotencyKey = strings.TrimSpace(p.IdempotencyKey)
	if p.TenantID <= 0 {
		return errors.New("客户无效")
	}
	for _, f := range []struct {
		s   string
		max int
	}{{p.Title, 160}, {p.Description, 2000}, {p.ContactName, 128}, {p.ContactPhone, 64}, {p.IdempotencyKey, 128}} {
		if f.s == "" || utf8.RuneCountInString(f.s) > f.max {
			return errors.New("问题、描述、联系人及电话不能为空或超长")
		}
	}
	switch p.Category {
	case "onboarding", "equipment", "live_config", "policy", "training", "voice", "other":
	default:
		return errors.New("协助类别无效")
	}
	if p.PreferredAt != nil && p.PreferredAt.Before(time.Now().Add(-24*time.Hour)) {
		return errors.New("预约时间不能是过去日期")
	}
	return nil
}

const ticketSelect = `SELECT k.id,k.ticket_no,k.tenant_id,t.name,k.requester_user_id,k.requester_role,k.category,k.title,k.description,k.contact_name,k.contact_phone,k.preferred_at,k.status,k.assigned_user_id,COALESCE(u.display_name,''),k.resolution,k.confirmed_at,k.version_no,k.idempotency_key,k.created_at,k.updated_at FROM crm_support_tickets k JOIN mgmt_tenants t ON t.id=k.tenant_id LEFT JOIN mgmt_users u ON u.id=k.assigned_user_id`

func scanTicket(q interface{ Scan(...any) error }) (model.SupportTicket, error) {
	var v model.SupportTicket
	err := q.Scan(&v.ID, &v.TicketNo, &v.TenantID, &v.CustomerName, &v.RequesterUserID, &v.RequesterRole, &v.Category, &v.Title, &v.Description, &v.ContactName, &v.ContactPhone, &v.PreferredAt, &v.Status, &v.AssignedUserID, &v.AssignedName, &v.Resolution, &v.ConfirmedAt, &v.Version, &v.IdempotencyKey, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func ticketScopeSQL(sc model.CustomerBusinessScope) (string, []any) {
	if sc.OperationsManager {
		return "1=1", nil
	}
	if sc.Operations {
		return "(k.status='pending' OR k.assigned_user_id=?)", []any{sc.UserID}
	}
	return customerScopeSQL(sc, "k.tenant_id")
}
func (s *Store) GetSupportTicket(ctx context.Context, sc model.CustomerBusinessScope, id int64) (model.SupportTicket, error) {
	w, a := ticketScopeSQL(sc)
	a = append([]any{id}, a...)
	return scanTicket(s.db.QueryRowContext(ctx, ticketSelect+" WHERE k.id=? AND ("+w+")", a...))
}
func (s *Store) CreateSupportTicket(ctx context.Context, sc model.CustomerBusinessScope, p model.SupportTicketInput) (model.SupportTicket, error) {
	if sc.Role != "customer" && sc.Role != "sales_staff" {
		return model.SupportTicket{}, sql.ErrNoRows
	}
	if err := ValidateSupportTicket(&p); err != nil {
		return model.SupportTicket{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SupportTicket{}, err
	}
	defer tx.Rollback()
	if err = lockCustomerBusinessWriter(ctx, tx, sc); err != nil {
		return model.SupportTicket{}, err
	}
	if err = checkCustomerBusinessAccess(ctx, tx, sc, p.TenantID); err != nil {
		return model.SupportTicket{}, err
	}
	existing, e := scanTicket(tx.QueryRowContext(ctx, ticketSelect+" WHERE k.requester_user_id=? AND k.idempotency_key=?", sc.UserID, p.IdempotencyKey))
	if e == nil {
		if existing.TenantID != p.TenantID || existing.Title != p.Title || existing.Description != p.Description || existing.Category != p.Category || existing.ContactName != p.ContactName || existing.ContactPhone != p.ContactPhone {
			return existing, ErrCustomerBusinessConflict
		}
		return existing, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return existing, e
	}
	no, err := newFinanceReference("SUP")
	if err != nil {
		return model.SupportTicket{}, err
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO crm_support_tickets(ticket_no,tenant_id,requester_user_id,requester_role,category,title,description,contact_name,contact_phone,preferred_at,idempotency_key) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, no, p.TenantID, sc.UserID, sc.Role, p.Category, p.Title, p.Description, p.ContactName, p.ContactPhone, p.PreferredAt, p.IdempotencyKey)
	if err != nil {
		return model.SupportTicket{}, normalizeDuplicate(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		return model.SupportTicket{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO crm_support_ticket_events(ticket_id,actor_user_id,action,status,note) VALUES(?,?,'created','pending','已申请协助；仅创建工单，不授予任何代操作权限')", id, sc.UserID); err != nil {
		return model.SupportTicket{}, err
	}
	v, err := scanTicket(tx.QueryRowContext(ctx, ticketSelect+" WHERE k.id=?", id))
	if err != nil {
		return v, err
	}
	return v, s.commitInboxTx(ctx, tx, "support")
}
func (s *Store) ListSupportTickets(ctx context.Context, sc model.CustomerBusinessScope, tenant int64, search, status string, page, size int) ([]model.SupportTicket, int64, error) {
	page, size = businessPage(page, size)
	w, a := ticketScopeSQL(sc)
	w = " WHERE (" + w + ")"
	if tenant > 0 {
		w += " AND k.tenant_id=?"
		a = append(a, tenant)
	}
	if status != "" && status != "all" {
		w += " AND k.status=?"
		a = append(a, status)
	}
	if search != "" {
		w += " AND (k.title LIKE ? ESCAPE '!' OR k.ticket_no LIKE ? ESCAPE '!' OR t.name LIKE ? ESCAPE '!')"
		a = append(a, businessLike(search), businessLike(search), businessLike(search))
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM crm_support_tickets k JOIN mgmt_tenants t ON t.id=k.tenant_id"+w, a...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, ticketSelect+w+" ORDER BY k.updated_at DESC,k.id DESC LIMIT ? OFFSET ?", append(append([]any{}, a...), size, (page-1)*size)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.SupportTicket{}
	for rows.Next() {
		v, e := scanTicket(rows)
		if e != nil {
			return nil, 0, e
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// All terminal transitions preserve the ticket and author history. Resolve != confirm.
func SupportTransition(status, action string, operations bool) (string, bool) {
	if operations {
		switch action {
		case "accept":
			return "accepted", status == "pending"
		case "start":
			return "in_progress", status == "accepted"
		case "wait_customer":
			return "waiting_customer", status == "accepted" || status == "in_progress"
		case "resolve":
			return "awaiting_confirmation", status == "in_progress" || status == "waiting_customer"
		case "reply":
			return status, status != "completed" && status != "cancelled"
		}
		return "", false
	}
	switch action {
	case "reply":
		if status == "waiting_customer" {
			return "in_progress", true
		}
		return status, status != "completed" && status != "cancelled"
	case "confirm":
		return "completed", status == "awaiting_confirmation"
	case "cancel":
		return "cancelled", status == "pending" || status == "accepted" || status == "waiting_customer"
	case "reopen":
		return "pending", status == "completed" || status == "awaiting_confirmation"
	}
	return "", false
}
func (s *Store) UpdateSupportTicket(ctx context.Context, sc model.CustomerBusinessScope, id int64, version int, action, note string) (model.SupportTicket, error) {
	note = strings.TrimSpace(note)
	if note == "" || utf8.RuneCountInString(note) > 2000 {
		return model.SupportTicket{}, errors.New("处理、确认或补充说明必填，最多2000字")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SupportTicket{}, err
	}
	defer tx.Rollback()
	if err = lockCustomerBusinessWriter(ctx, tx, sc); err != nil {
		return model.SupportTicket{}, err
	}
	w, a := ticketScopeSQL(sc)
	a = append([]any{id}, a...)
	v, err := scanTicket(tx.QueryRowContext(ctx, ticketSelect+" WHERE k.id=? AND ("+w+") FOR UPDATE", a...))
	if err != nil {
		return v, err
	}
	if v.Version != version {
		return v, ErrCustomerBusinessConflict
	}
	if sc.Operations && !sc.OperationsManager && v.AssignedUserID != nil && *v.AssignedUserID != sc.UserID {
		return v, sql.ErrNoRows
	}
	state, ok := SupportTransition(v.Status, action, sc.Operations)
	if !ok {
		return v, ErrCustomerBusinessConflict
	}
	if action == "accept" {
		v.AssignedUserID = &sc.UserID
	}
	if action == "resolve" {
		v.Resolution = note
	}
	var confirmedBy any
	var at any
	if action == "confirm" {
		confirmedBy = sc.UserID
		at = time.Now().UTC()
	}
	if action == "reopen" {
		v.AssignedUserID = nil
		v.Resolution = ""
	}
	_, err = tx.ExecContext(ctx, `UPDATE crm_support_tickets SET status=?,assigned_user_id=?,resolution=?,confirmed_by_user_id=?,confirmed_at=?,version_no=version_no+1 WHERE id=?`, state, v.AssignedUserID, v.Resolution, confirmedBy, at, id)
	if err != nil {
		return v, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO crm_support_ticket_events(ticket_id,actor_user_id,action,status,note) VALUES(?,?,?,?,?)", id, sc.UserID, action, state, note); err != nil {
		return v, err
	}
	v, err = scanTicket(tx.QueryRowContext(ctx, ticketSelect+" WHERE k.id=?", id))
	if err != nil {
		return v, err
	}
	return v, s.commitInboxTx(ctx, tx, "support")
}
func (s *Store) SupportTicketEvents(ctx context.Context, sc model.CustomerBusinessScope, id int64, page, size int) ([]model.CustomerBusinessEvent, int64, error) {
	if _, err := s.GetSupportTicket(ctx, sc, id); err != nil {
		return nil, 0, err
	}
	page, size = businessPage(page, size)
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM crm_support_ticket_events WHERE ticket_id=?", id).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT e.id,COALESCE(u.display_name,''),e.action,e.note,e.created_at FROM crm_support_ticket_events e LEFT JOIN mgmt_users u ON u.id=e.actor_user_id WHERE ticket_id=? ORDER BY e.id DESC LIMIT ? OFFSET ?", id, size, (page-1)*size)
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
