package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"livecompanion/management/internal/model"
)

// roomQuestionLibrary lists, creates, updates and deletes question-library rows.
func (s *Server) roomQuestionLibrary(w http.ResponseWriter, r *http.Request) {
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
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListQuestionLibrary(r.Context(), tenantID, roomID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取问题库失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost, http.MethodPut:
		var input model.QuestionLibraryInput
		if err := readJSON(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "问题库格式错误")
			return
		}
		if strings.TrimSpace(input.Question) == "" || strings.TrimSpace(input.Answer) == "" {
			writeError(w, http.StatusBadRequest, "问题和回答不能为空")
			return
		}
		id := int64(0)
		if r.Method == http.MethodPut {
			id, _ = strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("id")), 10, 64)
			if id <= 0 {
				writeError(w, http.StatusBadRequest, "缺少问题库条目 id")
				return
			}
		}
		item, err := s.store.UpsertQuestionLibrary(r.Context(), tenantID, roomID, actor.UserID, id, input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "保存问题库失败")
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodDelete:
		id, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("id")), 10, 64)
		if id <= 0 {
			writeError(w, http.StatusBadRequest, "缺少问题库条目 id")
			return
		}
		if err := s.store.DeleteQuestionLibrary(r.Context(), tenantID, roomID, id); err != nil {
			writeError(w, http.StatusInternalServerError, "删除问题库失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "不支持的方法")
	}
}
