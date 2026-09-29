package httpapi

import (
	"errors"
	"net/http"

	"livecompanion/core/internal/roomaudio"
)

func (s *Server) getRoomAudioEngine(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	if s.roomAudio == nil {
		writeJSON(w, http.StatusOK, roomaudio.Snapshot{RoomID: roomID, Phase: roomaudio.PhaseIdle})
		return
	}
	writeJSON(w, http.StatusOK, s.roomAudio.Snapshot(roomID))
}

func (s *Server) streamRoomCompositePCM(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	if s.roomAudio == nil {
		writeError(w, http.StatusServiceUnavailable, "房间实时合成音频引擎尚未初始化")
		return
	}
	frames, cancel, err := s.roomAudio.Subscribe(roomID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer cancel()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "audio/L16;rate=24000;channels=1")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Audio-Sample-Format", "s16le")
	w.Header().Set("X-Audio-Sample-Rate", "24000")
	w.Header().Set("X-Audio-Channels", "1")
	w.Header().Set("X-Audio-Frame-Duration-Ms", "20")
	w.Header().Set("X-Audio-Frame-Bytes", "960")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case frame, open := <-frames:
			if !open {
				return
			}
			if len(frame.PCM) != roomaudio.PCMBytesPerFrame {
				continue
			}
			if _, err := w.Write(frame.PCM); err != nil && !errors.Is(err, r.Context().Err()) {
				return
			}
			flusher.Flush()
		}
	}
}
