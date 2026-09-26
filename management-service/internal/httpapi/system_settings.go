package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type updateSystemSettingsRequest struct {
	Settings []model.SystemSettingUpdate `json:"settings"`
}

type updateMembershipRoomLimitsRequest struct {
	Items []model.MembershipRoomLimitUpdate `json:"items"`
}

type updateAgentPromptConfigsRequest struct {
	Items []model.AgentPromptConfigUpdate `json:"items"`
}

type rollbackAgentPromptRequest struct {
	Version uint64 `json:"version"`
}

func (s *Server) systemPublicConfig(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.PublicSystemConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取系统全局配置失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) systemSettingsDashboard(w http.ResponseWriter, r *http.Request) {
	actor, access, ok := s.requireStaffPermission(w, r, "system.settings.view")
	if !ok {
		return
	}
	item, err := s.store.SystemSettingsDashboard(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取系统设定失败")
		return
	}

	// 系统设定是统一入口，但返回内容必须跟随角色权限收敛。
	if !actor.IsPlatformAdmin() && !access.IsSuperAdmin {
		item.Settings = []model.SystemSetting{}
		item.AgentPromptConfigs = []model.AgentPromptConfig{}
		if !staffHasPermission(access, "commercial.membership.view") {
			item.MembershipRoomLimits = []model.MembershipRoomLimitSetting{}
		}
		if !staffHasPermission(access, "inventory.view") {
			item.Dictionaries = map[string][]model.SystemDictionaryItem{}
			item.Warehouses = []model.Warehouse{}
		}
	}
	writeJSON(w, http.StatusOK, item)
}

