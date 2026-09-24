package model

import (
	"fmt"
	"testing"
)

func TestLivePolicyAccessSeparationMatrix(t *testing.T) {
	for mask := 0; mask < 8; mask++ {
		for _, super := range []bool{false, true} {
			t.Run(fmt.Sprintf("permissions_%03b_super_%v", mask, super), func(t *testing.T) {
				access := StaffAccessContext{IsSuperAdmin: super}
				for index, code := range []string{"livepolicy.manage_l1", "livepolicy.manage_l2", "livepolicy.manage_l3_authorized"} {
					if mask&(1<<index) != 0 {
						access.Permissions = append(access.Permissions, code)
					}
				}
				wantL1 := super || mask&1 != 0
				wantL2 := wantL1 || mask&2 != 0
				wantL3 := !wantL1 && mask&2 != 0
				if got := access.CanManageLivePolicyL1(); got != wantL1 {
					t.Fatalf("L1=%v want %v", got, wantL1)
				}
				if got := access.CanManageLivePolicyL2(); got != wantL2 {
					t.Fatalf("L2=%v want %v", got, wantL2)
				}
				if got := access.CanUseLiveSupportCapability(LiveSupportCapabilityL3Policy); got != wantL3 {
					t.Fatalf("delegated L3=%v want %v", got, wantL3)
				}
			})
		}
	}
}

func TestLivePolicyWildcardAndMultipleRolesCannotBypassL3Exclusion(t *testing.T) {
	for _, permissions := range [][]string{
		{"*"},
		{"*", "livepolicy.manage_l3_authorized"},
		{"livepolicy.manage_l3_authorized", "livepolicy.manage_l2", "livepolicy.manage_l1"},
	} {
		access := StaffAccessContext{Permissions: permissions, RoleCodes: []string{"custom_manager", "live_operations_staff"}}
		if !access.CanManageLivePolicyL2() || access.CanDelegateLivePolicyL3() {
			t.Fatalf("incorrect permission union for %v", permissions)
		}
	}
}

func TestLivePolicySeparationPreservesIndependentSupportCapabilities(t *testing.T) {
	access := StaffAccessContext{Permissions: []string{
		"livepolicy.manage_l1", "livepolicy.manage_l3_authorized",
		"livecoach.anchor_authorized", "livevoice.clone_authorized",
	}}
	if access.CanDelegateLivePolicyL3() {
		t.Fatal("L1 staff must never delegate L3")
	}
	if !access.CanUseLiveSupportCapability(LiveSupportCapabilityAnchorTraining) ||
		!access.CanUseLiveSupportCapability(LiveSupportCapabilityVoiceClone) {
		t.Fatal("independent anchor/voice permissions must remain unchanged")
	}
	if access.CanUseLiveSupportCapability("unknown") {
		t.Fatal("unknown capability must be denied")
	}
}
