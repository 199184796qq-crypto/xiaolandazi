package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"github.com/go-sql-driver/mysql"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Server) registerCustomerBusinessRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/staff/finance/invitations", s.financeInvitations)
	m.HandleFunc("GET /api/v1/staff/finance/invitations/{invitationID}/earnings", s.financeInvitationEarnings)
	m.HandleFunc("GET /api/v1/staff/finance/review-policy", s.financeReviewPolicy)
	m.HandleFunc("GET /api/v1/customer-business/receipts", s.customerBusinessReceipts)
	m.HandleFunc("POST /api/v1/customer-business/receipts", s.customerBusinessSubmitReceipt)
	m.HandleFunc("POST /api/v1/customer-business/receipts/{id}/review", s.customerBusinessReviewReceipt)
	m.HandleFunc("POST /api/v1/customer-business/receipts/{id}/resubmit", s.customerBusinessResubmit)
	m.HandleFunc("GET /api/v1/customer-business/receipts/{id}/events", s.customerBusinessReceiptEvents)
	m.HandleFunc("GET /api/v1/customer-business/todos", s.customerBusinessTodos)
	m.HandleFunc("GET /api/v1/customer-business/accounts", s.customerBusinessAccounts)
	m.HandleFunc("GET /api/v1/customer-business/customers/{tenantID}/recognition", s.customerBusinessRecognition)
	m.HandleFunc("GET /api/v1/customer-business/customers/{tenantID}/money", s.customerBusinessMoney)
	m.HandleFunc("GET /api/v1/service/tickets", s.customerBusinessTickets)
	m.HandleFunc("POST /api/v1/service/tickets", s.customerBusinessCreateTicket)
	m.HandleFunc("POST /api/v1/service/tickets/{id}/actions", s.customerBusinessTicketAction)
	m.HandleFunc("GET /api/v1/service/tickets/{id}/events", s.customerBusinessTicketEvents)
}
func (s *Server) customerBusinessScope(w http.ResponseWriter, r *http.Request, domain string) (model.CustomerBusinessScope, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.CustomerBusinessScope{}, false
	}
	sc := model.CustomerBusinessScope{UserID: actor.UserID, Role: actor.Role}
	if actor.TenantID != nil {
		sc.TenantID = *actor.TenantID
	}
	w.Header().Set("Cache-Control", "no-store")
	if actor.Role == "customer" && sc.TenantID > 0 {
		return sc, true
	}
	if actor.IsSalesStaff() && domain != "finance" {
		return sc, true
	}
	if !actor.IsInternalStaff() {
		writeError(w, 403, "没有该业务权限")
		return sc, false
	}
	access, err := s.staffAccessForActor(r, actor)
	if err != nil {
		writeError(w, 503, "读取岗位权限失败")
		return sc, false
	}
	if domain == "finance" && (actor.IsPlatformAdmin() || staffHasPermission(access, "finance.dashboard.view")) {
		sc.Finance = true
		return sc, true
	}
	// Explicit finance roles take precedence; ordinary sales keep assigned-customer scope.
	if actor.IsSalesStaff() {
		return sc, true
	}
	if domain == "support" && (actor.IsPlatformAdmin() || staffHasPermission(access, "liveops.configure") || staffHasPermission(access, "liveops.ticket.manage")) {
		sc.Operations = true
		sc.OperationsManager = actor.IsPlatformAdmin() || staffHasPermission(access, "liveops.ticket.manage")
		return sc, true
	}
	writeError(w, 403, "没有该业务岗位权限")
	return sc, false
}
func customerBusinessError(w http.ResponseWriter, err error) {
	var sqlerr *mysql.MySQLError
	switch {
	case errors.Is(err, db.ErrFinanceReviewPolicyUnavailable):
		writeError(w, 503, err.Error())
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, 404, "记录不存在或不在你的权限范围")
	case errors.As(err, &sqlerr), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		writeError(w, 503, "业务处理暂未完成，请刷新记录核对后重试")
	case strings.Contains(err.Error(), "duplicate:"):
		writeError(w, 409, "这笔流水已经提交，请查看原收款单，不要重复入账")
	default:
		writeError(w, 409, err.Error())
	}
}
func businessListQuery(w http.ResponseWriter, r *http.Request) (int, int, int64, bool) {
	p, size := normalizedPageQuery(r, 12, 100)
	q := r.URL.Query()
	if p > 1000000 || utf8.RuneCountInString(q.Get("search")) > 160 || len(q.Get("status")) > 32 || len(q.Get("channel")) > 32 || len(q.Get("kind")) > 64 {
		writeError(w, 400, "查询参数超出范围")
		return 0, 0, 0, false
	}
	var tenant int64
	if q.Get("tenant_id") != "" {
		var err error
		tenant, err = strconv.ParseInt(q.Get("tenant_id"), 10, 64)
		if err != nil || tenant <= 0 {
			writeError(w, 400, "客户编号无效")
			return 0, 0, 0, false
		}
	}
	return p, size, tenant, true
}
func redactReceipt(v model.CustomerReceipt, sc model.CustomerBusinessScope) model.CustomerReceipt {
	if !sc.Finance {
		v.ReceivingAccount = ""
		v.PayerName = ""
		v.Evidence = ""
		if len(v.ExternalTradeNo) > 6 {
			v.ExternalTradeNo = "***" + v.ExternalTradeNo[len(v.ExternalTradeNo)-6:]
		}
	}
	v.IdempotencyKey = ""
	return v
}
func writeBusinessPage(w http.ResponseWriter, items any, total int64, page, size int) {
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size, "total_pages": totalPages(total, size)})
}
func (s *Server) customerBusinessReceipts(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.customerBusinessScope(w, r, "finance")
	if !ok {
		return
	}
	p, size, tenant, ok := businessListQuery(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	items, total, err := s.store.ListCustomerReceipts(r.Context(), sc, tenant, q.Get("search"), q.Get("status"), q.Get("channel"), p, size)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	for i := range items {
		items[i] = redactReceipt(items[i], sc)
	}
	writeBusinessPage(w, items, total, p, size)
}
func (s *Server) customerBusinessSubmitReceipt(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.customerBusinessScope(w, r, "finance")
	if !ok {
		return
	}
	if sc.Finance {
		if _, _, ok = s.requireStaffPermission(w, r, "finance.recharge.create"); !ok {
			return
		}
	}
	var v model.CustomerReceiptInput
	if readJSON(w, r, &v) != nil {
		writeError(w, 400, "收款内容格式不正确")
		return
	}
	if sc.Role == "customer" {
		v.TenantID = sc.TenantID
	}
	if err := db.ValidateCustomerReceipt(&v); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	out, err := s.store.SubmitCustomerReceipt(r.Context(), sc, v)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeJSON(w, 201, redactReceipt(out, sc))
}
func (s *Server) customerBusinessReviewReceipt(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.customerBusinessScope(w, r, "finance")
	if !ok {
		return
	}
	if !sc.Finance {
		writeError(w, 403, "仅财务审核岗位可以审核入账")
		return
	}
	if _, _, ok = s.requireStaffPermission(w, r, "finance.recharge.approve"); !ok {
		return
	}
	id, ok := parsePositivePathID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var v struct {
		Action    string `json:"action"`
		Version   int    `json:"version"`
		Note      string `json:"note"`
		Confirmed bool   `json:"confirmed"`
	}
	if readJSON(w, r, &v) != nil || !v.Confirmed {
		writeError(w, 400, "必须核对实际到账凭据并确认本次审核")
		return
	}
	out, err := s.store.ReviewCustomerReceipt(r.Context(), sc, id, v.Version, v.Action, v.Note)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) customerBusinessResubmit(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.customerBusinessScope(w, r, "finance")
	if !ok {
		return
	}
	if sc.Finance {
		if _, _, ok = s.requireStaffPermission(w, r, "finance.recharge.create"); !ok {
			return
		}
	}
	id, ok := parsePositivePathID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var p struct {
		Version  int    `json:"version"`
		Evidence string `json:"evidence"`
	}
	if readJSON(w, r, &p) != nil {
		writeError(w, 400, "请求格式不正确")
		return
	}
	v, err := s.store.ResubmitCustomerReceipt(r.Context(), sc, id, p.Version, p.Evidence)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeJSON(w, 200, redactReceipt(v, sc))
}
func (s *Server) customerBusinessReceiptEvents(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.customerBusinessScope(w, r, "finance")
	if !ok {
		return
	}
	id, ok := parsePositivePathID(w, r.PathValue("id"))
	if !ok {
		return
	}
	p, size, _, ok := businessListQuery(w, r)
	if !ok {
		return
	}
	items, total, err := s.store.ReceiptEvents(r.Context(), sc, id, p, size)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeBusinessPage(w, items, total, p, size)
}
func (s *Server) customerBusinessTodos(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.customerBusinessScope(w, r, "finance")
	if !ok {
		return
	}
	v, err := s.store.ReceiptTodos(r.Context(), sc)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) customerBusinessRecognition(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.customerBusinessScope(w, r, "finance")
	if !ok {
		return
	}
	tenant, ok := parsePositivePathID(w, r.PathValue("tenantID"))
	if !ok {
		return
	}
	v, err := s.store.CustomerRecognition(r.Context(), sc, tenant)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) customerBusinessMoney(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.customerBusinessScope(w, r, "finance")
	if !ok {
		return
	}
	tenant, ok := parsePositivePathID(w, r.PathValue("tenantID"))
	if !ok {
		return
	}
	p, size, _, ok := businessListQuery(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var from, to *string
	for _, name := range []string{"from", "to"} {
		if q.Get(name) != "" {
			d, err := time.Parse("2006-01-02", q.Get(name))
			if err != nil {
				writeError(w, 400, "日期格式应为 YYYY-MM-DD")
				return
			}
			if name == "to" {
				d = d.AddDate(0, 0, 1)
			}
			v := d.Format("2006-01-02 15:04:05")
			if name == "from" {
				from = &v
			} else {
				to = &v
			}
		}
	}
	items, total, err := s.store.ListCustomerMoney(r.Context(), sc, tenant, q.Get("kind"), q.Get("status"), q.Get("search"), from, to, p, size)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeBusinessPage(w, items, total, p, size)
}
func (s *Server) customerBusinessAccounts(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.customerBusinessScope(w, r, "finance")
	if !ok {
		return
	}
	p, size, _, ok := businessListQuery(w, r)
	if !ok {
		return
	}
	items, total, err := s.store.CustomerBusinessAccounts(r.Context(), sc, r.URL.Query().Get("search"), r.URL.Query().Get("qualification"), p, size)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeBusinessPage(w, items, total, p, size)
}
func (s *Server) customerBusinessTickets(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.customerBusinessScope(w, r, "support")
	if !ok {
		return
	}
	p, size, tenant, ok := businessListQuery(w, r)
	if !ok {
		return
	}
	items, total, err := s.store.ListSupportTickets(r.Context(), sc, tenant, r.URL.Query().Get("search"), r.URL.Query().Get("status"), p, size)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeBusinessPage(w, items, total, p, size)
}
func (s *Server) customerBusinessCreateTicket(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.customerBusinessScope(w, r, "support")
	if !ok {
		return
	}
	var p model.SupportTicketInput
	if readJSON(w, r, &p) != nil {
		writeError(w, 400, "申请内容格式不正确")
		return
	}
	if sc.Role == "customer" {
		p.TenantID = sc.TenantID
	}
	if err := db.ValidateSupportTicket(&p); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	v, err := s.store.CreateSupportTicket(r.Context(), sc, p)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeJSON(w, 201, v)
}
func (s *Server) customerBusinessTicketAction(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.customerBusinessScope(w, r, "support")
	if !ok {
		return
	}
	id, ok := parsePositivePathID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var p struct {
		Action  string `json:"action"`
		Version int    `json:"version"`
		Note    string `json:"note"`
	}
	if readJSON(w, r, &p) != nil {
		writeError(w, 400, "请求格式不正确")
		return
	}
	v, err := s.store.UpdateSupportTicket(r.Context(), sc, id, p.Version, p.Action, p.Note)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) customerBusinessTicketEvents(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.customerBusinessScope(w, r, "support")
	if !ok {
		return
	}
	id, ok := parsePositivePathID(w, r.PathValue("id"))
	if !ok {
		return
	}
	p, size, _, ok := businessListQuery(w, r)
	if !ok {
		return
	}
	items, total, err := s.store.SupportTicketEvents(r.Context(), sc, id, p, size)
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeBusinessPage(w, items, total, p, size)
}
