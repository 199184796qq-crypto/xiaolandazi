package httpapi

import (
	"net/http"
	"strconv"

	appdb "livecompanion/management/internal/db"
)

func (s *Server) liveRoomReview(w http.ResponseWriter, r *http.Request) {
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
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	summary, events, err := s.store.LoadLatestLiveReview(r.Context(), tenantID, roomID, limit)
	if err != nil {
		if appdb.IsLiveReviewNotFound(err) {
			writeError(w, http.StatusNotFound, "当前直播间还没有可复盘的归档场次")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取直播复盘归档失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"summary": summary,
		"events":  events,
	})
}
