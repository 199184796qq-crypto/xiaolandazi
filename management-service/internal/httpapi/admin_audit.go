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
		if err != nil || !actor.IsPlatformAdmin() {
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
	case method == http.MethodPost && path == "/api/v1/auth/change-password":
		return "admin.change_password"
	case method == http.MethodPost && path == "/api/v1/auth/logout":
		return "admin.logout"
	case method == http.MethodPost && path == "/api/v1/rooms":
		return "room.create"
	case method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/rooms/"):
		return "room.delete"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/admin/customers/") &&
		strings.HasSuffix(path, "/reset-password"):
		return "customer.password_reset"
	case method == http.MethodDelete &&
		strings.HasPrefix(path, "/api/v1/admin/customers/"):
		return "customer.delete"
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
