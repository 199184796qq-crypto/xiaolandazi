package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

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
	runtimeSession, err := s.store.GetGeneratedSpeechHistorySession(r.Context(), tenantID, roomID, requestedSessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "当前还没有可查看的直播回答历史")
		return
	}
	page := parseGeneratedSpeechInt(r.URL.Query().Get("page"), 1)
	pageSize := parseGeneratedSpeechInt(r.URL.Query().Get("page_size"), 10)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	result, err := s.store.ListGeneratedSpeechHistory(
		r.Context(), tenantID, roomID, runtimeSession.ID, query, page, pageSize,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取回答历史失败")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
