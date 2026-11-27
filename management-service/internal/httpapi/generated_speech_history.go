package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) listLiveRecords(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "仅终端账号可查看直播记录")
		return
	}
	const pageSize = 20
	query := r.URL.Query()
	var roomID int64
	if query.Has("room_id") {
		var err error
		roomID, err = strconv.ParseInt(query.Get("room_id"), 10, 64)
		if err != nil || roomID <= 0 {
			writeError(w, http.StatusBadRequest, "请选择有效的直播间")
			return
		}
	}
	page := min(parseGeneratedSpeechInt(query.Get("page"), 1), 100_000)
	items, err := s.store.ListLiveRecords(r.Context(), *actor.TenantID, roomID, s.executionRealm, query.Get("usage_only") == "1", pageSize+1, (page-1)*pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播记录失败")
		return
	}
	hasNext := len(items) > pageSize
	writeJSON(w, http.StatusOK, map[string]any{"items": items[:min(len(items), pageSize)], "has_next": hasNext})
}

func parseGeneratedSpeechInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func (s *Server) listRoomGeneratedSpeeches(w http.ResponseWriter, r *http.Request) {
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
	requestedSessionID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("session_id")), 10, 64)
	playedOnly := r.URL.Query().Get("played_only") == "1"
	if playedOnly && requestedSessionID <= 0 {
		writeError(w, http.StatusBadRequest, "请选择有效的直播场次")
		return
	}
	runtimeSession, err := s.store.GetGeneratedSpeechHistorySession(r.Context(), tenantID, roomID, requestedSessionID)
	if err != nil || playedOnly && !runtimeSession.BelongsToExecutionRealm(s.executionRealm) {
		writeError(w, http.StatusNotFound, "当前还没有可查看的直播回答历史")
		return
	}
	page := parseGeneratedSpeechInt(r.URL.Query().Get("page"), 1)
	pageSize := parseGeneratedSpeechInt(r.URL.Query().Get("page_size"), 10)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	result, err := s.store.ListGeneratedSpeechHistory(
		r.Context(), tenantID, roomID, runtimeSession.ID, query, page, pageSize, playedOnly,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取回答历史失败")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
