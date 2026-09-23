package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

type featurePermissionSpec struct {
	View   string
	Manage string
}

var featurePermissionSpecs = map[string]featurePermissionSpec{
	"agent-levels":    {View: "agent.view_all", Manage: "system.architecture.view"},
	"agent-contracts": {View: "agent.view_all", Manage: "system.architecture.view"},
	"agent-exit":      {View: "agent.view_all", Manage: "system.architecture.view"},
}

func (s *Server) adminListFeatureRecords(w http.ResponseWriter, r *http.Request) {
	featureKey, spec, ok := s.featurePermission(w, r, false)
	if !ok {
		return
	}
	_ = spec

	items, err := s.store.ListFeatureRecords(r.Context(), featureKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取配置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminCreateFeatureRecord(w http.ResponseWriter, r *http.Request) {
	featureKey, _, ok := s.featurePermission(w, r, true)
	if !ok {
		return
	}
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}

	input, ok := readFeatureRecordInput(w, r)
	if !ok {
		return
	}

	item, err := s.store.CreateFeatureRecord(
		r.Context(),
		featureKey,
		actor.UserID,
		input,
	)
	if err != nil {
		if isDuplicateDBError(err) {
			writeError(w, http.StatusConflict, "内部编码已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "创建配置失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) adminUpdateFeatureRecord(w http.ResponseWriter, r *http.Request) {
	featureKey, _, ok := s.featurePermission(w, r, true)
	if !ok {
		return
	}
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	recordID, ok := featureRecordID(w, r)
	if !ok {
		return
	}
	input, ok := readFeatureRecordInput(w, r)
	if !ok {
		return
	}

	item, err := s.store.UpdateFeatureRecord(
		r.Context(),
		featureKey,
		recordID,
		actor.UserID,
		input,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "配置不存在")
		case isDuplicateDBError(err):
			writeError(w, http.StatusConflict, "内部编码已存在")
		default:
			writeError(w, http.StatusInternalServerError, "保存配置失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminDeleteFeatureRecord(w http.ResponseWriter, r *http.Request) {
	featureKey, _, ok := s.featurePermission(w, r, true)
	if !ok {
		return
	}
	recordID, ok := featureRecordID(w, r)
	if !ok {
		return
	}

	if err := s.store.DeleteFeatureRecord(r.Context(), featureKey, recordID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "配置不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "删除配置失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) featurePermission(
	w http.ResponseWriter,
	r *http.Request,
	manage bool,
) (string, featurePermissionSpec, bool) {
	featureKey := strings.TrimSpace(r.PathValue("featureKey"))
	spec, exists := featurePermissionSpecs[featureKey]
	if !exists {
		writeError(w, http.StatusNotFound, "未知功能配置")
		return "", featurePermissionSpec{}, false
	}

	permission := spec.View
	if manage {
		permission = spec.Manage
	}
	if _, _, ok := s.requireStaffPermission(w, r, permission); !ok {
		return "", featurePermissionSpec{}, false
	}
	return featureKey, spec, true
}

func readFeatureRecordInput(
	w http.ResponseWriter,
	r *http.Request,
) (model.FeatureRecordInput, bool) {
	var input model.FeatureRecordInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return model.FeatureRecordInput{}, false
	}

	input.RecordKey = strings.ToLower(strings.TrimSpace(input.RecordKey))
	input.Title = strings.TrimSpace(input.Title)
	input.Status = strings.TrimSpace(input.Status)
	input.PayloadJSON = strings.TrimSpace(input.PayloadJSON)

	if input.RecordKey == "" || len(input.RecordKey) > 128 {
		writeError(w, http.StatusBadRequest, "内部编码不能为空且最多 128 个字符")
		return model.FeatureRecordInput{}, false
	}
	if utf8.RuneCountInString(input.Title) < 1 || utf8.RuneCountInString(input.Title) > 160 {
		writeError(w, http.StatusBadRequest, "名称需为 1-160 个字符")
		return model.FeatureRecordInput{}, false
	}
	if input.Status == "" {
		input.Status = "active"
	}
	switch input.Status {
	case "active", "inactive", "draft", "pending", "completed", "cancelled":
	default:
		writeError(w, http.StatusBadRequest, "状态不支持")
		return model.FeatureRecordInput{}, false
	}
	if input.PayloadJSON == "" {
		input.PayloadJSON = "{}"
	}
	var parsed any
	if err := json.Unmarshal([]byte(input.PayloadJSON), &parsed); err != nil {
		writeError(w, http.StatusBadRequest, "扩展配置必须是有效 JSON")
		return model.FeatureRecordInput{}, false
	}

	return input, true
}

func featureRecordID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("recordID")), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "配置 ID 无效")
		return 0, false
	}
	return value, true
}
