package httpapi

import (
	"context"
	"net/http"

	"livecompanion/management/internal/model"
)

func (s *Server) buildBootstrap(
	ctx context.Context,
	actor model.Actor,
) (map[string]any, error) {
	tenants := make([]model.Tenant, 0)

	var staffAccess *model.StaffAccessContext
	if actor.IsPlatformAdmin() {
		staffAccess = &model.StaffAccessContext{
			IsSuperAdmin:       true,
			RoleCodes:          []string{"super_system_admin"},
			Permissions:        []string{"*"},
			PermissionScopes:   map[string]string{"*": "all"},
			PermissionGroupIDs: map[string][]int64{},
			GroupIDs:           []int64{},
			ManagedGroupIDs:    []int64{},
		}
	} else if actor.IsInternalStaff() {
		access, accessErr := s.store.GetStaffAccess(ctx, actor.UserID)
		if accessErr == nil {
			staffAccess = &access
		}
	}

	if actor.TenantID != nil {
		tenant, err := s.store.GetTenant(ctx, *actor.TenantID)
		if err != nil {
			return nil, err
		}
		tenants = []model.Tenant{tenant}
	}

	return map[string]any{
		"actor":        actor,
		"staff_access": staffAccess,
		"tenants":      tenants,
		"environment":  s.env,
	}, nil
}

func (s *Server) listTenants(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsPlatformAdmin() {
		writeError(w, http.StatusForbidden, "仅超级系统管理员可读取终端目录")
		return
	}

	tenants, err := s.store.ListTenants(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取终端列表失败")
		return
	}
	if tenants == nil {
		tenants = []model.Tenant{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": tenants,
	})
}
