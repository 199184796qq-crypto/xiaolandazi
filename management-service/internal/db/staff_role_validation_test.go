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

func TestLiveOperationsRolesDoNotGrantUnrelatedTopLevelPermissions(t *testing.T) {
	forbidden := map[string]bool{
		"customer.view_all":    true,
		"agent.view_all":       true,
		"system.settings.view": true,
	}

	for _, role := range staffRoleSeeds {
		if role.Code != "live_operations_manager" && role.Code != "live_operations_staff" {
			continue
		}
		for _, permission := range role.Permissions {
			if forbidden[permission] {
				t.Fatalf("role %s must not grant unrelated permission %s", role.Code, permission)
			}
		}
	}
}

func TestSystemSettingsAreReservedForPlatformAdmin(t *testing.T) {
	for _, role := range staffRoleSeeds {
		for _, permission := range role.Permissions {
			if permission == "system.settings.view" || permission == "system.settings.liveops.manage" || permission == "system.settings.inventory.manage" {
				t.Fatalf("role %s must not grant system settings permission %s", role.Code, permission)
			}
		}
	}
}

func TestFinanceRolesDoNotGrantCustomerOrAgentModules(t *testing.T) {
	forbidden := map[string]bool{"customer.view_all": true, "agent.view_all": true}
	for _, role := range staffRoleSeeds {
		if role.GroupCode != "finance" {
			continue
		}
		for _, permission := range role.Permissions {
			if forbidden[permission] {
				t.Fatalf("role %s must not grant unrelated permission %s", role.Code, permission)
			}
		}
	}
}

func TestSalesStaffOnlyGetsAssignedCustomerAccess(t *testing.T) {
	for _, role := range staffRoleSeeds {
		if role.Code != "sales_staff" {
			continue
		}
		if len(role.Permissions) != 1 || role.Permissions[0] != "sales.customer.view_assigned" {
			t.Fatalf("sales_staff permissions=%v want [sales.customer.view_assigned]", role.Permissions)
		}
		return
	}
	t.Fatal("sales_staff seed missing")
}
