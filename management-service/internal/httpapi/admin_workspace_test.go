package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/model"
)

func TestAdminWorkspaceRoleTitle(t *testing.T) {
	for _, test := range []struct {
		name  string
		role  string
		roles []string
		want  string
	}{
		{name: "super admin", role: "platform_admin", want: "超级系统管理员"},
		{name: "marketing manager", role: "staff", roles: []string{"live_operations_staff", "live_operations_manager"}, want: "营销运维负责人"},
		{name: "marketing employee", role: "staff", roles: []string{"live_operations_staff"}, want: "营销运维专员"},
		{name: "other staff", role: "staff", roles: []string{"warehouse_after_sales_staff"}},
		{name: "sales staff", role: "sales_staff"},
		{name: "customer", role: "customer", roles: []string{"live_operations_manager"}},
		{name: "agent", role: "agent_admin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			access := model.StaffAccessContext{RoleCodes: test.roles, PrimaryGroupCode: "live_operations"}
			got := adminWorkspaceRoleTitle(model.Actor{Role: test.role}, access)
			if got != test.want {
				t.Fatalf("role title = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAdminWorkspaceRoutesRequireStaffPasswordSession(t *testing.T) {
	handler := (&Server{auth: auth.NewResolver("test", nil)}).Handler()
	for _, test := range []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodGet, "/bootstrap", "", http.StatusUnauthorized},
		{http.MethodGet, "/account", "", http.StatusUnauthorized},
		{http.MethodPatch, "/account", `{"username":"new-name"}`, http.StatusUnauthorized},
		{http.MethodPost, "/account/check-identifier", `{"field":"username","value":"new-name"}`, http.StatusUnauthorized},
		{http.MethodGet, "/shipping-regions", "", http.StatusUnauthorized},
		{http.MethodPost, "/login", `{"username":"staff","password":"example","captcha":"12345","method":"sms"}`, http.StatusBadRequest},
		{http.MethodPost, "/login", `{"username":"staff","password":"example","captcha":"12345"}`, http.StatusBadRequest},
		{http.MethodPost, "/register", `{}`, http.StatusNotFound},
	} {
		request := httptest.NewRequest(test.method, "/api/v1/admin/workspace"+test.path, strings.NewReader(test.body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status || response.Header().Get("Set-Cookie") != "" {
			t.Fatalf("%s %s = %d, want %d and no session cookie", test.method, test.path, response.Code, test.status)
		}
	}
}
