package httpapi

import (
	"database/sql"
	"errors"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
	"strconv"
)

func (s *Server) liveOpsUserVoiceCachePolicies(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage"); !ok {
		return
	}
	items, err := s.store.ListUserVoiceCachePolicies(r.Context())
	if err != nil {
		writeError(w, 503, "读取用户声音缓存设置失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) liveOpsUpdateUserVoiceCachePolicy(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage")
	if !ok {
		return
	}
	tenant, err := strconv.ParseInt(r.PathValue("tenantID"), 10, 64)
	if err != nil || tenant <= 0 {
		writeError(w, 400, "用户账号无效")
		return
	}
	var input struct {
		UsingDefault *bool `json:"using_default"`
		Mainline     *bool `json:"mainline_cache_reuse_enabled"`
		Interaction  *bool `json:"interaction_cache_reuse_enabled"`
		Hours        *int  `json:"retention_hours"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "用户声音缓存设置格式错误")
		return
	}
	var settings *model.UserVoiceCacheSettings
	if input.UsingDefault == nil || !*input.UsingDefault {
		if input.Mainline == nil || input.Interaction == nil || input.Hours == nil {
			writeError(w, 400, "请完整设置主线缓存、互动缓存和保留时间")
			return
		}
		settings = &model.UserVoiceCacheSettings{MainlineReuse: *input.Mainline, InteractionReuse: *input.Interaction, RetentionHours: *input.Hours}
		if _, err := db.ParseAnchorVoiceCacheHours(strconv.Itoa(*input.Hours)); err != nil {
			writeError(w, 400, "保留时间须为1～168小时的整数")
			return
		}
	} else if input.Mainline != nil || input.Interaction != nil || input.Hours != nil {
		writeError(w, 400, "恢复平台默认时无需填写用户单独设置")
		return
	}
	err = s.store.UpdateUserVoiceCachePolicy(r.Context(), tenant, actor.UserID, settings)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "用户不存在")
		return
	}
	if err != nil {
		writeError(w, 503, "保存用户声音缓存失败，请稍后重试")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"saved": true})
}
