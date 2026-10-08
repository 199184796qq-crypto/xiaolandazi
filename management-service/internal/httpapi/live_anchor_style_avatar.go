package httpapi

import (
	"errors"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
)

func (s *Server) liveAnchorStyleAvatar(w http.ResponseWriter, r *http.Request) {
	actor, tenant, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	id, ok := liveAnchorStylePathID(w, r, "styleID", "主播")
	if !ok {
		return
	}
	var input struct {
		AvatarType string `json:"avatar_type"`
	}
	if readJSON(w, r, &input) != nil || !model.ValidLiveAnchorAvatar(input.AvatarType) {
		writeError(w, http.StatusBadRequest, "请选择男、女、中年男或中年女头像")
		return
	}
	item, err := s.store.UpdateLiveAnchorStyleAvatar(r.Context(), tenant, id, actor.UserID, input.AvatarType)
	if errors.Is(err, appdb.ErrLiveAnchorStyleNotFound) {
		writeError(w, http.StatusNotFound, "主播不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存主播头像失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}
