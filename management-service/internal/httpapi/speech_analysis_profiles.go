package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"livecompanion/management/internal/model"
)

func (s *Server) liveAnalysisListProfiles(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "liveanalysis.view"); !ok {
		return
	}
	items, err := s.store.ListSpeechAnalysisProfiles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播分析配置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveAnalysisCreateProfile(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "liveanalysis.manage")
	if !ok {
		return
	}
	var input model.SpeechAnalysisProfileInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.CreateSpeechAnalysisProfileVersion(r.Context(), actor.UserID, input)
	if err != nil {
		message := strings.TrimSpace(err.Error())
		if strings.Contains(message, "不能为空") || strings.Contains(message, "最多") || strings.Contains(message, "不能超过") {
			writeError(w, http.StatusBadRequest, message)
			return
		}
		writeError(w, http.StatusInternalServerError, "保存直播分析配置失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) liveAnalysisActivateProfile(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "liveanalysis.manage")
	if !ok {
		return
	}
	profileID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("profileID")), 10, 64)
	if err != nil || profileID <= 0 {
		writeError(w, http.StatusBadRequest, "配置版本编号无效")
		return
	}
	item, err := s.store.ActivateSpeechAnalysisProfile(r.Context(), profileID, actor.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "配置版本不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "启用直播分析配置失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}
