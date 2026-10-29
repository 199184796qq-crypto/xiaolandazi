package httpapi

import (
	"errors"
	"net/http"

	"livecompanion/management/internal/ttsscheduler"
)

func (s *Server) systemTTSPoolGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := s.requireAgentPromptAdmin(w, r); !ok {
		return
	}
	snapshot, err := s.store.TTSPoolSnapshot(r.Context())
	if err != nil {
		writeError(w, 500, "读取TTS账户池失败")
		return
	}
	writeJSON(w, 200, snapshot)
}
func (s *Server) systemTTSPoolSave(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.requireAgentPromptAdmin(w, r)
	if !ok {
		return
	}
	var input ttsscheduler.AccountInput
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "TTS账户配置格式错误")
		return
	}
	if err := s.store.SaveTTSAccount(r.Context(), input, actor.UserID); err != nil {
		if errors.Is(err, ttsscheduler.ErrRevision) {
			writeError(w, 409, err.Error())
		} else if errors.Is(err, ttsscheduler.ErrConfig) {
			writeError(w, 400, err.Error())
		} else {
			writeError(w, 500, "保存TTS账户失败，请稍后重试")
		}
		return
	}
	writeJSON(w, 200, map[string]bool{"saved": true})
}
