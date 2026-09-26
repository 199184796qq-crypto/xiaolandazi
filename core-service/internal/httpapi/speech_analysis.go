package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"livecompanion/core/internal/speechanalysis"
)

func (s *Server) startRoomSpeechAnalysisJob(w http.ResponseWriter, r *http.Request) {
	if s.speechAnalysis == nil {
		writeError(w, http.StatusServiceUnavailable, "speech analysis unavailable")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	var input speechanalysis.JobInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.RoomID = roomID
	snapshot, err := s.speechAnalysis.Start(input)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "speech analysis unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, snapshot)
}

func (s *Server) getRoomSpeechAnalysisJob(w http.ResponseWriter, r *http.Request) {
	if s.speechAnalysis == nil {
		writeError(w, http.StatusServiceUnavailable, "speech analysis unavailable")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	taskID, err := strconv.ParseInt(r.PathValue("taskID"), 10, 64)
	if err != nil || taskID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	snapshot, ok := s.speechAnalysis.Get(taskID)
	if !ok || snapshot.RoomID != roomID {
		writeError(w, http.StatusNotFound, "speech analysis job not found")
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}
