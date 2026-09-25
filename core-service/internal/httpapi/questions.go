package httpapi

import (
	"net/http"
	"strings"
	"time"

	"livecompanion/core/internal/questionqueue"
)

func (s *Server) SetQuestionQueue(queue *questionqueue.Queue) {
	if queue != nil {
		s.questions = queue
	}
}

func (s *Server) enqueueRoomQuestion(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	var input struct {
		Question string `json:"question"`
		Topic    string `json:"topic"`
		UserID   string `json:"user_id"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(input.Question) == "" {
		writeError(w, http.StatusBadRequest, "question is required")
		return
	}
	item, merged := s.questions.Enqueue(roomID, input.Question, input.Topic, input.UserID)
	writeJSON(w, http.StatusOK, map[string]any{
		"item":   item,
		"merged": merged,
		"ttl_ms": questionqueue.DefaultTTL.Milliseconds(),
	})
}

func (s *Server) listRoomQuestions(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  s.questions.List(roomID),
		"ttl_ms": questionqueue.DefaultTTL.Milliseconds(),
	})
}

func (s *Server) claimRoomQuestion(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	item, claimed := s.questions.ClaimNext(roomID)
	if !claimed {
		writeJSON(w, http.StatusOK, map[string]any{"item": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func (s *Server) completeRoomQuestion(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("questionID"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "question id is required")
		return
	}
	if !s.questions.Complete(roomID, id) {
		writeError(w, http.StatusNotFound, "question is no longer pending")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) releaseRoomQuestion(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("questionID"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "question id is required")
		return
	}
	var input struct {
		RetryAfterSeconds int `json:"retry_after_seconds"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.RetryAfterSeconds < 0 {
		input.RetryAfterSeconds = 0
	}
	if input.RetryAfterSeconds > 60 {
		input.RetryAfterSeconds = 60
	}
	item, released := s.questions.Release(roomID, id, time.Duration(input.RetryAfterSeconds)*time.Second)
	if !released {
		writeError(w, http.StatusNotFound, "question expired or no longer pending")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func (s *Server) dropRoomQuestion(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("questionID"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "question id is required")
		return
	}
	if !s.questions.Drop(roomID, id) {
		writeError(w, http.StatusNotFound, "question expired or no longer pending")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
