package model

// L1 and customer L3 delegation are deliberately mutually exclusive. Check the
// effective permission union, not a job title, so a second role cannot bypass it.
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
	return !a.CanManageLivePolicyL1() && a.CanManageLivePolicyL2()
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
