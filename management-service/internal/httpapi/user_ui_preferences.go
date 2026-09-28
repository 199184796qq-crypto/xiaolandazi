package httpapi

import (
	"net/http"

	appdb "livecompanion/management/internal/db"
)

type userUIPreferencesInput struct {
	SelectedLiveRoomID     *int64 `json:"selected_live_room_id"`
	SidebarCollapsed       *bool  `json:"sidebar_collapsed"`
	LivePlanPanelCollapsed *bool  `json:"live_plan_panel_collapsed"`
	AgentDrawerCollapsed   *bool  `json:"agent_drawer_collapsed"`
}

func (s *Server) getUserUIPreferences(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	item, err := s.store.GetUserUIPreferences(r.Context(), actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取界面偏好失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) updateUserUIPreferences(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	var input userUIPreferencesInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "界面偏好请求格式错误")
		return
	}
	item, err := s.store.UpdateUserUIPreferences(r.Context(), actor.UserID, appdb.UserUIPreferencesPatch{
		SelectedLiveRoomID:     input.SelectedLiveRoomID,
		SidebarCollapsed:       input.SidebarCollapsed,
		LivePlanPanelCollapsed: input.LivePlanPanelCollapsed,
		AgentDrawerCollapsed:   input.AgentDrawerCollapsed,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存界面偏好失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, item)
}
