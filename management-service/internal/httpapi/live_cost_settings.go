package httpapi

import (
	"errors"
	"io"
	"net/http"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/livecost"
)

func (s *Server) systemLiveCostSettingsGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := s.requireAgentPromptAdmin(w, r); !ok {
		return
	}
	config, err := s.store.LiveCostSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取直播费用配置失败，请稍后重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": config})
}

func (s *Server) systemLiveCostSettingsPut(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.requireAgentPromptAdmin(w, r)
	if !ok {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, livecost.MaxSettingsJSONBytes+1))
	if err != nil || len(raw) > livecost.MaxSettingsJSONBytes {
		writeError(w, http.StatusBadRequest, "费用配置内容过长或读取失败")
		return
	}
	input, err := livecost.DecodeSettings(string(raw))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	config, err := s.store.SaveLiveCostSettings(r.Context(), input, actor.UserID)
	if err != nil {
		if errors.Is(err, db.ErrLiveCostRevision) {
			writeError(w, http.StatusConflict, db.ErrLiveCostRevision.Error())
			return
		}
		writeError(w, http.StatusServiceUnavailable, "保存直播费用配置失败，请重新读取后重试")
		return
	}
	// Refresh this process through the same validated runtime publication path;
	// other processes load the persisted revision at their next request boundary.
	_, _ = s.liveCostSettingsSnapshot(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"config": config})
}
