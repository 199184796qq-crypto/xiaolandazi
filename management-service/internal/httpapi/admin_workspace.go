package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

var errAdminWorkspaceForbidden = errors.New("当前账号没有营销运维部工作台的访问权限")

type adminWorkspaceSession struct {
	Name               string `json:"name"`
	AvatarURL          string `json:"avatar_url"`
	RoleTitle          string `json:"role_title"`
	DepartmentName     string `json:"department_name,omitempty"`
	DepartmentCode     string `json:"department_code,omitempty"`
	MustChangePassword bool   `json:"must_change_password"`
}

type adminAccountPosition struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type adminAccountDepartment struct {
	ID        int64                  `json:"id"`
	Name      string                 `json:"name"`
	IsPrimary bool                   `json:"is_primary"`
	Positions []adminAccountPosition `json:"positions"`
}

func adminAccountDepartments(employee model.StaffEmployeeSummary) []adminAccountDepartment {
	positions := make(map[int64][]adminAccountPosition)
	for _, role := range employee.Roles {
		positions[role.GroupID] = append(positions[role.GroupID], adminAccountPosition{ID: role.RoleID, Name: role.Name})
	}
	departments := make([]adminAccountDepartment, 0, len(employee.Groups))
	for _, group := range employee.Groups {
		groupPositions := positions[group.GroupID]
		if groupPositions == nil {
			groupPositions = []adminAccountPosition{}
		}
		departments = append(departments, adminAccountDepartment{
			ID: group.GroupID, Name: group.GroupName, IsPrimary: group.IsPrimary,
			Positions: groupPositions,
		})
	}
	return departments
}

func adminWorkspaceRoleTitle(actor model.Actor, access model.StaffAccessContext) string {
	switch {
	case actor.IsPlatformAdmin():
		return "超级系统管理员"
	case !actor.IsInternalStaff():
		return ""
	case slices.Contains(access.RoleCodes, "live_operations_manager"):
		return "营销运维负责人"
	case slices.Contains(access.RoleCodes, "live_operations_staff"):
		return "营销运维专员"
	default:
		return ""
	}
}

func (s *Server) adminWorkspaceSession(r *http.Request, actor model.Actor) (adminWorkspaceSession, error) {
	if !actor.IsInternalStaff() {
		return adminWorkspaceSession{}, errAdminWorkspaceForbidden
	}
	access, err := s.staffAccessForActor(r, actor)
	if errors.Is(err, sql.ErrNoRows) {
		return adminWorkspaceSession{}, errAdminWorkspaceForbidden
	}
	if err != nil {
		return adminWorkspaceSession{}, err
	}
	role := adminWorkspaceRoleTitle(actor, access)
	if role == "" {
		return adminWorkspaceSession{}, errAdminWorkspaceForbidden
	}
	name := actor.DisplayName
	if strings.TrimSpace(name) == "" {
		name = actor.Username
	}
	return adminWorkspaceSession{
		Name:               name,
		AvatarURL:          actor.AvatarURL,
		RoleTitle:          role,
		DepartmentName:     access.PrimaryGroupName,
		DepartmentCode:     access.PrimaryGroupCode,
		MustChangePassword: actor.MustChangePassword,
	}, nil
}

func writeAdminWorkspaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errAdminWorkspaceForbidden):
		writeError(w, http.StatusForbidden, errAdminWorkspaceForbidden.Error())
	case errors.Is(err, auth.ErrCaptchaInvalid):
		writeError(w, http.StatusBadRequest, "图形验证码错误或已过期，请重新输入")
	case errors.Is(err, auth.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "登录尝试过多，请 15 分钟后再试")
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "员工账号或密码错误")
	default:
		writeError(w, http.StatusServiceUnavailable, "暂时无法验证工作台登录，请稍后重试")
	}
}

