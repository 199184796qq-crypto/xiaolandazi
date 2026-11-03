package httpapi

import (
	"errors"
	"net/http"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/ttsscheduler"
)

func (s *Server) systemTTSVoiceDisplay(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.requireAgentPromptAdmin(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		snapshot, err := s.store.TTSPoolSnapshot(r.Context())
		if err != nil {
			writeError(w, 500, "读取音色目录失败")
			return
		}
		displays, err := s.store.TTSVoiceDisplays(r.Context())
		if err != nil {
			writeError(w, 500, "读取声音展示资料失败")
			return
		}
		items := []db.TTSVoiceDisplay{}
		for _, voice := range highFidelityPoolVoices(snapshot.Accounts) {
			display, exists := displays[voice.ID]
			if !exists {
				display = db.TTSVoiceDisplay{VoiceID: voice.ID}
			}
			items = append(items, display)
		}
		writeJSON(w, 200, map[string]any{"items": items})
		return
	}
	var input db.TTSVoiceDisplay
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "声音展示资料格式错误")
		return
	}
	if _, err := s.findHighFidelityPoolVoice(r.Context(), input.VoiceID); err != nil {
		writeError(w, 400, "该音色当前不可用，请重新读取目录")
		return
	}
	result, err := s.store.SaveTTSVoiceDisplay(r.Context(), input, actor.UserID)
	if errors.Is(err, ttsscheduler.ErrRevision) {
		writeError(w, 409, "声音展示资料已被修改，请重新读取后保存")
		return
	}
	if errors.Is(err, ttsscheduler.ErrConfig) {
		writeError(w, 400, err.Error())
		return
	}
	if err != nil {
		writeError(w, 500, "保存声音展示资料失败")
		return
	}
	writeJSON(w, 200, result)
}
