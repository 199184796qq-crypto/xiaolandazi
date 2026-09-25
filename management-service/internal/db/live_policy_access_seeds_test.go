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
		if access.CanManageLivePolicyL1() != isManager || !access.CanManageLivePolicyL2() || !access.CanDelegateLivePolicyL3() {
			t.Fatalf("unexpected hierarchy for %s", role.Code)
		}
		hasUserLayerSupport := false
		for _, permission := range role.Permissions {
			if permission == "livepolicy.manage_l3_authorized" {
				hasUserLayerSupport = true
				break
			}
		}
		if !hasUserLayerSupport {
			t.Fatalf("%s must advertise customer-authorized user-layer support", role.Code)
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
