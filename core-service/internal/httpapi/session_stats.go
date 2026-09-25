package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
)

func (s *Server) getRoomSessionStats(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := optionalTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), tenantID, roomID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get room failed")
		return
	}
	stats, err := s.events.GetSessionStats(r.Context(), roomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get session stats failed")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
