package httpapi

import (
	"net/http"
	"strings"

	"livecompanion/management/internal/model"
)

// roomReadingRules reads or replaces the room's "读写规则" text.
func (s *Server) roomReadingRules(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
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
	if r.Method == http.MethodGet {
		item, err := s.store.GetRoomReadingRules(r.Context(), tenantID, roomID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取读写规则失败")
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}
	var input model.RoomReadingRulesInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "读写规则格式错误")
		return
	}
	if lines := strings.Count(strings.TrimSpace(input.RulesText), "\n") + 1; len([]rune(input.RulesText)) > 4000 {
		writeError(w, http.StatusBadRequest, "读写规则过长，最多 4000 字")
		return
	} else if lines > 200 {
		writeError(w, http.StatusBadRequest, "读写规则最多 200 行")
		return
	}
	item, err := s.store.UpsertRoomReadingRules(r.Context(), tenantID, roomID, actor.UserID, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存读写规则失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}
