package httpapi

import (
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"livecompanion/management/internal/model"
)

type auditStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *auditStatusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *auditStatusWriter) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(payload)
}

func (s *Server) adminAuditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isMutationMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		actor, err := s.auth.Resolve(r)
		if err != nil ||
			(!actor.IsInternalStaff() && !actor.IsAgentAdmin()) {
			next.ServeHTTP(w, r)
			return
		}

		entry := model.AdminAuditLog{
			ActorUserID:   actor.UserID,
			ActorUsername: actor.Username,
			Action:        auditAction(r.Method, r.URL.Path),
			HTTPMethod:    r.Method,
			Path:          r.URL.Path,
			ClientIP:      requestClientIP(r),
			Result:        "started",
		}
		s.fillAuditTarget(r, &entry)

		pendingID, err := s.audit.Begin(r.Context(), entry)
		if err != nil {
			writeError(
				w,
				http.StatusServiceUnavailable,
				"审计日志服务暂不可用，管理操作已阻止",
			)
			return
		}

		recorder := &auditStatusWriter{
			ResponseWriter: w,
			status:         http.StatusOK,
		}
		next.ServeHTTP(recorder, r)

		entry.Result = "http_" + strconv.Itoa(recorder.status)
		if err := s.audit.Complete(
			r.Context(),
			pendingID,
			entry,
		); err != nil {
			log.Printf(
				"admin audit finalize failed action=%s actor=%s error=%v",
				entry.Action,
				entry.ActorUsername,
				err,
			)
		}
	})
}

func isMutationMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func auditAction(method string, path string) string {
	switch {
	case method == http.MethodPost &&
		path == "/api/v1/auth/change-password":
		return "admin.change_password"
	case method == http.MethodPost &&
		path == "/api/v1/auth/logout":
		return "admin.logout"
	case method == http.MethodPost &&
		path == "/api/v1/rooms":
		return "room.create"
	case method == http.MethodDelete &&
		strings.HasPrefix(path, "/api/v1/rooms/"):
		return "room.delete"
	case method == http.MethodPost &&
		path == "/api/v1/commercial/memberships":
		return "commercial.membership.create"
	case method == http.MethodPut &&
		strings.HasPrefix(path, "/api/v1/commercial/memberships/") &&
		strings.HasSuffix(path, "/draft"):
		return "commercial.membership.draft_save"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/commercial/memberships/") &&
		strings.HasSuffix(path, "/publish"):
		return "commercial.membership.publish"
	case method == http.MethodPost &&
		path == "/api/v1/admin/agents":
		return "agent.create"
	case method == http.MethodPost &&
		path == "/api/v1/agent/customers":
		return "agent.customer.create"
	case method == http.MethodPatch &&
		strings.HasPrefix(path, "/api/v1/admin/invitations/"):
		return "invitation.status_update"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/admin/agents/") &&
		strings.HasSuffix(path, "/resources/adjust"):
		return "agent.resource_adjust"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/admin/customers/") &&
		strings.HasSuffix(path, "/resources/adjust"):
		return "customer.resource_adjust"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/agent/customers/") &&
		strings.HasSuffix(path, "/resources/allocate"):
		return "agent.customer.resource_allocate"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/admin/customers/") &&
		strings.HasSuffix(path, "/reset-password"):
		return "customer.password_reset"
	case method == http.MethodDelete &&
		strings.HasPrefix(path, "/api/v1/admin/customers/"):
		return "customer.delete"
	case method == http.MethodPost &&
		path == "/api/v1/staff/groups":
		return "staff.group.create"
	case method == http.MethodPatch &&
		strings.HasPrefix(path, "/api/v1/staff/groups/"):
		return "staff.group.update"
	case method == http.MethodPost &&
		path == "/api/v1/staff/roles":
		return "staff.role.create"
	case method == http.MethodPut &&
		strings.HasPrefix(path, "/api/v1/staff/roles/"):
		return "staff.role.update"
	case method == http.MethodPost &&
		path == "/api/v1/staff/employees":
		return "staff.employee.create"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/staff/employees/") &&
		strings.HasSuffix(path, "/disable"):
		return "staff.employee.disable"
	case method == http.MethodPut &&
		strings.HasPrefix(path, "/api/v1/staff/employees/") &&
		strings.HasSuffix(path, "/roles"):
		return "staff.employee.role_update"
	case method == http.MethodPatch &&
		strings.HasPrefix(path, "/api/v1/staff/approval-policies/"):
		return "staff.approval_policy.update"
	case method == http.MethodPost &&
		path == "/api/v1/staff/finance/recharge":
		return "finance.recharge.create"
	case method == http.MethodPost &&
		path == "/api/v1/staff/finance/refund":
		return "finance.refund.create"
	case method == http.MethodPost &&
		path == "/api/v1/staff/finance/reward":
		return "finance.reward.grant"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/staff/finance/approvals/") &&
		strings.HasSuffix(path, "/approve"):
		return "finance.approval.approve"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/staff/finance/approvals/") &&
		strings.HasSuffix(path, "/reject"):
		return "finance.approval.reject"
	default:
		return method + " " + path
	}
}
func (s *Server) fillAuditTarget(
	r *http.Request,
	entry *model.AdminAuditLog,
) {
	if !strings.HasPrefix(
		r.URL.Path,
		"/api/v1/admin/customers/",
	) {
		return
	}

	userID, err := strconv.ParseInt(
		r.PathValue("userID"),
		10,
		64,
	)
	if err != nil || userID <= 0 {
		return
	}

	entry.TargetUserID = userID
	customer, err := s.store.GetAdminCustomer(
		r.Context(),
		userID,
	)
	if err != nil {
		return
	}
	entry.TargetUsername = customer.Username
	entry.TargetTenantID = customer.TenantID
}

func requestClientIP(r *http.Request) string {
	host := r.RemoteAddr
	if parsedHost, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = parsedHost
	}
	return host
}
