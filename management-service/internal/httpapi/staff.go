package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/model"
)

func normalizeStaffEmployeeNo(raw string) (string, bool) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if strings.HasPrefix(value, "EMP-") {
		value = strings.TrimPrefix(value, "EMP-")
	}
	if value == "" || len(value) > 6 {
		return "", false
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 || n > 999999 {
		return "", false
	}
	return fmt.Sprintf("EMP-%06d", n), true
}

type createStaffGroupRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type updateStaffGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type createStaffRoleRequest struct {
	GroupID          int64   `json:"group_id"`
	Code             string  `json:"code"`
	Name             string  `json:"name"`
	Description      string  `json:"description"`
	IsGroupManager   bool    `json:"is_group_manager"`
	DefaultScopeType string  `json:"default_scope_type"`
	PermissionIDs    []int64 `json:"permission_ids"`
}

type updateStaffRoleRequest struct {
	Name             string  `json:"name"`
	Description      string  `json:"description"`
	IsGroupManager   bool    `json:"is_group_manager"`
	DefaultScopeType string  `json:"default_scope_type"`
	Status           string  `json:"status"`
	PermissionIDs    []int64 `json:"permission_ids"`
}

type createStaffEmployeeRequest struct {
	EmployeeNo     string  `json:"employee_no"`
	PrimaryGroupID int64   `json:"primary_group_id"`
	RoleIDs        []int64 `json:"role_ids"`
	Username       string  `json:"username"`
	DisplayName    string  `json:"display_name"`
	Phone          string  `json:"phone"`
	Email          string  `json:"email"`
	Province       string  `json:"province"`
	City           string  `json:"city"`
	District       string  `json:"district"`
	DeliveryMethod string  `json:"delivery_method"`
}

type replaceStaffEmployeeRolesRequest struct {
	RoleIDs []int64 `json:"role_ids"`
}

type updateApprovalPolicyRequest struct {
	Mode             string  `json:"mode"`
	ThresholdAmount  float64 `json:"threshold_amount"`
	ApproverRoleCode string  `json:"approver_role_code"`
	Status           string  `json:"status"`
}

func (s *Server) staffAccessForActor(
	r *http.Request,
	actor model.Actor,
) (model.StaffAccessContext, error) {
	if actor.IsPlatformAdmin() {
		return model.StaffAccessContext{
			IsSuperAdmin:       true,
			RoleCodes:          []string{"super_system_admin"},
			Permissions:        []string{"*"},
			PermissionScopes:   map[string]string{"*": "all"},
			PermissionGroupIDs: map[string][]int64{},
			GroupIDs:           []int64{},
			ManagedGroupIDs:    []int64{},
		}, nil
	}
	return s.store.GetStaffAccess(r.Context(), actor.UserID)
}

func staffHasPermission(
	access model.StaffAccessContext,
	permission string,
) bool {
	if access.IsSuperAdmin {
		return true
	}
	for _, code := range access.Permissions {
		if code == permission || code == "*" {
			return true
		}
	}
	return false
}

func staffPermissionScope(
	access model.StaffAccessContext,
	permission string,
) string {
	if access.IsSuperAdmin {
		return "all"
	}
	if value := access.PermissionScopes[permission]; value != "" {
		return value
	}
	return ""
}

func staffPermissionGroupIDs(
	access model.StaffAccessContext,
	permission string,
) []int64 {
	if access.IsSuperAdmin {
		return nil
	}
	return access.PermissionGroupIDs[permission]
}

func staffEmployeeBelongsToGroup(item model.StaffEmployeeSummary, groupID int64) bool {
	if groupID <= 0 {
		return false
	}
	if item.PrimaryGroupID == groupID {
		return true
	}
	for _, group := range item.Groups {
		if group.GroupID == groupID {
			return true
		}
	}
	return false
}

