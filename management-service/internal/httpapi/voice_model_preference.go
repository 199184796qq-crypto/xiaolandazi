package httpapi

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
)

func (s *Server) liveVoiceModelPreference(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	room, ok := namedPathID(w, r, "roomID", "直播间")
	if !ok {
		return
	}
	profileID, ok := namedPathID(w, r, "profileID", "声音档案")
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
	var input struct {
		Model string `json:"model"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "声音模型选择格式错误")
		return
	}
	modelName, err := normalizeVoiceCloneModel(input.Model)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	profile, err := s.store.GetVoiceProfile(r.Context(), tenant, profileID)
	if err != nil {
		writeError(w, 404, "声音档案不存在")
		return
	}
	if !s.requireLiveSupportVoice(w, r, actor, tenant, profile, true) {
		return
	}
	updated, err := s.store.SaveVoiceModelPreference(r.Context(), tenant, room, profileID, actor.UserID, modelName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "直播间或声音档案已变化，请刷新后重试")
			return
		}
		log.Printf("save voice model preference tenant=%d room=%d profile=%d: %v", tenant, room, profileID, err)
		writeError(w, http.StatusInternalServerError, "模型选择未保存，请稍后重试；如仍失败，请联系后台")
		return
	}
	writeJSON(w, 200, updated)
}
