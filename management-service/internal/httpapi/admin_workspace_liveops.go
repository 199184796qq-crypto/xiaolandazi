package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func (s *Server) adminWorkspaceLiveOpsAccess(w http.ResponseWriter, r *http.Request) (model.Actor, model.StaffAccessContext, bool) {
	actor, access, ok := s.requireStaffPermission(w, r, "liveops.room_quota.view")
	if !ok {
		return actor, access, false
	}
	if actor.MustChangePassword || adminWorkspaceRoleTitle(actor, access) == "" {
		writeError(w, http.StatusForbidden, "当前员工不能进入直播运维")
		return actor, access, false
	}
	return actor, access, true
}

func (s *Server) adminWorkspaceLiveOps(w http.ResponseWriter, r *http.Request) {
	actor, access, ok := s.adminWorkspaceLiveOpsAccess(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	items, err := s.store.ListLiveOpsRoomQuotas(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取终端用户失败")
		return
	}
	catalog, err := s.store.ListCommercialMembershipPlans(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取会员等级失败")
		return
	}
	type choice struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	choices := make([]choice, 0, len(catalog))
	for _, item := range catalog {
		choices = append(choices, choice{ID: item.ID, Name: item.Name})
	}
	eligible, err := s.store.IsEligibleLiveSupportStaff(ctx, actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "校验协助申请权限失败")
		return
	}
	canDelete, err := s.canDeleteLiveOpsCustomer(ctx, actor, access)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取删除用户权限失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "memberships": choices,
		"can_adjust_quota":    staffHasPermission(access, "liveops.room_quota.manage"),
		"can_reset_password":  staffHasPermission(access, "customer.password_reset"),
		"can_delete_customer": canDelete,
		"can_view_support":    eligible && access.CanDelegateLivePolicyL3(),
		"can_configure_rooms": actor.IsPlatformAdmin() || staffHasPermission(access, "liveops.configure"),
	})
}

func (s *Server) adminWorkspaceLiveOpsOverview(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.adminWorkspaceLiveOpsAccess(w, r); !ok {
		return
	}
	overview, err := s.store.GetLiveOpsOverview(r.Context(), s.executionRealm)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取直播运维统计失败")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	online := s.countLiveOpsOnlineRooms(ctx, overview.RunningRooms)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"cooperating_users": overview.CooperatingUsers,
		"total_rooms":       overview.TotalRooms,
		"online_ai_rooms":   online,
	})
}

func (s *Server) countLiveOpsOnlineRooms(ctx context.Context, rooms []db.LiveOpsRoomKey) *int {
	online := 0
	for _, room := range rooms {
		// Core owns the live state. A stale running session after a Core restart
		// is not online until the runtime is actually working.
		state, err := s.getCoreAgentState(ctx, room.TenantID, room.RoomID)
		if err != nil {
			return nil
		}
		if state.State == "working" {
			online++
		}
	}
	return &online
}

func (s *Server) adminWorkspaceSupportEditor(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAdminWorkspaceAccount(w, r)
	if !ok {
		return
	}
	planID, ok := namedPathID(w, r, "planID", "直播方案")
	if !ok {
		return
	}
	access, err := s.staffAccessForActor(r, actor)
	if err != nil || !access.CanDelegateLivePolicyL3() {
		writeError(w, http.StatusForbidden, "当前员工没有方案协助权限")
		return
	}
	_, err = s.store.AuthorizedLivePlanSupportTenant(r.Context(), planID, actor.UserID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusForbidden, "客户未授权此方案，或已撤销授权")
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "校验方案协助授权失败")
	default:
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{"user_id": actor.UserID, "username": actor.Username, "display_name": actor.DisplayName})
	}
}

func (s *Server) adminWorkspaceRoomPlanScope(w http.ResponseWriter, r *http.Request) (staffID, tenantID, roomID int64, ok bool) {
	actor, access, ok := s.requireStaffPermission(w, r, "liveops.configure")
	if !ok {
		return 0, 0, 0, false
	}
	if actor.MustChangePassword || adminWorkspaceRoleTitle(actor, access) == "" || !access.CanDelegateLivePolicyL3() {
		writeError(w, http.StatusForbidden, "当前员工没有挂载策略的权限")
		return 0, 0, 0, false
	}
	roomID, ok = pathID(w, r)
	if !ok {
		return 0, 0, 0, false
	}
	tenantID, ok = s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return 0, 0, 0, false
	}
	if actor.TenantID != nil {
		if _, err := s.getCoreRoomState(r.Context(), tenantID, roomID); err != nil {
			writeError(w, http.StatusNotFound, "直播间不存在或不属于当前终端")
			return 0, 0, 0, false
		}
	}
	return actor.UserID, tenantID, roomID, true
}

func (s *Server) adminWorkspaceRoomPlanChoices(w http.ResponseWriter, r *http.Request) {
	staffID, tenantID, roomID, ok := s.adminWorkspaceRoomPlanScope(w, r)
	if !ok {
		return
	}
	items, err := s.store.ListAuthorizedRoomPlans(r.Context(), tenantID, roomID, staffID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取已授权策略失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminWorkspaceSetRoomPlan(w http.ResponseWriter, r *http.Request) {
	staffID, tenantID, roomID, ok := s.adminWorkspaceRoomPlanScope(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	_, changed := s.changeLiveRuntimeRoomPlan(w, r, tenantID, roomID, db.LiveAgentRoomPlanChange{
		PlanID: planID, ActorID: staffID, Clear: r.Method == http.MethodDelete, RequireSupportGrant: true,
	})
	if changed {
		w.WriteHeader(http.StatusNoContent)
	}
}
