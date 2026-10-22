package httpapi

import (
	"log"
	"net/http"
	"strings"

	"livecompanion/core/internal/contentblock"
)

func (s *Server) SetContentBlockStore(store *contentblock.Store) {
	s.contentBlocks = store
}

func (s *Server) listRoomBlockedContents(w http.ResponseWriter, r *http.Request) {
	roomID, tenantID, ok := s.resolveBlockedUserRoom(w, r)
	if !ok {
		return
	}
	if s.contentBlocks == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []contentblock.Entry{}})
		return
	}
	items := s.contentBlocks.List(roomID)
	filtered := make([]contentblock.Entry, 0, len(items))
	for _, item := range items {
		if tenantID != nil && item.TenantID != *tenantID {
			continue
		}
		filtered = append(filtered, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": filtered})
}

func (s *Server) blockRoomContent(w http.ResponseWriter, r *http.Request) {
	roomID, tenantID, ok := s.resolveBlockedUserRoom(w, r)
	if !ok {
		return
	}
	if tenantID == nil {
		writeError(w, http.StatusBadRequest, "tenant_id is required")
		return
	}
	if s.contentBlocks == nil {
		writeError(w, http.StatusServiceUnavailable, "blocked content store unavailable")
		return
	}

	var input struct {
		Content string `json:"content"`
		Reason  string `json:"reason"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.Content = strings.TrimSpace(input.Content)
	input.Reason = strings.TrimSpace(input.Reason)
	if contentblock.ContentKey(input.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if input.Reason == "" {
		input.Reason = "manual_block_content"
	}

	item, err := s.contentBlocks.Block(r.Context(), *tenantID, roomID, input.Content, input.Reason)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "block content failed")
		return
	}
	log.Printf("[BLOCK_CONTENT] room=%d tenant=%d key=%s chars=%d reason=%q",
		roomID, *tenantID, item.ContentKey, len([]rune(item.Content)), item.Reason)
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) restoreRoomBlockedContent(w http.ResponseWriter, r *http.Request) {
	roomID, _, ok := s.resolveBlockedUserRoom(w, r)
	if !ok {
		return
	}
	if s.contentBlocks == nil {
		writeError(w, http.StatusServiceUnavailable, "blocked content store unavailable")
		return
	}

	var input struct {
		ContentKey string `json:"content_key"`
		Content    string `json:"content"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	key := strings.TrimSpace(input.ContentKey)
	if key == "" {
		key = contentblock.ContentKey(input.Content)
	}
	if key == "" {
		writeError(w, http.StatusBadRequest, "content_key or content is required")
		return
	}
	restored, err := s.contentBlocks.Restore(r.Context(), roomID, key, input.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "restore content failed")
		return
	}
	if !restored {
		writeError(w, http.StatusNotFound, "blocked content not found")
		return
	}
	log.Printf("[RESTORE_CONTENT] room=%d key=%s", roomID, key)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// eventContentBlocked 判断这条公屏内容是否命中本房间的屏蔽内容名单。
func (s *Server) eventContentBlocked(roomID int64, content string) bool {
	if s.contentBlocks == nil || strings.TrimSpace(content) == "" {
		return false
	}
	return s.contentBlocks.IsBlocked(roomID, content)
}
