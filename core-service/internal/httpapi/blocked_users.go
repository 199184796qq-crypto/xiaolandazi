package httpapi

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"

	"livecompanion/core/internal/userblock"
)

func (s *Server) SetUserBlockStore(store *userblock.Store) {
	s.userBlocks = store
}

func (s *Server) listRoomBlockedUsers(w http.ResponseWriter, r *http.Request) {
	roomID, tenantID, ok := s.resolveBlockedUserRoom(w, r)
	if !ok {
		return
	}
	if s.userBlocks == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []userblock.Entry{}})
		return
	}
	items := s.userBlocks.List(roomID)
	filtered := make([]userblock.Entry, 0, len(items))
	for _, item := range items {
		if tenantID != nil && item.TenantID != *tenantID {
			continue
		}
		filtered = append(filtered, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": filtered})
}

func (s *Server) blockRoomUser(w http.ResponseWriter, r *http.Request) {
	roomID, tenantID, ok := s.resolveBlockedUserRoom(w, r)
	if !ok {
		return
	}
	if tenantID == nil {
		writeError(w, http.StatusBadRequest, "tenant_id is required")
		return
	}
	if s.userBlocks == nil {
		writeError(w, http.StatusServiceUnavailable, "blocked user store unavailable")
		return
	}

	var input struct {
		UserID   string `json:"user_id"`
		Nickname string `json:"nickname"`
		Reason   string `json:"reason"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.UserID = strings.TrimSpace(input.UserID)
	input.Nickname = strings.TrimSpace(input.Nickname)
	input.Reason = strings.TrimSpace(input.Reason)
	if userblock.SubjectKey(input.UserID, input.Nickname) == "" {
		writeError(w, http.StatusBadRequest, "user_id or nickname is required")
		return
	}
	if input.Reason == "" {
		input.Reason = "manual_block"
	}

	item, err := s.userBlocks.Block(
		r.Context(),
		*tenantID,
		roomID,
		input.UserID,
		input.Nickname,
		input.Reason,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "block user failed")
		return
	}
	if input.UserID != "" && s.questions != nil {
		s.questions.DropByUser(roomID, input.UserID)
	}
	if input.UserID != "" && s.brain != nil {
		if remover, ok := s.brain.(interface{ RemoveUser(int64, string) }); ok {
			remover.RemoveUser(roomID, input.UserID)
		}
	}
	log.Printf("[BLOCK_USER] room=%d tenant=%d subject=%s user_id=%q nickname=%q reason=%q", roomID, *tenantID, item.SubjectKey, item.UserID, item.Nickname, item.Reason)
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) restoreRoomBlockedUser(w http.ResponseWriter, r *http.Request) {
	roomID, _, ok := s.resolveBlockedUserRoom(w, r)
	if !ok {
		return
	}
	if s.userBlocks == nil {
		writeError(w, http.StatusServiceUnavailable, "blocked user store unavailable")
		return
	}

	var input struct {
		UserID   string `json:"user_id"`
		Nickname string `json:"nickname"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.UserID = strings.TrimSpace(input.UserID)
	input.Nickname = strings.TrimSpace(input.Nickname)
	if userblock.SubjectKey(input.UserID, input.Nickname) == "" {
		writeError(w, http.StatusBadRequest, "user_id or nickname is required")
		return
	}
	restored, err := s.userBlocks.Restore(r.Context(), roomID, input.UserID, input.Nickname)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "restore user failed")
		return
	}
	if !restored {
		writeError(w, http.StatusNotFound, "blocked user not found")
		return
	}
	log.Printf("[RESTORE_USER] room=%d subject=%s", roomID, userblock.SubjectKey(input.UserID, input.Nickname))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) resolveBlockedUserRoom(
	w http.ResponseWriter,
	r *http.Request,
) (int64, *int64, bool) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return 0, nil, false
	}
	tenantID, ok := optionalTenantID(w, r)
	if !ok {
		return 0, nil, false
	}
	if _, err := s.rooms.Get(r.Context(), tenantID, roomID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
			return 0, nil, false
		}
		writeError(w, http.StatusInternalServerError, "get room failed")
		return 0, nil, false
	}
	return roomID, tenantID, true
}

func (s *Server) eventIsBlocked(roomID int64, userID, nickname string) bool {
	return s.userBlocks != nil && s.userBlocks.IsBlocked(roomID, userID, nickname)
}
