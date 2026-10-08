package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func (s *Server) anchorVoiceCacheTTL(ctx context.Context) (time.Duration, error) {
	if s.store == nil {
		return 5 * time.Hour, nil
	} // Isolated unit tests only.
	hours, err := s.store.AnchorVoiceCacheRetentionHours(ctx)
	return time.Duration(hours) * time.Hour, err
}

func (s *Server) systemAnchorVoiceCachePolicyGet(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage"); !ok {
		return
	}
	hours, err := s.store.AnchorVoiceCacheRetentionHours(r.Context())
	if err != nil {
		writeError(w, 503, "读取声音缓存运维配置失败")
		return
	}
	writeJSON(w, 200, map[string]any{"retention_hours": hours, "default_hours": db.DefaultAnchorVoiceCacheHours, "applies_to": "new_cache_entries"})
}

func (s *Server) systemAnchorVoiceCachePolicyPut(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage")
	if !ok {
		return
	}
	var input struct {
		RetentionHours int `json:"retention_hours"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "声音缓存配置格式错误")
		return
	}
	value := strconv.Itoa(input.RetentionHours)
	if _, err := db.ParseAnchorVoiceCacheHours(value); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := s.store.UpdateSystemSettings(r.Context(), []model.SystemSettingUpdate{{Key: db.AnchorVoiceCacheRetentionSetting, Value: value}}, actor.UserID); err != nil {
		writeError(w, 503, "保存声音缓存运维配置失败")
		return
	}
	writeJSON(w, 200, map[string]any{"retention_hours": input.RetentionHours, "default_hours": db.DefaultAnchorVoiceCacheHours, "applies_to": "new_cache_entries"})
}
