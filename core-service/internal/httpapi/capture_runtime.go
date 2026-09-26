package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"livecompanion/core/internal/capture"
	"livecompanion/core/internal/model"
)

func (s *Server) captureRoom(w http.ResponseWriter, r *http.Request) (model.Room, bool) {
	if s.capture == nil {
		writeError(w, http.StatusServiceUnavailable, "capture manager is not configured")
		return model.Room{}, false
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return model.Room{}, false
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return model.Room{}, false
	}
	room, err := s.rooms.Get(r.Context(), &tenantID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
		} else {
			writeError(w, http.StatusInternalServerError, "get room failed")
		}
		return model.Room{}, false
	}
	return room, true
}

func (s *Server) getRoomCapture(w http.ResponseWriter, r *http.Request) {
	room, ok := s.captureRoom(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.capture.Snapshot(room.ID))
}

func (s *Server) startRoomAudioRecording(w http.ResponseWriter, r *http.Request) {
	room, ok := s.captureRoom(w, r)
	if !ok {
		return
	}
	if !s.collectors.IsServingRoom(room.ID) {
		writeError(w, http.StatusServiceUnavailable, "当前 Core 没有这个直播间的采集流")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	snapshot, err := s.capture.StartAudio(ctx, room)
	if err != nil {
		writeCaptureError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) stopRoomAudioRecording(w http.ResponseWriter, r *http.Request) {
	room, ok := s.captureRoom(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	snapshot, err := s.capture.StopAudio(ctx, room.ID)
	if err != nil && !errors.Is(err, capture.ErrRecordingMissing) {
		writeCaptureError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func safeRecordingDownloadPart(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer(
		"\\", "_",
		"/", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	).Replace(value)
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, value)
	return strings.TrimRight(strings.TrimSpace(value), ". ")
}

func roomRecordingDownloadName(room model.Room, snapshot capture.Snapshot) string {
	base := safeRecordingDownloadPart(room.Name)
	if base == "" {
		externalID := safeRecordingDownloadPart(room.ExternalRoomID)
		if externalID == "" {
			base = "直播间"
		} else {
			base = "直播间" + externalID
		}
	}

	startedAt := time.Now().UTC()
	if snapshot.Recording != nil && !snapshot.Recording.StartedAt.IsZero() {
		startedAt = snapshot.Recording.StartedAt
	}
	chinaTime := startedAt.In(time.FixedZone("CST", 8*60*60))
	return fmt.Sprintf("%s_%s.wav", base, chinaTime.Format("20060102_150405"))
}

func (s *Server) downloadRoomAudioRecording(w http.ResponseWriter, r *http.Request) {
	room, ok := s.captureRoom(w, r)
	if !ok {
		return
	}
	path, _, err := s.capture.RecordingFile(room.ID)
	if err != nil {
		writeCaptureError(w, err)
		return
	}
	downloadName := roomRecordingDownloadName(room, s.capture.Snapshot(room.ID))
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set(
		"Content-Disposition",
		"attachment; filename=\"recording.wav\"; filename*=UTF-8''"+url.PathEscape(downloadName),
	)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}

func writeCaptureError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, capture.ErrCaptureBusy):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, capture.ErrRecordingMissing):
		writeError(w, http.StatusConflict, "当前没有正在录制的声音任务")
	case errors.Is(err, capture.ErrRecordingNotReady):
		writeError(w, http.StatusConflict, "录音文件还没有完成")
	default:
		writeError(w, http.StatusServiceUnavailable, err.Error())
	}
}
