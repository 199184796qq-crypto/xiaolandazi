package db

import (
	"errors"
	"testing"

	"livecompanion/management/internal/model"
)

func TestValidateStaffRoleCombinationLiveOperationsMutualExclusion(t *testing.T) {
	manager := model.StaffRoleSummary{Code: "live_operations_manager"}
	staff := model.StaffRoleSummary{Code: "live_operations_staff"}
	other := model.StaffRoleSummary{Code: "finance_staff"}

	if err := validateStaffRoleCombination([]model.StaffRoleSummary{manager}); err != nil {
		t.Fatalf("manager alone: %v", err)
	}
	if err := validateStaffRoleCombination([]model.StaffRoleSummary{staff}); err != nil {
		t.Fatalf("staff alone: %v", err)
	}
	if err := validateStaffRoleCombination([]model.StaffRoleSummary{manager, other}); err != nil {
		t.Fatalf("manager plus unrelated role: %v", err)
	}
	if err := validateStaffRoleCombination([]model.StaffRoleSummary{manager, staff}); !errors.Is(err, ErrMutuallyExclusiveStaffRoles) {
		t.Fatalf("manager + staff err=%v want ErrMutuallyExclusiveStaffRoles", err)
	}
}

func TestStaffRolesContainGroupCode(t *testing.T) {
	roles := []model.StaffRoleSummary{
		{GroupCode: "finance", Code: "finance_reviewer"},
		{GroupCode: "sales", Code: "sales_staff"},
	}
	if !staffRolesContainGroupCode(roles, "sales") {
		t.Fatal("sales role should be detected even when sales is not the primary department")
	}
	if staffRolesContainGroupCode(roles, "warehouse_after_sales") {
		t.Fatal("unassigned department must not be detected")
	}
}
