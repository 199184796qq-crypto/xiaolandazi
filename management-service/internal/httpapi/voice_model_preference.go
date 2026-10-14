package httpapi

import "net/http"

func (s *Server) liveVoiceModelPreference(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	room, ok := namedPathID(w, r, "roomID", "直播间")
	if !ok {
		return
	}
	_, ok = namedPathID(w, r, "profileID", "声音档案")
	if !ok {
		return
	}
	tenant, ok := s.tenantForRoom(w, r, actor, room)
	if !ok {
		return
	}
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenant, room) {
		return
	}
	writeError(w, http.StatusForbidden, "声音模型由后台统一设置，请刷新页面后选择声音")
}