func (s *Server) requireStaffPermission(
	w http.ResponseWriter,
	r *http.Request,
	permission string,
) (model.Actor, model.StaffAccessContext, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, model.StaffAccessContext{}, false
	}
	if !actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "仅内部员工可执行此操作")
		return model.Actor{}, model.StaffAccessContext{}, false
	}
	access, err := s.staffAccessForActor(r, actor)
	if err != nil {
		writeError(w, http.StatusForbidden, "当前员工没有组织架构权限")
		return model.Actor{}, model.StaffAccessContext{}, false
	}
	if !staffHasPermission(access, permission) {
		writeError(w, http.StatusForbidden, "当前角色没有此操作权限")
		return model.Actor{}, model.StaffAccessContext{}, false
	}
	return actor, access, true
}

func (s *Server) requireAnyStaffPermission(
	w http.ResponseWriter,
	r *http.Request,
	permissions ...string,
) (model.Actor, model.StaffAccessContext, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, model.StaffAccessContext{}, false
	}
	if !actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "仅内部员工可执行此操作")
		return model.Actor{}, model.StaffAccessContext{}, false
	}
	access, err := s.staffAccessForActor(r, actor)
	if err != nil {
		writeError(w, http.StatusForbidden, "当前员工没有组织架构权限")
		return model.Actor{}, model.StaffAccessContext{}, false
	}
	for _, permission := range permissions {
		if staffHasPermission(access, permission) {
			return actor, access, true
		}
	}
	writeError(w, http.StatusForbidden, "当前角色没有此操作权限")
	return model.Actor{}, model.StaffAccessContext{}, false
}
func canManageStaffGroup(
	access model.StaffAccessContext,
	permission string,
	groupID int64,
) bool {
	if access.IsSuperAdmin {
		return true
	}
	scope := staffPermissionScope(access, permission)
	switch scope {
	case "all", "all_internal":
		return true
	case "managed_groups":
		for _, id := range access.ManagedGroupIDs {
			if id == groupID {
				return true
			}
		}
		return false
	case "group":
		for _, id := range staffPermissionGroupIDs(access, permission) {
			if id == groupID {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func canViewStaffEmployee(
	access model.StaffAccessContext,
	item model.StaffEmployeeSummary,
) bool {
	if access.IsSuperAdmin {
		return true
	}
	scope := staffPermissionScope(access, "staff.employee.view")
	switch scope {
	case "all", "all_internal":
		return true
	case "managed_groups":
		for _, id := range access.ManagedGroupIDs {
			if staffEmployeeBelongsToGroup(item, id) {
				return true
			}
		}
		return false
	case "group":
		for _, id := range staffPermissionGroupIDs(access, "staff.employee.view") {
			if staffEmployeeBelongsToGroup(item, id) {
				return true
			}
		}
		return false
	case "self":
		return item.ID == access.EmployeeID
	default:
		return false
	}
}

func (s *Server) staffDashboard(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "仅内部员工可查看组织架构")
		return
	}
	access, err := s.staffAccessForActor(r, actor)
	if err != nil {
		writeError(w, http.StatusForbidden, "当前账号未加入内部员工体系")
		return
	}

	allGroups, err := s.store.ListStaffGroups(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取部门失败")
		return
	}

	visibleGroupIDs := map[int64]struct{}{}
	groupScope := staffPermissionScope(access, "staff.group.view")
	switch {
	case access.IsSuperAdmin ||
		groupScope == "all" ||
		groupScope == "all_internal":
		for _, group := range allGroups {
			visibleGroupIDs[group.ID] = struct{}{}
		}
	case groupScope == "managed_groups":
		for _, groupID := range access.ManagedGroupIDs {
			visibleGroupIDs[groupID] = struct{}{}
		}
	case groupScope == "group":
		for _, groupID := range staffPermissionGroupIDs(access, "staff.group.view") {
			visibleGroupIDs[groupID] = struct{}{}
		}
	default:
		for _, groupID := range access.GroupIDs {
			visibleGroupIDs[groupID] = struct{}{}
		}
	}

	groups := make([]model.StaffGroupSummary, 0)
	for _, group := range allGroups {
		if _, visible := visibleGroupIDs[group.ID]; visible {
			groups = append(groups, group)
		}
	}

	allRoles, err := s.store.ListStaffRoles(r.Context(), 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取角色失败")
		return
	}
	roles := make([]model.StaffRoleSummary, 0)
	if access.IsSuperAdmin || staffHasPermission(access, "staff.role.view") {
		for _, role := range allRoles {
			if _, visible := visibleGroupIDs[role.GroupID]; visible {
				roles = append(roles, role)
			}
		}
	}

	permissions := []model.StaffPermissionSummary{}
	if access.IsSuperAdmin || staffHasPermission(access, "staff.permission.view") {
		permissions, err = s.store.ListStaffPermissions(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取权限失败")
			return
		}
	}

	allEmployees, err := s.store.ListStaffEmployees(r.Context(), 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取员工失败")
		return
	}
	filteredEmployees := make([]model.StaffEmployeeSummary, 0)
	for _, item := range allEmployees {
		if access.IsSuperAdmin ||
			(staffHasPermission(access, "staff.employee.view") &&
				canViewStaffEmployee(access, item)) {
			filteredEmployees = append(filteredEmployees, item)
		}
	}

	policies := []model.StaffApprovalPolicySummary{}
	if access.IsSuperAdmin ||
		staffHasPermission(access, "finance.dashboard.view") {
		policies, err = s.store.ListStaffApprovalPolicies(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取审批策略失败")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access":            access,
		"groups":            groups,
		"roles":             roles,
		"permissions":       permissions,
		"employees":         filteredEmployees,
		"approval_policies": policies,
	})
}
func (s *Server) staffCreateGroup(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsPlatformAdmin() {
		writeError(w, http.StatusForbidden, "仅超级系统管理员可新增部门")
		return
	}
	var input createStaffGroupRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.CreateStaffGroup(
		r.Context(),
		input.Code,
		input.Name,
		input.Description,
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate:") {
			writeError(w, http.StatusConflict, "部门编码已存在")
			return
		}
		writeError(w, http.StatusBadRequest, "创建部门失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) staffUpdateGroup(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsPlatformAdmin() {
		writeError(w, http.StatusForbidden, "仅超级系统管理员可修改部门")
		return
	}
	groupID, ok := staffPathID(w, r, "groupID")
	if !ok {
		return
	}
	var input updateStaffGroupRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if input.Status == "" {
		input.Status = "active"
	}
	if err := s.store.UpdateStaffGroup(
		r.Context(),
		groupID,
		input.Name,
		input.Description,
		input.Status,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "部门不存在")
			return
		}
		if strings.Contains(err.Error(), "system managed staff group is locked") {
			writeError(w, http.StatusConflict, "系统内置部门不可修改")
			return
		}
		writeError(w, http.StatusBadRequest, "修改部门失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) staffCreateRole(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsPlatformAdmin() {
		writeError(w, http.StatusForbidden, "仅超级系统管理员可新增角色")
		return
	}
	var input createStaffRoleRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.CreateStaffRole(
		r.Context(),
		input.GroupID,
		input.Code,
		input.Name,
		input.Description,
		input.IsGroupManager,
		input.DefaultScopeType,
		input.PermissionIDs,
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate:") {
			writeError(w, http.StatusConflict, "角色编码已存在")
			return
		}
		writeError(w, http.StatusBadRequest, "创建角色失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) staffUpdateRole(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsPlatformAdmin() {
		writeError(w, http.StatusForbidden, "仅超级系统管理员可修改角色权限")
		return
	}
	roleID, ok := staffPathID(w, r, "roleID")
	if !ok {
		return
	}
	var input updateStaffRoleRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if input.Status == "" {
		input.Status = "active"
	}
	if err := s.store.UpdateStaffRole(
		r.Context(),
		roleID,
		input.Name,
		input.Description,
		input.IsGroupManager,
		input.DefaultScopeType,
		input.Status,
		input.PermissionIDs,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "角色不存在")
			return
		}
		writeError(w, http.StatusBadRequest, "修改角色失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) staffCreateEmployee(w http.ResponseWriter, r *http.Request) {
	_, access, ok := s.requireStaffPermission(
		w,
		r,
		"staff.employee.create",
	)
	if !ok {
		return
	}

	var input createStaffEmployeeRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	employeeNo, validEmployeeNo := normalizeStaffEmployeeNo(input.EmployeeNo)
	if !validEmployeeNo {
		writeError(w, http.StatusBadRequest, "员工编号只填写 1-6 位数字")
		return
	}
	input.EmployeeNo = employeeNo
	input.DeliveryMethod = normalizeDeliveryMethod(input.DeliveryMethod)

	email, emailOK := normalizeCredentialEmail(input.Email)
	if !emailOK {
		writeError(w, http.StatusBadRequest, "邮箱格式不正确")
		return
	}
	input.Email = email
	if input.DeliveryMethod == "email" && input.Email == "" {
		writeError(w, http.StatusBadRequest, "选择邮件发送时必须填写邮箱")
		return
	}
	if !canManageStaffGroup(
		access,
		"staff.employee.create",
		input.PrimaryGroupID,
	) {
		writeError(w, http.StatusForbidden, "当前角色不能向该部门添加员工")
		return
	}

	allowManagerRole := access.IsSuperAdmin ||
		staffPermissionScope(access, "staff.employee.role_assign") == "all_internal"
	allRoles, err := s.store.ListStaffRoles(r.Context(), 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取角色失败")
		return
	}
	roleByID := make(map[int64]model.StaffRoleSummary, len(allRoles))
	for _, role := range allRoles {
		roleByID[role.ID] = role
	}
	for _, roleID := range input.RoleIDs {
		role, exists := roleByID[roleID]
		if !exists || role.Status != "active" {
			writeError(w, http.StatusBadRequest, "选择的岗位不存在或已停用")
			return
		}
		if role.GroupID != input.PrimaryGroupID &&
			!canManageStaffGroup(access, "staff.employee.role_assign", role.GroupID) {
			writeError(w, http.StatusForbidden, "当前角色不能给员工增加该部门的兼任职责")
			return
		}
		if role.IsGroupManager && !allowManagerRole {
			writeError(w, http.StatusForbidden, "部门负责人角色只能由超级系统管理员或具备全局员工管理权限的人员分配")
			return
		}
	}

	initialPassword, err := auth.GenerateInitialPassword()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成初始密码失败")
		return
	}
	passwordHash, err := auth.HashPassword(initialPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "初始密码加密失败")
		return
	}

	item, err := s.store.CreateStaffEmployee(
		r.Context(),
		input.EmployeeNo,
		input.PrimaryGroupID,
		input.RoleIDs,
		input.Username,
		input.DisplayName,
		input.Phone,
		input.Email,
		input.Province,
		input.City,
		input.District,
		passwordHash,
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate:") {
			writeError(w, http.StatusConflict, "员工编号或登录账号已存在")
			return
		}
		writeError(w, http.StatusBadRequest, "创建员工失败")
		return
	}

	credential := s.deliverInitialCredential(
		input.DeliveryMethod,
		input.Email,
		input.DisplayName,
		input.Username,
		initialPassword,
		"internal",
	)
	writeJSON(w, http.StatusCreated, map[string]any{
		"item":       item,
		"credential": credential,
	})
}

