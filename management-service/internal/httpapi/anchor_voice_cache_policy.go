package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func (s *Server) anchorVoiceCacheTTL(ctx context.Context, tenants ...int64) (time.Duration, error) {
	if s.store == nil {
		return 5 * time.Hour, nil
	} // Isolated unit tests only.
	var hours int
	var err error
	if len(tenants) > 0 {
		hours, err = s.store.AnchorVoiceCacheRetentionHoursForTenant(ctx, tenants[0])
	} else {
		hours, err = s.store.AnchorVoiceCacheRetentionHours(ctx)
	}
	return time.Duration(hours) * time.Hour, err
}

func (s *Server) mainlineCacheReuseEnabled(ctx context.Context, tenants ...int64) bool {
	reader := s.generationCachePolicy
	if reader == nil && s.store != nil {
		reader = s.store
	}
	if reader == nil {
		return true
	} // Isolated fixtures keep existing behavior.
	if scoped, ok := reader.(model.TenantGenerationCachePolicyReader); ok && len(tenants) > 0 {
		enabled, err := scoped.GenerationCacheReuseEnabledForTenant(ctx, tenants[0], model.MainlineGenerationCacheReuseSetting)
		return err == nil && enabled
	}
	enabled, err := reader.GenerationCacheReuseEnabled(ctx, model.MainlineGenerationCacheReuseSetting)
	// Configuration failure must select fresh generation, never stop supply.
	return err == nil && enabled
}

func (s *Server) anchorVoiceCachePolicyReply(w http.ResponseWriter, r *http.Request, hours int) {
	mainline, err := s.store.GenerationCacheReuseEnabled(r.Context(), model.MainlineGenerationCacheReuseSetting)
	if err != nil {
		writeError(w, 503, "读取主线缓存复用配置失败")
		return
	}
	interaction, err := s.store.GenerationCacheReuseEnabled(r.Context(), model.InteractionGenerationCacheReuseSetting)
	if err != nil {
		writeError(w, 503, "读取互动缓存复用配置失败")
		return
	}
	writeJSON(w, 200, map[string]any{"retention_hours": hours, "default_hours": db.DefaultAnchorVoiceCacheHours, "applies_to": "new_cache_entries", "mainline_cache_reuse_enabled": mainline, "interaction_cache_reuse_enabled": interaction, "reuse_applies_to": "new_requests"})
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
	s.anchorVoiceCachePolicyReply(w, r, hours)
}

func (s *Server) systemAnchorVoiceCachePolicyPut(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage")
	if !ok {
		return
	}
	var input struct {
		RetentionHours   int   `json:"retention_hours"`
		MainlineReuse    *bool `json:"mainline_cache_reuse_enabled"`
		InteractionReuse *bool `json:"interaction_cache_reuse_enabled"`
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
	updates := []model.SystemSettingUpdate{{Key: db.AnchorVoiceCacheRetentionSetting, Value: value}}
	if input.MainlineReuse != nil {
		updates = append(updates, model.SystemSettingUpdate{Key: model.MainlineGenerationCacheReuseSetting, Value: strconv.FormatBool(*input.MainlineReuse)})
	}
	if input.InteractionReuse != nil {
		updates = append(updates, model.SystemSettingUpdate{Key: model.InteractionGenerationCacheReuseSetting, Value: strconv.FormatBool(*input.InteractionReuse)})
	}
	if err := s.store.UpdateSystemSettings(r.Context(), updates, actor.UserID); err != nil {
		writeError(w, 503, "保存声音缓存运维配置失败")
		return
	}
	s.anchorVoiceCachePolicyReply(w, r, input.RetentionHours)
}
