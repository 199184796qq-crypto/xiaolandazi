package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"livecompanion/core/internal/model"
)

type roomSessionDecisionInput struct {
	Action string `json:"action"`
}

func (s *Server) resolveRoomSessionDecision(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := optionalTenantID(w, r)
	if !ok {
		return
	}
	room, err := s.rooms.Get(r.Context(), tenantID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get room failed")
		return
	}
	var input roomSessionDecisionInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action != "merge" && action != "fresh" {
		writeError(w, http.StatusBadRequest, "action must be merge or fresh")
		return
	}

	stats, err := s.events.ResolveSessionDecision(r.Context(), room.TenantID, roomID, action, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if action == "fresh" {
		if s.brain != nil {
			s.brain.Reset(roomID)
		}
		if s.questions != nil {
			s.questions.ClearRoom(roomID)
		}
		if s.speechRuntime != nil {
			s.speechRuntime.Reset(roomID)
		}

		// The fresh decision may be made a few seconds after the stream has
		// already returned. Rehydrate only events belonging to the new
		// broadcast window so the active room brain does not lose those first
		// seconds while still keeping the previous raw events for review.
		items, listErr := s.events.ListRecent(r.Context(), &room.TenantID, roomID, 5000)
		if listErr == nil && s.brain != nil {
			sort.SliceStable(items, func(i, j int) bool {
				if items[i].OccurredAt.Equal(items[j].OccurredAt) {
					return items[i].ID < items[j].ID
				}
				return items[i].OccurredAt.Before(items[j].OccurredAt)
			})
			for _, item := range items {
				if item.OccurredAt.Before(stats.StartedAt) {
					continue
				}
				switch strings.ToLower(strings.TrimSpace(item.EventType)) {
				case "session_start", "session_end", "session_reopen", "session_resume":
					continue
				}
				s.brain.Ingest(item)
			}
		}
	}
	writeJSON(w, http.StatusOK, stats)
}

func filterEventsToActiveSession(items []model.RoomEvent, startedAt time.Time) []model.RoomEvent {
	if startedAt.IsZero() || len(items) == 0 {
		return items
	}
	filtered := items[:0]
	for _, item := range items {
		if item.OccurredAt.Before(startedAt) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}