func (s *Server) adminWorkspaceLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Captcha  string `json:"captcha"`
	}
	if err := readJSONWithLimit(w, r, &input, 4096); err != nil ||
		strings.TrimSpace(input.Username) == "" || input.Password == "" || strings.TrimSpace(input.Captcha) == "" {
		writeError(w, http.StatusBadRequest, "请填写员工账号、密码和图形验证码")
		return
	}
	var session adminWorkspaceSession
	_, err := s.auth.AuthenticateWithAccess(r.Context(), w, r, auth.LoginInput{
		Method:     auth.LoginMethodPassword,
		Identifier: input.Username,
		Credential: input.Password,
		Captcha:    input.Captcha,
		Scope:      db.AccountStaff,
	}, func(actor model.Actor) error {
		var accessErr error
		session, accessErr = s.adminWorkspaceSession(r, actor)
		if accessErr != nil {
			return accessErr
		}
		if actor.IsPlatformAdmin() {
			return s.recordPlatformLogin(r, actor)
		}
		return nil
	})
	if err != nil {
		writeAdminWorkspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) adminWorkspaceBootstrap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	session, err := s.adminWorkspaceSession(r, actor)
	if err != nil {
		writeAdminWorkspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) requireAdminWorkspaceAccount(w http.ResponseWriter, r *http.Request) (model.Actor, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return actor, false
	}
	if _, err := s.adminWorkspaceSession(r, actor); err != nil {
		writeAdminWorkspaceError(w, err)
		return actor, false
	}
	if actor.MustChangePassword {
		writeError(w, http.StatusForbidden, "请先修改初始密码")
		return actor, false
	}
	return actor, true
}

func (s *Server) adminWorkspaceAccount(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	actor, ok := s.requireAdminWorkspaceAccount(w, r)
	if !ok {
		return
	}
	profile, err := s.store.GetAccountProfile(r.Context(), actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取员工资料失败")
		return
	}
	employee, err := s.store.GetStaffEmployeeByUserID(r.Context(), actor.UserID)
	if err != nil && !(actor.IsPlatformAdmin() && errors.Is(err, sql.ErrNoRows)) {
		writeError(w, http.StatusInternalServerError, "读取员工所属部门和职务失败")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Profile     model.AccountProfile     `json:"profile"`
		Departments []adminAccountDepartment `json:"departments"`
		SystemTitle string                   `json:"system_title,omitempty"`
	}{
		Profile:     profile,
		Departments: adminAccountDepartments(employee),
		// Only a platform administrator has a position outside the department structure.
		SystemTitle: adminWorkspaceRoleTitle(actor, model.StaffAccessContext{}),
	})
}

func (s *Server) adminWorkspaceShippingRegions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if _, ok := s.requireAdminWorkspaceAccount(w, r); !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(customerShippingRegions)
}

func (s *Server) adminWorkspaceUpdateAccount(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	actor, ok := s.requireAdminWorkspaceAccount(w, r)
	if !ok {
		return
	}
	var input model.AccountProfileChanges
	if err := readJSONWithLimit(w, r, &input, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	s.updateOwnAccountProfile(w, r, actor.UserID, input)
}

func (s *Server) adminWorkspaceCheckIdentifier(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	actor, ok := s.requireAdminWorkspaceAccount(w, r)
	if !ok {
		return
	}
	var input struct {
		Field string `json:"field"`
		Value string `json:"value"`
	}
	if err := readJSONWithLimit(w, r, &input, 4096); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	var changes model.AccountProfileChanges
	var conflict string
	switch input.Field {
	case "username":
		changes.Username = &input.Value
		conflict = "该登录账号已被使用"
	case "phone":
		changes.Phone = &input.Value
		conflict = "该手机号已经绑定其他账号"
	default:
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if message := normalizeAccountProfileChanges(changes); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	used, err := s.store.StaffIdentifierInUse(r.Context(), actor.UserID, input.Field, input.Value)
	switch {
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "暂时无法检查登录信息，请稍后重试")
	case used:
		writeError(w, http.StatusConflict, conflict)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
