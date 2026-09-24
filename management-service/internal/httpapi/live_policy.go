package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
)

func (s *Server) requireLivePolicyView(w http.ResponseWriter, r *http.Request) (model.Actor, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, false
	}
	if actor.IsPlatformAdmin() {
		return actor, true
	}
	if !actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "仅有权限的内部管理人员可查看系统与行业策略")
		return model.Actor{}, false
	}
	access, err := s.staffAccessForActor(r, actor)
	if err != nil || !staffHasPermission(access, "livepolicy.view") {
		writeError(w, http.StatusForbidden, "当前角色没有策略查看权限")
		return model.Actor{}, false
	}
	return actor, true
}

func (s *Server) requireLivePolicyLayerManage(
	w http.ResponseWriter,
	r *http.Request,
	layer string,
) (model.Actor, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, false
	}
	layer = strings.ToUpper(strings.TrimSpace(layer))
	if layer == model.LivePolicyLayerL1 {
		if !actor.IsPlatformAdmin() {
			writeError(w, http.StatusForbidden, "L1 系统强制规则仅超级系统管理员可发布")
			return model.Actor{}, false
		}
		return actor, true
	}
	if layer != model.LivePolicyLayerL2 {
		writeError(w, http.StatusBadRequest, "管理端只允许维护 L1 或 L2")
		return model.Actor{}, false
	}
	if actor.IsPlatformAdmin() {
		return actor, true
	}
	if !actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "仅有权限的内部管理人员可维护行业策略")
		return model.Actor{}, false
	}
	access, err := s.staffAccessForActor(r, actor)
	if err != nil || !staffHasPermission(access, "livepolicy.manage_l2") {
		writeError(w, http.StatusForbidden, "当前角色没有行业策略维护权限")
		return model.Actor{}, false
	}
	return actor, true
}

func (s *Server) requireCustomerPolicyRoom(
	w http.ResponseWriter,
	r *http.Request,
) (model.Actor, int64, int64, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, 0, 0, false
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "L3 直播间策略只能由对应终端账号维护")
		return model.Actor{}, 0, 0, false
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return model.Actor{}, 0, 0, false
	}
	tenantID := *actor.TenantID
	if _, err := s.getCoreRoomState(r.Context(), tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "直播间不存在或不属于当前终端")
		return model.Actor{}, 0, 0, false
	}
	return actor, tenantID, roomID, true
}

