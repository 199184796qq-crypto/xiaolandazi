package httpapi

import (
	"net/http"
	"strings"

	"livecompanion/management/internal/model"
)

func requireSpeechMissionInternalViewer(w http.ResponseWriter, actor model.Actor) bool {
	if actor.IsInternalStaff() {
		return true
	}
	writeError(w, http.StatusForbidden, "策略黑板仅内部管理人员可查看")
	return false
}

func (s *Server) liveRoomSpeechMissions(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !requireSpeechMissionInternalViewer(w, actor) {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, roomID) {
		return
	}
	if s.speechMissions == nil {
		writeError(w, http.StatusServiceUnavailable, "策略黑板暂时不可用")
		return
	}
	items := s.speechMissions.RoomMissionSnapshots(roomID)
	filtered := items[:0]
	for _, item := range items {
		if item.TenantID == tenantID {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) > 100 {
		filtered = filtered[:100]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room_id":  roomID,
		"missions": filtered,
	})
}

func (s *Server) liveRoomSpeechMission(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !requireSpeechMissionInternalViewer(w, actor) {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, roomID) {
		return
	}
	if s.speechMissions == nil {
		writeError(w, http.StatusServiceUnavailable, "策略黑板暂时不可用")
		return
	}
	missionID := strings.TrimSpace(r.PathValue("missionID"))
	if missionID == "" {
		writeError(w, http.StatusBadRequest, "mission_id is required")
		return
	}
	mission, exists := s.speechMissions.MissionSnapshot(missionID)
	if !exists || mission.RoomID != roomID || mission.TenantID != tenantID {
		writeError(w, http.StatusNotFound, "口播任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, mission)
}
