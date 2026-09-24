package httpapi

import (
	"reflect"
	"testing"

	"livecompanion/management/internal/model"
)

func TestStaffBusinessScopeOrdinaryEmployeeIsForcedToSelf(t *testing.T) {
	actor := model.Actor{
		UserID: 41,
		Role:   "sales_staff",
	}
	access := model.StaffAccessContext{
		EmployeeID:       7,
		PrimaryGroupID:   3,
		RoleCodes:        []string{"sales_staff"},
		Permissions:      []string{"sales.view_all"},
		PermissionScopes: map[string]string{"sales.view_all": "all_internal"},
		ManagedGroupIDs:  []int64{},
	}

	scope := staffBusinessScope(actor, access, "sales.view_all")
	if scope.Mode != "self" {
		t.Fatalf("mode=%q want self", scope.Mode)
	}
	if scope.ActorUserID != actor.UserID {
		t.Fatalf("actor_user_id=%d want %d", scope.ActorUserID, actor.UserID)
	}
	if scope.ManagerView {
		t.Fatal("ordinary employee must not get manager view")
	}
}

func TestStaffBusinessScopeManagerUsesManagedGroups(t *testing.T) {
	actor := model.Actor{
		UserID: 51,
		Role:   "sales_staff",
	}
	access := model.StaffAccessContext{
		EmployeeID:       9,
		PrimaryGroupID:   3,
		RoleCodes:        []string{"sales_manager"},
		Permissions:      []string{"customer.view_all"},
		PermissionScopes: map[string]string{"customer.view_all": "group"},
		ManagedGroupIDs:  []int64{3, 8, 3},
	}

	scope := staffBusinessScope(actor, access, "customer.view_all")
	if scope.Mode != "groups" {
		t.Fatalf("mode=%q want groups", scope.Mode)
	}
	if !scope.ManagerView {
		t.Fatal("manager must get manager view")
	}
	want := []int64{3, 8}
	if !reflect.DeepEqual(scope.GroupIDs, want) {
		t.Fatalf("group_ids=%v want %v", scope.GroupIDs, want)
	}
}

func TestStaffBusinessScopeGroupUsesPermissionOriginDepartment(t *testing.T) {
	actor := model.Actor{UserID: 61, Role: "sales_staff"}
	access := model.StaffAccessContext{
		EmployeeID:       12,
		PrimaryGroupID:   3,
		RoleCodes:        []string{"sales_manager", "finance_reviewer"},
		Permissions:      []string{"finance.dashboard.view"},
		PermissionScopes: map[string]string{"finance.dashboard.view": "group"},
		PermissionGroupIDs: map[string][]int64{
			"finance.dashboard.view": {8},
		},
		ManagedGroupIDs: []int64{3},
	}

	scope := staffBusinessScope(actor, access, "finance.dashboard.view")
	if scope.Mode != "groups" {
		t.Fatalf("mode=%q want groups", scope.Mode)
	}
	want := []int64{8}
	if !reflect.DeepEqual(scope.GroupIDs, want) {
		t.Fatalf("group_ids=%v want %v", scope.GroupIDs, want)
	}
}

func TestStaffBusinessScopeSuperAdminGetsAll(t *testing.T) {
	actor := model.Actor{
		UserID: 1,
		Role:   "platform_admin",
	}
	scope := staffBusinessScope(actor, model.StaffAccessContext{}, "customer.view_all")
	if scope.Mode != "all" || !scope.ManagerView {
		t.Fatalf("scope=%+v want all manager view", scope)
	}
}

func TestTotalPages(t *testing.T) {
	cases := []struct {
		total    int64
		pageSize int
		want     int64
	}{
		{0, 10, 1},
		{1, 10, 1},
		{10, 10, 1},
		{11, 10, 2},
		{101, 10, 11},
	}
	for _, tc := range cases {
		if got := totalPages(tc.total, tc.pageSize); got != tc.want {
			t.Fatalf("totalPages(%d,%d)=%d want %d", tc.total, tc.pageSize, got, tc.want)
		}
	}
}