func (s *Server) livePolicyIndustries(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireLivePolicyView(w, r); !ok {
		return
	}
	items, err := s.store.ListLivePolicyIndustries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取行业策略目录失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) livePolicyUpsertIndustry(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireLivePolicyLayerManage(w, r, model.LivePolicyLayerL2)
	if !ok {
		return
	}
	var input model.LivePolicyIndustry
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "行业信息格式错误")
		return
	}
	item, err := s.store.UpsertLivePolicyIndustry(r.Context(), actor.UserID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "保存行业信息失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) livePolicyBindTenantIndustry(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireLivePolicyLayerManage(w, r, model.LivePolicyLayerL2)
	if !ok {
		return
	}
	tenantID, ok := namedPathID(w, r, "tenantID", "终端")
	if !ok {
		return
	}
	var input struct {
		IndustryCode string `json:"industry_code"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "行业绑定格式错误")
		return
	}
	if err := s.store.BindTenantLivePolicyIndustry(
		r.Context(), tenantID, input.IndustryCode, actor.UserID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "所选行业不存在")
			return
		}
		writeError(w, http.StatusBadRequest, "绑定终端行业失败")
		return
	}
	s.refreshRunningPolicySnapshots(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id":     tenantID,
		"industry_code": strings.ToLower(strings.TrimSpace(input.IndustryCode)),
	})
}

func (s *Server) livePolicyAdminContext(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireLivePolicyView(w, r)
	if !ok {
		return
	}
	layer := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("layer")))
	if layer == "" {
		layer = model.LivePolicyLayerL1
	}
	if layer != model.LivePolicyLayerL1 && layer != model.LivePolicyLayerL2 {
		writeError(w, http.StatusBadRequest, "管理端只能查看 L1 或 L2")
		return
	}
	industryCode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("industry_code")))
	if layer == model.LivePolicyLayerL2 && industryCode == "" {
		industryCode = "general"
	}
	if _, err := s.store.EnsureLivePolicyScope(
		r.Context(), layer, industryCode, 0, 0, actor.UserID, "",
	); err != nil {
		writeError(w, http.StatusBadRequest, "初始化策略作用域失败")
		return
	}
	context, err := s.store.GetLivePolicyContext(r.Context(), layer, industryCode, 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取策略配置失败")
		return
	}
	writeJSON(w, http.StatusOK, context)
}

func (s *Server) livePolicyAdminCreateDraft(w http.ResponseWriter, r *http.Request) {
	var input model.CreateLivePolicyDraftInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "策略草稿格式错误")
		return
	}
	input.Layer = strings.ToUpper(strings.TrimSpace(input.Layer))
	actor, ok := s.requireLivePolicyLayerManage(w, r, input.Layer)
	if !ok {
		return
	}
	if input.Layer == model.LivePolicyLayerL2 {
		input.IndustryCode = strings.ToLower(strings.TrimSpace(input.IndustryCode))
		if input.IndustryCode == "" {
			writeError(w, http.StatusBadRequest, "L2 草稿必须指定行业")
			return
		}
	}
	var l1 *model.LivePolicyVersion
	if input.Layer == model.LivePolicyLayerL2 {
		l1, _ = s.store.GetActiveLivePolicyVersion(r.Context(), model.LivePolicyLayerL1, "", 0, 0)
	}
	input.Conflicts = append(
		input.Conflicts,
		policy.ValidateDraft(input.Layer, input.Rules, input.Overrides, l1)...,
	)
	item, err := s.store.CreateLivePolicyDraft(r.Context(), actor.UserID, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存策略草稿失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) livePolicyAdminPublish(w http.ResponseWriter, r *http.Request) {
	versionID, ok := namedPathID(w, r, "versionID", "策略版本")
	if !ok {
		return
	}
	version, err := s.store.GetLivePolicyVersion(r.Context(), versionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "策略版本不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取策略版本失败")
		return
	}
	scope, err := s.store.GetLivePolicyScopeByID(r.Context(), version.PolicyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取策略作用域失败")
		return
	}
	actor, ok := s.requireLivePolicyLayerManage(w, r, scope.Layer)
	if !ok {
		return
	}
	l1, _ := s.store.GetActiveLivePolicyVersion(r.Context(), model.LivePolicyLayerL1, "", 0, 0)
	conflicts := append(
		[]model.LivePolicyConflict{},
		version.Conflicts...,
	)
	conflicts = append(conflicts, policy.ValidateDraft(scope.Layer, version.Rules, version.Overrides, l1)...)
	if len(conflicts) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":     "当前草稿存在冲突，不能发布",
			"conflicts": conflicts,
		})
		return
	}
	item, err := s.store.PublishLivePolicyVersion(r.Context(), versionID, actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "发布策略版本失败")
		return
	}
	s.refreshRunningPolicySnapshots(r.Context())
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) livePolicyAdminRollback(w http.ResponseWriter, r *http.Request) {
	versionID, ok := namedPathID(w, r, "versionID", "策略版本")
	if !ok {
		return
	}
	version, err := s.store.GetLivePolicyVersion(r.Context(), versionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "策略版本不存在")
		return
	}
	scope, err := s.store.GetLivePolicyScopeByID(r.Context(), version.PolicyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取策略作用域失败")
		return
	}
	actor, ok := s.requireLivePolicyLayerManage(w, r, scope.Layer)
	if !ok {
		return
	}
	l1, _ := s.store.GetActiveLivePolicyVersion(r.Context(), model.LivePolicyLayerL1, "", 0, 0)
	conflicts := policy.ValidateDraft(scope.Layer, version.Rules, version.Overrides, l1)
	if len(conflicts) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":     "历史版本与当前 L1 存在冲突，不能直接回滚",
			"conflicts": conflicts,
		})
		return
	}
	item, err := s.store.RollbackLivePolicyVersion(r.Context(), versionID, actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "回滚策略版本失败")
		return
	}
	s.refreshRunningPolicySnapshots(r.Context())
	writeJSON(w, http.StatusOK, item)
}

func customerSafePolicyConflicts(
	conflicts []model.LivePolicyConflict,
) []model.LivePolicyConflict {
	if len(conflicts) == 0 {
		return []model.LivePolicyConflict{}
	}
	return []model.LivePolicyConflict{{
		Code:    "policy_boundary",
		Message: "该调整与平台策略边界冲突，请修改直播间自定义策略后重试",
	}}
}

func customerSafeLivePolicyVersion(
	version model.LivePolicyVersion,
) model.LivePolicyVersion {
	version.Conflicts = customerSafePolicyConflicts(version.Conflicts)
	return version
}

func customerSafeLivePolicyVersionPtr(
	version *model.LivePolicyVersion,
) *model.LivePolicyVersion {
	if version == nil {
		return nil
	}
	safe := customerSafeLivePolicyVersion(*version)
	return &safe
}

func customerSafeEffectivePolicy(
	effective model.LiveEffectivePolicy,
) model.LiveEffectivePolicy {
	rules := make([]model.LiveEffectivePolicyRule, 0)
	for _, rule := range effective.Rules {
		if rule.SourceLayer != model.LivePolicyLayerL3 {
			continue
		}
		rules = append(rules, rule)
	}
	return model.LiveEffectivePolicy{
		IndustryCode: effective.IndustryCode,
		L3:           customerSafeLivePolicyVersionPtr(effective.L3),
		Rules:        rules,
		Conflicts:    []model.LivePolicyConflict{},
		PromptText:   "",
	}
}

func customerSafePolicyPrompt(effective model.LiveEffectivePolicy) string {
	safe := customerSafeEffectivePolicy(effective)
	if len(safe.Rules) == 0 {
		return "平台上层直播规则由服务端强制执行且不会向终端会话暴露。当前直播间暂无客户自定义 L3 规则。"
	}
	return strings.TrimSpace(
		"平台上层直播规则由服务端强制执行且不会向终端会话暴露。\n\n" +
			policy.RenderPrompt(safe),
	)
}

func (s *Server) roomPolicyContext(
	r *http.Request,
	tenantID, roomID, actorUserID int64,
) (model.LiveRoomPolicyContext, error) {
	scope, err := s.store.EnsureLivePolicyScope(
		r.Context(), model.LivePolicyLayerL3, "", tenantID, roomID, actorUserID, "",
	)
	if err != nil {
		return model.LiveRoomPolicyContext{}, err
	}
	versions, err := s.store.ListLivePolicyVersions(r.Context(), scope.ID)
	if err != nil {
		return model.LiveRoomPolicyContext{}, err
	}
	for index := range versions {
		versions[index] = customerSafeLivePolicyVersion(versions[index])
	}
	industryCode, l1, l2, l3, err := s.store.LoadLivePolicyLayers(r.Context(), tenantID, roomID)
	if err != nil {
		return model.LiveRoomPolicyContext{}, err
	}
	effective := policy.BuildEffective(industryCode, l1, l2, l3)
	return model.LiveRoomPolicyContext{
		IndustryCode: industryCode,
		Effective:    customerSafeEffectivePolicy(effective),
		Versions:     versions,
		L3Scope:      &scope,
	}, nil
}

func (s *Server) liveRoomPolicyContext(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, roomID, ok := s.requireCustomerPolicyRoom(w, r)
	if !ok {
		return
	}
	context, err := s.roomPolicyContext(r, tenantID, roomID, actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播间策略失败")
		return
	}
	writeJSON(w, http.StatusOK, context)
}

func (s *Server) liveRoomPolicyCreateDraft(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, roomID, ok := s.requireCustomerPolicyRoom(w, r)
	if !ok {
		return
	}
	var input model.CreateLivePolicyDraftInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "L3 策略草稿格式错误")
		return
	}
	input.Layer = model.LivePolicyLayerL3
	input.TenantID = tenantID
	input.RoomID = roomID
	input.Rules = nil
	l1, _ := s.store.GetActiveLivePolicyVersion(r.Context(), model.LivePolicyLayerL1, "", 0, 0)
	input.Conflicts = append(
		input.Conflicts,
		policy.ValidateDraft(model.LivePolicyLayerL3, nil, input.Overrides, l1)...,
	)
	item, err := s.store.CreateLivePolicyDraft(r.Context(), actor.UserID, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存 L3 策略草稿失败")
		return
	}
	safeItem := customerSafeLivePolicyVersion(item)
	writeJSON(w, http.StatusCreated, safeItem)
}

func (s *Server) liveRoomPolicyPublish(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, roomID, ok := s.requireCustomerPolicyRoom(w, r)
	if !ok {
		return
	}
	versionID, ok := namedPathID(w, r, "versionID", "策略版本")
	if !ok {
		return
	}
	scope, err := s.store.GetLivePolicyScope(r.Context(), model.LivePolicyLayerL3, "", tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusNotFound, "直播间还没有 L3 策略")
		return
	}
	version, err := s.store.GetLivePolicyVersion(r.Context(), versionID)
	if err != nil || version.PolicyID != scope.ID {
		writeError(w, http.StatusNotFound, "策略版本不属于当前直播间")
		return
	}
	l1, _ := s.store.GetActiveLivePolicyVersion(r.Context(), model.LivePolicyLayerL1, "", 0, 0)
	conflicts := append([]model.LivePolicyConflict{}, version.Conflicts...)
	conflicts = append(conflicts, policy.ValidateDraft(model.LivePolicyLayerL3, nil, version.Overrides, l1)...)
	if len(conflicts) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":     "该调整与平台策略边界冲突，不能发布",
			"conflicts": customerSafePolicyConflicts(conflicts),
		})
		return
	}
	item, err := s.store.PublishLivePolicyVersion(r.Context(), versionID, actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "发布 L3 策略失败")
		return
	}
	s.refreshRunningPolicySnapshots(r.Context())
	safeItem := customerSafeLivePolicyVersion(item)
	writeJSON(w, http.StatusOK, safeItem)
}

func (s *Server) liveRoomPolicyRollback(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, roomID, ok := s.requireCustomerPolicyRoom(w, r)
	if !ok {
		return
	}
	versionID, ok := namedPathID(w, r, "versionID", "策略版本")
	if !ok {
		return
	}
	scope, err := s.store.GetLivePolicyScope(r.Context(), model.LivePolicyLayerL3, "", tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusNotFound, "直播间还没有 L3 策略")
		return
	}
	version, err := s.store.GetLivePolicyVersion(r.Context(), versionID)
	if err != nil || version.PolicyID != scope.ID {
		writeError(w, http.StatusNotFound, "策略版本不属于当前直播间")
		return
	}
	l1, _ := s.store.GetActiveLivePolicyVersion(r.Context(), model.LivePolicyLayerL1, "", 0, 0)
	conflicts := policy.ValidateDraft(model.LivePolicyLayerL3, nil, version.Overrides, l1)
	if len(conflicts) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":     "历史直播间策略与平台策略边界冲突，不能直接回滚",
			"conflicts": customerSafePolicyConflicts(conflicts),
		})
		return
	}
	item, err := s.store.RollbackLivePolicyVersion(r.Context(), versionID, actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "回滚 L3 策略失败")
		return
	}
	s.refreshRunningPolicySnapshots(r.Context())
	safeItem := customerSafeLivePolicyVersion(item)
	writeJSON(w, http.StatusOK, safeItem)
}

func (s *Server) liveRoomEffectivePolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	if _, err := s.getCoreRoomState(r.Context(), tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "直播间不存在")
		return
	}
	industryCode, l1, l2, l3, err := s.store.LoadLivePolicyLayers(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取有效策略失败")
		return
	}
	effective := policy.BuildEffective(industryCode, l1, l2, l3)
	if actor.Role == "customer" {
		writeJSON(w, http.StatusOK, customerSafeEffectivePolicy(effective))
		return
	}
	writeJSON(w, http.StatusOK, effective)
}

func policyVersionID(version *model.LivePolicyVersion) *int64 {
	if version == nil {
		return nil
	}
	value := version.ID
	return &value
}