func (s *Server) staffDisableEmployee(w http.ResponseWriter, r *http.Request) {
	actor, access, ok := s.requireStaffPermission(
		w,
		r,
		"staff.employee.disable",
	)
	if !ok {
		return
	}
	employeeID, ok := staffPathID(w, r, "employeeID")
	if !ok {
		return
	}
	item, err := s.store.GetStaffEmployee(r.Context(), employeeID)
	if err != nil {
		writeError(w, http.StatusNotFound, "员工不存在")
		return
	}
	if item.UserID == actor.UserID {
		writeError(w, http.StatusConflict, "不能停用当前登录账号")
		return
	}
	if !canManageStaffGroup(
		access,
		"staff.employee.disable",
		item.PrimaryGroupID,
	) {
		writeError(w, http.StatusForbidden, "当前角色不能停用该员工")
		return
	}
	if err := s.store.DisableStaffEmployee(r.Context(), employeeID); err != nil {
		writeError(w, http.StatusInternalServerError, "停用员工失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) staffReplaceEmployeeRoles(w http.ResponseWriter, r *http.Request) {
	_, access, ok := s.requireStaffPermission(
		w,
		r,
		"staff.employee.role_assign",
	)
	if !ok {
		return
	}
	employeeID, ok := staffPathID(w, r, "employeeID")
	if !ok {
		return
	}
	item, err := s.store.GetStaffEmployee(r.Context(), employeeID)
	if err != nil {
		writeError(w, http.StatusNotFound, "员工不存在")
		return
	}
	var input replaceStaffEmployeeRolesRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	allRoles, err := s.store.ListStaffRoles(r.Context(), 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取角色失败")
		return
	}
	roleByID := make(map[int64]model.StaffRoleSummary, len(allRoles))
	for _, role := range allRoles {
		roleByID[role.ID] = role
	}
	currentRoleIDs := make(map[int64]struct{}, len(item.Roles))
	requestedRoleIDs := make(map[int64]struct{}, len(input.RoleIDs))
	for _, role := range item.Roles {
		currentRoleIDs[role.RoleID] = struct{}{}
	}
	for _, roleID := range input.RoleIDs {
		requestedRoleIDs[roleID] = struct{}{}
	}

	allowManagerRoles := access.IsSuperAdmin ||
		staffPermissionScope(access, "staff.employee.role_assign") == "all_internal"

	// Responsibilities from departments the operator cannot manage are locked:
	// they must remain on the employee and cannot be added by this operator.
	for _, currentRole := range item.Roles {
		if canManageStaffGroup(access, "staff.employee.role_assign", currentRole.GroupID) {
			continue
		}
		if _, retained := requestedRoleIDs[currentRole.RoleID]; !retained {
			writeError(w, http.StatusForbidden, "该员工还有其他部门职责，当前账号不能移除")
			return
		}
	}
	for _, roleID := range input.RoleIDs {
		role, exists := roleByID[roleID]
		if !exists || role.Status != "active" {
			writeError(w, http.StatusBadRequest, "选择的岗位不存在或已停用")
			return
		}
		_, alreadyAssigned := currentRoleIDs[roleID]
		if !alreadyAssigned &&
			!canManageStaffGroup(access, "staff.employee.role_assign", role.GroupID) {
			writeError(w, http.StatusForbidden, "当前账号不能增加该部门的兼任职责")
			return
		}
		if role.IsGroupManager && !allowManagerRoles && !alreadyAssigned {
			writeError(w, http.StatusForbidden, "部门负责人角色只能由超级系统管理员或具备全局员工管理权限的人员分配")
			return
		}
	}
	if !allowManagerRoles {
		for _, currentRole := range item.Roles {
			if !currentRole.IsGroupManager {
				continue
			}
			if _, retained := requestedRoleIDs[currentRole.RoleID]; !retained {
				writeError(w, http.StatusForbidden, "当前账号不能移除部门负责人职责")
				return
			}
		}
	}

	if err := s.store.ReplaceStaffEmployeeRoles(
		r.Context(),
		employeeID,
		input.RoleIDs,
		true,
	); err != nil {
		message := "调整员工部门职责失败"
		if strings.Contains(err.Error(), "primary department") {
			message = "主部门必须至少保留一个岗位"
		}
		writeError(w, http.StatusBadRequest, message)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) staffUpdateApprovalPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsPlatformAdmin() {
		writeError(w, http.StatusForbidden, "仅超级系统管理员可修改审批规则")
		return
	}
	policyID, ok := staffPathID(w, r, "policyID")
	if !ok {
		return
	}
	var input updateApprovalPolicyRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := s.store.UpdateStaffApprovalPolicy(
		r.Context(),
		policyID,
		input.Mode,
		input.ThresholdAmount,
		input.ApproverRoleCode,
		input.Status,
	); err != nil {
		writeError(w, http.StatusBadRequest, "修改审批规则失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func staffPathID(
	w http.ResponseWriter,
	r *http.Request,
	name string,
) (int64, bool) {
	value, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "ID 无效")
		return 0, false
	}
	return value, true
}

func containsRoleID(values []int64, value int64) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
