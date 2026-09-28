package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func validateLiveAgentPlanScriptReferenceInput(key, title, content, goal, transition, executionMode string) error {
	key = strings.TrimSpace(key)
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	if key == "" {
		return errors.New("话术参考标识不能为空")
	}
	if title == "" {
		return errors.New("话术参考标题不能为空")
	}
	if content == "" {
		return errors.New("话术参考内容不能为空")
	}
	if utf8.RuneCountInString(key) > 255 {
		return errors.New("话术参考标识不能超过 255 字")
	}
	if utf8.RuneCountInString(title) > 255 {
		return errors.New("话术参考标题不能超过 255 字")
	}
	if utf8.RuneCountInString(content) > 12000 {
		return errors.New("单条话术参考不能超过 12000 字")
	}
	if utf8.RuneCountInString(goal) > 512 || utf8.RuneCountInString(transition) > 512 {
		return errors.New("话术参考目标或转场不能超过 512 字")
	}
	mode := strings.ToLower(strings.TrimSpace(executionMode))
	if mode != "" && mode != "intent" && mode != "verbatim" {
		return errors.New("执行方式只能是 intent 或 verbatim")
	}
	return nil
}

func (s *Server) liveAgentPlanScriptReferenceList(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return
	}
	items, err := s.store.ListLiveAgentPlanScriptReferences(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取正式话术参考失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveAgentPlanScriptReferenceCreate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input model.CreateLiveAgentPlanScriptReferenceInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "新增话术参考格式错误")
		return
	}
	if err := validateLiveAgentPlanScriptReferenceInput(input.ReferenceKey, input.Title, input.ContentText, input.Goal, input.Transition, input.ExecutionMode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	item, err := s.store.CreateLiveAgentPlanScriptReference(r.Context(), tenantID, planID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanScriptReferenceAlreadyExists) {
		writeError(w, http.StatusConflict, "当前方案已经存在同标识话术参考")
		return
	}
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "新增正式话术参考失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func liveAgentPlanScriptReferencePathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value := strings.TrimSpace(r.PathValue("referenceID"))
	referenceID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || referenceID <= 0 {
		writeError(w, http.StatusBadRequest, "话术参考编号无效")
		return 0, false
	}
	return referenceID, true
}

func (s *Server) liveAgentPlanScriptReferenceUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	referenceID, ok := liveAgentPlanScriptReferencePathID(w, r)
	if !ok {
		return
	}
	var input model.UpdateLiveAgentPlanScriptReferenceInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "修改话术参考格式错误")
		return
	}
	if err := validateLiveAgentPlanScriptReferenceInput(input.ReferenceKey, input.Title, input.ContentText, input.Goal, input.Transition, input.ExecutionMode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	item, err := s.store.UpdateLiveAgentPlanScriptReference(r.Context(), tenantID, planID, referenceID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanScriptReferenceNotFound) {
		writeError(w, http.StatusNotFound, "正式话术参考不存在")
		return
	}
	if errors.Is(err, appdb.ErrLiveAgentPlanScriptReferenceVersionConflict) {
		writeError(w, http.StatusConflict, "话术参考已经产生新版本，请刷新后重新修改")
		return
	}
	if errors.Is(err, appdb.ErrLiveAgentPlanScriptReferenceAlreadyExists) {
		writeError(w, http.StatusConflict, "当前方案已经存在同标识话术参考")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "修改正式话术参考失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveAgentPlanScriptReferenceDelete(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	referenceID, ok := liveAgentPlanScriptReferencePathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return
	}
	expectedVersion, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("expected_version_no")), 10, 64)
	err := s.store.DeleteLiveAgentPlanScriptReferenceWithExpectedVersion(
		r.Context(), tenantID, planID, referenceID, actor.UserID, expectedVersion,
	)
	if errors.Is(err, appdb.ErrLiveAgentPlanScriptReferenceNotFound) {
		writeError(w, http.StatusNotFound, "正式话术参考不存在")
		return
	}
	if errors.Is(err, appdb.ErrLiveAgentPlanScriptReferenceVersionConflict) {
		writeError(w, http.StatusConflict, "话术参考已经产生新版本，请刷新后重新停用")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "停用正式话术参考失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
