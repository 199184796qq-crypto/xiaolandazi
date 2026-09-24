package httpapi

import (
	"sort"

	"livecompanion/management/internal/model"
)

func staffBusinessScope(
	actor model.Actor,
	access model.StaffAccessContext,
	permission string,
) model.StaffBusinessScope {
	if access.IsSuperAdmin || actor.IsPlatformAdmin() {
		return model.StaffBusinessScope{
			Mode:        "all",
			ActorUserID: actor.UserID,
			ManagerView: true,
		}
	}

	managerView := len(access.ManagedGroupIDs) > 0
	if !managerView {
		return model.StaffBusinessScope{
			Mode:        "self",
			ActorUserID: actor.UserID,
			ManagerView: false,
		}
	}

	scope := staffPermissionScope(access, permission)
	switch scope {
	case "all", "all_internal":
		return model.StaffBusinessScope{
			Mode:        "all",
			ActorUserID: actor.UserID,
			ManagerView: true,
		}
	case "managed_groups":
		ids := append([]int64(nil), access.ManagedGroupIDs...)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		return model.StaffBusinessScope{
			Mode:        "groups",
			ActorUserID: actor.UserID,
			GroupIDs:    uniquePositiveIDs(ids),
			ManagerView: true,
		}
	case "group":
		ids := append([]int64(nil), staffPermissionGroupIDs(access, permission)...)
		// Backward-compatible fallback for older access contexts and tests that
		// predate permission-specific department provenance.
		if len(ids) == 0 {
			ids = append(ids, access.ManagedGroupIDs...)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		return model.StaffBusinessScope{
			Mode:        "groups",
			ActorUserID: actor.UserID,
			GroupIDs:    uniquePositiveIDs(ids),
			ManagerView: true,
		}
	default:
		return model.StaffBusinessScope{
			Mode:        "self",
			ActorUserID: actor.UserID,
			ManagerView: false,
		}
	}
}

func uniquePositiveIDs(values []int64) []int64 {
	result := make([]int64, 0, len(values))
	var previous int64
	for _, value := range values {
		if value <= 0 || value == previous {
			continue
		}
		result = append(result, value)
		previous = value
	}
	return result
}
