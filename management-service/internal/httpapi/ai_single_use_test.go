package httpapi

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestAISingleUsePayerCustomerUsesVirtualBeans(t *testing.T) {
	got := aiSingleUsePayer(model.Actor{Role: "customer"})
	if got != aiSingleUsePayerCustomer {
		t.Fatalf("customer payer=%q want %q", got, aiSingleUsePayerCustomer)
	}
}

func TestAISingleUsePayerInternalStaffUsesCompany(t *testing.T) {
	roles := []string{"platform_admin", "agent_admin", "staff", "sales_staff"}
	for _, role := range roles {
		t.Run(role, func(t *testing.T) {
			got := aiSingleUsePayer(model.Actor{Role: role})
			if got != aiSingleUsePayerCompany {
				t.Fatalf("role=%s payer=%q want %q", role, got, aiSingleUsePayerCompany)
			}
		})
	}
}