func validateLivePolicyFontSetting(key, raw string) error {
	minValue, maxValue := 0, 0
	switch key {
	case "live_policy_rule_title_font_size":
		minValue, maxValue = 16, 40
	case "live_policy_rule_body_font_size":
		minValue, maxValue = 14, 36
	case "live_policy_rule_meta_font_size":
		minValue, maxValue = 12, 28
	case "live_policy_test_title_font_size":
		minValue, maxValue = 18, 32
	case "live_policy_test_body_font_size":
		minValue, maxValue = 16, 28
	case "live_policy_test_meta_font_size":
		minValue, maxValue = 14, 24
	default:
		return nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < minValue || value > maxValue {
		return fmt.Errorf("%s 必须是 %d 到 %d 的整数", key, minValue, maxValue)
	}
	return nil
}

func (s *Server) systemUpdateSettings(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requirePlatformAdmin(w, r)
	if !ok {
		return
	}
	var input updateSystemSettingsRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if len(input.Settings) == 0 {
		writeError(w, http.StatusBadRequest, "没有需要保存的系统设置")
		return
	}
	for _, item := range input.Settings {
		if err := db.ValidateFinanceReviewSetting(item.Key, item.Value); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if utf8.RuneCountInString(item.Value) > 4096 {
			writeError(w, http.StatusBadRequest, "单项系统设置内容过长")
			return
		}
		if err := validateLivePolicyFontSetting(item.Key, item.Value); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if item.Key == "internal_agent_name" || item.Key == "client_agent_name" {
			name := strings.TrimSpace(item.Value)
			if name == "" {
				writeError(w, http.StatusBadRequest, "智能体名称不能为空")
				return
			}
			if utf8.RuneCountInString(name) > 32 {
				writeError(w, http.StatusBadRequest, "智能体名称不能超过 32 个字符")
				return
			}
		}
	}
	if err := s.store.UpdateSystemSettings(r.Context(), input.Settings, actor.UserID); err != nil {
		writeError(w, http.StatusBadRequest, "保存系统设置失败")
		return
	}
	item, err := s.store.SystemSettingsDashboard(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "系统设置已保存，但重新读取失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) requireAgentPromptAdmin(w http.ResponseWriter, r *http.Request) (model.Actor, bool) {
	actor, access, ok := s.requireStaffPermission(w, r, "system.settings.view")
	if !ok {
		return model.Actor{}, false
	}
	if !actor.IsPlatformAdmin() && !access.IsSuperAdmin {
		writeError(w, http.StatusForbidden, "仅超级系统管理员可维护模型与智能体配置")
		return model.Actor{}, false
	}
	return actor, true
}

func (s *Server) systemUpdateAgentPromptConfigs(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgentPromptAdmin(w, r)
	if !ok {
		return
	}
	var input updateAgentPromptConfigsRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "模型指令配置格式错误")
		return
	}
	if len(input.Items) == 0 {
		writeError(w, http.StatusBadRequest, "没有需要保存的模型指令配置")
		return
	}
	for _, item := range input.Items {
		if strings.TrimSpace(item.Key) == "" {
			writeError(w, http.StatusBadRequest, "模型指令 Key 不能为空")
			return
		}
		if utf8.RuneCountInString(item.CurrentValue) > 20000 {
			writeError(w, http.StatusBadRequest, "单项模型指令内容不能超过 20000 字")
			return
		}
	}
	if err := s.store.UpdateAgentPromptConfigs(r.Context(), input.Items, actor.UserID); err != nil {
		writeError(w, http.StatusBadRequest, "保存模型指令草稿失败")
		return
	}
	items, err := s.store.ListAgentPromptConfigs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "草稿已保存，但重新读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) systemPublishAgentPromptConfig(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgentPromptAdmin(w, r)
	if !ok {
		return
	}
	key := strings.TrimSpace(r.PathValue("key"))
	if key == "" {
		writeError(w, http.StatusBadRequest, "模型指令 Key 不能为空")
		return
	}
	if err := s.store.PublishAgentPromptConfig(r.Context(), key, actor.UserID, "publish"); err != nil {
		writeError(w, http.StatusBadRequest, "发布模型指令失败")
		return
	}
	items, err := s.store.ListAgentPromptConfigs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "模型指令已发布，但重新读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) systemAgentPromptHistory(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAgentPromptAdmin(w, r); !ok {
		return
	}
	key := strings.TrimSpace(r.PathValue("key"))
	if key == "" {
		writeError(w, http.StatusBadRequest, "模型指令 Key 不能为空")
		return
	}
	items, err := s.store.ListAgentPromptHistory(r.Context(), key, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模型指令版本历史失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) systemRollbackAgentPromptConfig(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgentPromptAdmin(w, r)
	if !ok {
		return
	}
	key := strings.TrimSpace(r.PathValue("key"))
	var input rollbackAgentPromptRequest
	if err := readJSON(w, r, &input); err != nil || input.Version == 0 {
		writeError(w, http.StatusBadRequest, "请选择需要回滚的版本")
		return
	}
	if err := s.store.RollbackAgentPromptConfig(r.Context(), key, input.Version, actor.UserID); err != nil {
		writeError(w, http.StatusBadRequest, "回滚模型指令失败")
		return
	}
	items, err := s.store.ListAgentPromptConfigs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "模型指令已回滚，但重新读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) systemResetAgentPromptConfig(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgentPromptAdmin(w, r)
	if !ok {
		return
	}
	key := strings.TrimSpace(r.PathValue("key"))
	if key == "" {
		writeError(w, http.StatusBadRequest, "模型指令 Key 不能为空")
		return
	}
	if err := s.store.ResetAgentPromptConfig(r.Context(), key, actor.UserID); err != nil {
		writeError(w, http.StatusBadRequest, "恢复默认模型指令草稿失败")
		return
	}
	items, err := s.store.ListAgentPromptConfigs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "默认内容已恢复到草稿，但重新读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) systemUpdateMembershipRoomLimits(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage")
	if !ok {
		return
	}
	var input updateMembershipRoomLimitsRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if len(input.Items) == 0 {
		writeError(w, http.StatusBadRequest, "没有需要保存的会员直播间上限")
		return
	}
	for _, item := range input.Items {
		if item.PlanID <= 0 || item.RoomLimit < 1 || item.RoomLimit > 10 {
			writeError(w, http.StatusBadRequest, "会员直播间上限必须是 1 到 10 的整数")
			return
		}
	}
	if err := s.store.UpdateMembershipRoomLimits(r.Context(), input.Items, actor.UserID); err != nil {
		writeError(w, http.StatusBadRequest, "保存会员直播间上限失败")
		return
	}
	item, err := s.store.SystemSettingsDashboard(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "会员直播间上限已保存，但重新读取失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) systemDictionaryList(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	category := strings.TrimSpace(r.PathValue("category"))
	if category == "" {
		writeError(w, http.StatusBadRequest, "字典类别不能为空")
		return
	}
	includeDisabled := r.URL.Query().Get("include_disabled") == "1"
	if includeDisabled && !actor.IsPlatformAdmin() {
		writeError(w, http.StatusForbidden, "仅超级系统管理员可读取停用字典项")
		return
	}
	items, err := s.store.ListSystemDictionaryItems(r.Context(), category, includeDisabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取系统字典失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) systemDictionaryCreate(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "system.settings.inventory.manage"); !ok {
		return
	}
	var input model.SystemDictionaryItemInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if !validSystemDictionaryInput(input) {
		writeError(w, http.StatusBadRequest, "字典类别、编码和显示名称不能为空")
		return
	}
	item, err := s.store.CreateSystemDictionaryItem(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "创建系统字典项失败，编码可能已存在")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) systemDictionaryUpdate(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "system.settings.inventory.manage"); !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("itemID"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "字典项 ID 无效")
		return
	}
	var input model.SystemDictionaryItemInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if !validSystemDictionaryInput(input) {
		writeError(w, http.StatusBadRequest, "字典类别、编码和显示名称不能为空")
		return
	}
	item, err := s.store.UpdateSystemDictionaryItem(r.Context(), id, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "系统字典项不存在")
			return
		}
		writeError(w, http.StatusBadRequest, "更新系统字典项失败，编码可能已存在")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) systemWarehouseCreate(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "system.settings.inventory.manage"); !ok {
		return
	}
	var input model.WarehouseInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input = normalizeWarehouseInput(input)
	if !validWarehouseInput(input) {
		writeError(w, http.StatusBadRequest, "仓库编码、名称或状态无效")
		return
	}
	if isProtectedWarehouseCode(input.Code) {
		writeError(w, http.StatusBadRequest, "该编码为系统仓库保留编码")
		return
	}
	item, err := s.store.CreateWarehouse(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "新增仓库失败，仓库编码可能已存在")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) systemWarehouseUpdate(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "system.settings.inventory.manage"); !ok {
		return
	}
	warehouseID, err := strconv.ParseInt(r.PathValue("warehouseID"), 10, 64)
	if err != nil || warehouseID <= 0 {
		writeError(w, http.StatusBadRequest, "仓库 ID 无效")
		return
	}
	existing, err := s.store.GetWarehouse(r.Context(), warehouseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "仓库不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取仓库失败")
		return
	}
	var input model.WarehouseInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input = normalizeWarehouseInput(input)
	if isProtectedWarehouseCode(existing.Code) {
		input.Code = existing.Code
		input.Status = "active"
	}
	if !validWarehouseInput(input) {
		writeError(w, http.StatusBadRequest, "仓库编码、名称或状态无效")
		return
	}
	if isProtectedWarehouseCode(input.Code) && !isProtectedWarehouseCode(existing.Code) {
		writeError(w, http.StatusBadRequest, "该编码为系统仓库保留编码")
		return
	}
	item, err := s.store.UpdateWarehouse(r.Context(), warehouseID, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "仓库不存在")
			return
		}
		writeError(w, http.StatusBadRequest, "保存仓库失败，仓库编码可能已存在")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func normalizeWarehouseInput(input model.WarehouseInput) model.WarehouseInput {
	input.Code = strings.ToUpper(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "active"
	}
	return input
}

func validWarehouseInput(input model.WarehouseInput) bool {
	if utf8.RuneCountInString(input.Code) < 2 || utf8.RuneCountInString(input.Code) > 64 ||
		utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 128 {
		return false
	}
	if input.Status != "active" && input.Status != "inactive" {
		return false
	}
	for _, r := range input.Code {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func isProtectedWarehouseCode(code string) bool {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "HQ_MAIN", "AFTER_SALES_PENDING", "REPAIR", "SCRAP_HOLD":
		return true
	default:
		return false
	}
}

func validSystemDictionaryInput(input model.SystemDictionaryItemInput) bool {
	category := strings.TrimSpace(input.Category)
	code := strings.TrimSpace(input.Code)
	label := strings.TrimSpace(input.Label)
	if category == "" || code == "" || label == "" {
		return false
	}
	return utf8.RuneCountInString(category) <= 64 &&
		utf8.RuneCountInString(code) <= 96 &&
		utf8.RuneCountInString(label) <= 160 &&
		utf8.RuneCountInString(input.Description) <= 512
}
