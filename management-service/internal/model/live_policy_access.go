package model

// Live policy permissions are evaluated from the effective permission union,
// not from a job title, so custom roles keep the same authorization semantics.
func (a StaffAccessContext) hasLivePolicyPermission(permission string) bool {
	if a.IsSuperAdmin {
		return true
	}
	for _, code := range a.Permissions {
		if code == "*" || code == permission {
			return true
		}
	}
	return false
}

func (a StaffAccessContext) CanManageLivePolicyL1() bool {
	return a.hasLivePolicyPermission("livepolicy.manage_l1")
}

func (a StaffAccessContext) CanManageLivePolicyL2() bool {
	return a.CanManageLivePolicyL1() || a.hasLivePolicyPermission("livepolicy.manage_l2")
}

// This is only the staff-side eligibility check. A live, room-specific customer
// grant and room ownership must still be checked before every delegated action.
func (a StaffAccessContext) CanDelegateLivePolicyL3() bool {
	return a.CanManageLivePolicyL2() &&
		a.hasLivePolicyPermission("livepolicy.manage_l3_authorized")
}

func (a StaffAccessContext) CanUseLiveSupportCapability(capability string) bool {
	switch capability {
	case LiveSupportCapabilityL3Policy:
		return a.CanDelegateLivePolicyL3()
	case LiveSupportCapabilityAnchorTraining:
		return a.hasLivePolicyPermission("livecoach.anchor_authorized")
	case LiveSupportCapabilityVoiceClone:
		return a.hasLivePolicyPermission("livevoice.clone_authorized")
	default:
		return false
	}
}
