package httpapi

import (
	"net/http"
	"testing"
)

func TestAuditActionStaffEmployeePasswordReset(t *testing.T) {
	got := auditAction(http.MethodPost, "/api/v1/staff/employees/42/reset-password")
	if got != "staff.employee.password_reset" {
		t.Fatalf("auditAction() = %q, want %q", got, "staff.employee.password_reset")
	}
}
