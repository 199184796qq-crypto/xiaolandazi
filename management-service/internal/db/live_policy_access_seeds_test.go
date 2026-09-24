package db

import (
	"livecompanion/management/internal/model"
	"testing"
)

func TestLivePolicySeedHierarchyAndSeparation(t *testing.T) {
	seen := map[string]bool{}
	for _, role := range staffRoleSeeds {
		if role.Code != "live_operations_manager" && role.Code != "live_operations_staff" {
			continue
		}
		seen[role.Code] = true
		access := model.StaffAccessContext{Permissions: role.Permissions}
		isManager := role.Code == "live_operations_manager"
		if access.CanManageLivePolicyL1() != isManager || !access.CanManageLivePolicyL2() || access.CanDelegateLivePolicyL3() == isManager {
			t.Fatalf("unexpected hierarchy for %s", role.Code)
		}
		if isManager {
			for _, permission := range role.Permissions {
				if permission == "livepolicy.manage_l3_authorized" {
					t.Fatal("L1 role must not advertise delegated L3 permission")
				}
			}
		}
		if !access.CanUseLiveSupportCapability(model.LiveSupportCapabilityAnchorTraining) ||
			!access.CanUseLiveSupportCapability(model.LiveSupportCapabilityVoiceClone) {
			t.Fatalf("independent support capabilities changed for %s", role.Code)
		}
	}
	if len(seen) != 2 {
		t.Fatal("both live operations seed roles must exist")
	}
}
