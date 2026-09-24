package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/model"
)

type loginRequest struct {
	Method     string `json:"method"`
	Identifier string `json:"identifier"`
	Credential string `json:"credential"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	Captcha    string `json:"captcha"`
}

type smsLoginCodeRequest struct {
	Phone string `json:"phone"`
}

type registerRequest struct {
	Username        string `json:"username"`
	DisplayName     string `json:"display_name"`
	Phone           string `json:"phone"`
	Province        string `json:"province"`
	City            string `json:"city"`
	District        string `json:"district"`
	Password        string `json:"password"`
	InviteCode      string `json:"invite_code"`
	ConfirmPassword string `json:"confirm_password"`
	Captcha         string `json:"captcha"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password"`
}

func (s *Server) authCaptcha(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.Captcha(w, r); err != nil {
		writeError(w, http.StatusInternalServerError, "生成验证码失败")
	}
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	var input loginRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	method := strings.ToLower(strings.TrimSpace(input.Method))
	if method == "" {
		method = string(auth.LoginMethodPassword)
	}
	identifier := strings.TrimSpace(input.Identifier)
	credential := input.Credential
	if method == string(auth.LoginMethodPassword) {
		if identifier == "" {
			identifier = strings.TrimSpace(input.Username)
		}
		if credential == "" {
			credential = input.Password
		}
	}

	actor, err := s.auth.Authenticate(
		r.Context(),
		w,
		r,
		auth.LoginInput{
			Method:     auth.LoginMethod(method),
			Identifier: identifier,
			Credential: credential,
			Captcha:    input.Captcha,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrCaptchaInvalid):
			writeError(w, http.StatusBadRequest, "验证码错误或已过期，请重新输入")
		case errors.Is(err, auth.ErrRateLimited):
			writeError(w, http.StatusTooManyRequests, "登录尝试过多，请 15 分钟后再试")
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "账号或密码错误")
		case errors.Is(err, auth.ErrInvalidPhone):
			writeError(w, http.StatusBadRequest, "请输入正确的手机号码")
		case errors.Is(err, auth.ErrPhoneNotRegistered):
			writeError(w, http.StatusUnauthorized, "该手机号未绑定已注册账号")
		case errors.Is(err, auth.ErrSMSCodeInvalid):
			writeError(w, http.StatusUnauthorized, "短信验证码错误或已失效")
		case errors.Is(err, auth.ErrSMSCodeExpired):
			writeError(w, http.StatusUnauthorized, "短信验证码已过期，请重新获取")
		case errors.Is(err, auth.ErrUnsupportedLoginMethod):
			writeError(w, http.StatusBadRequest, "不支持的登录方式")
		default:
			writeError(w, http.StatusInternalServerError, "登录失败")
		}
		return
	}

	if actor.IsPlatformAdmin() {
		if err := s.audit.Record(r.Context(), model.AdminAuditLog{
			ActorUserID:   actor.UserID,
			ActorUsername: actor.Username,
			Action:        "admin.login",
			HTTPMethod:    r.Method,
			Path:          r.URL.Path,
			ClientIP:      requestClientIP(r),
			Result:        "http_200",
		}); err != nil {
			_ = s.auth.Logout(r.Context(), w, r)
			writeError(
				w,
				http.StatusServiceUnavailable,
				"审计日志服务暂不可用，管理员登录已取消",
			)
			return
		}
	}

	payload, err := s.buildBootstrap(r.Context(), actor)
	if err != nil {
		_ = s.auth.Logout(r.Context(), w, r)
		writeError(w, http.StatusInternalServerError, "读取登录信息失败")
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) authSendSMSLoginCode(w http.ResponseWriter, r *http.Request) {
	var input smsLoginCodeRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	result, err := s.auth.SendSMSLoginCode(r.Context(), r, input.Phone)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidPhone):
			writeError(w, http.StatusBadRequest, "请输入正确的手机号码")
		case errors.Is(err, auth.ErrPhoneNotRegistered):
			writeError(w, http.StatusNotFound, "该手机号未绑定任何已注册账号")
		case errors.Is(err, auth.ErrSMSRateLimited):
			writeError(w, http.StatusTooManyRequests, "验证码发送过于频繁，请稍后再试")
		case errors.Is(err, auth.ErrSMSUnavailable):
			writeError(w, http.StatusServiceUnavailable, "短信服务尚未配置")
		default:
			writeError(w, http.StatusInternalServerError, "发送短信验证码失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) authRegister(w http.ResponseWriter, r *http.Request) {
	var input registerRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	if strings.TrimSpace(input.InviteCode) == "" {
		writeError(w, http.StatusBadRequest, "邀请码不能为空，系统已关闭无邀请码注册")
		return
	}
	if input.Password != input.ConfirmPassword {
		writeError(w, http.StatusBadRequest, "两次输入的密码不一致")
		return
	}

	actor, err := s.auth.Register(
		r.Context(),
		w,
		r,
		input.Username,
		input.DisplayName,
		input.Phone,
		input.Province,
		input.City,
		input.District,
		input.Password,
		input.InviteCode,
		input.Captcha,
	)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrCaptchaInvalid):
			writeError(w, http.StatusBadRequest, "验证码错误或已过期，请重新输入")
		case errors.Is(err, auth.ErrUsernameTaken):
			writeError(w, http.StatusConflict, "该登录账号已被使用")
		case errors.Is(err, auth.ErrInvalidUsername):
			writeError(w, http.StatusBadRequest, "账号需为 4-32 位字母、数字、下划线、点或短横线")
		case errors.Is(err, auth.ErrInvalidPhone):
			writeError(w, http.StatusBadRequest, "请输入正确的中国大陆手机号码")
		case errors.Is(err, auth.ErrPhoneTaken):
			writeError(w, http.StatusConflict, "该手机号已经绑定其他账号")
		case errors.Is(err, auth.ErrInvalidInviteCode):
			writeError(w, http.StatusBadRequest, "邀请码无效、已停用或已过期")
		case errors.Is(err, auth.ErrRegistrationCapacity):
			writeError(w, http.StatusConflict, "该代理终端名额已用完，请联系邀请人或平台处理")
		case errors.Is(err, auth.ErrInvalidDisplayName):
			writeError(w, http.StatusBadRequest, "终端名称需为 2-64 个字符")
		case errors.Is(err, auth.ErrWeakPassword):
			writeError(w, http.StatusBadRequest, "密码长度需为 8-72 位")
		default:
			writeError(w, http.StatusInternalServerError, "注册失败")
		}
		return
	}

	payload, err := s.buildBootstrap(r.Context(), actor)
	if err != nil {
		_ = s.auth.Logout(r.Context(), w, r)
		writeError(w, http.StatusInternalServerError, "读取注册登录信息失败")
		return
	}
	writeJSON(w, http.StatusCreated, payload)
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.Logout(r.Context(), w, r); err != nil {
		writeError(w, http.StatusInternalServerError, "退出登录失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authChangePassword(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}

	var input changePasswordRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if input.NewPassword != input.ConfirmPassword {
		writeError(w, http.StatusBadRequest, "两次输入的新密码不一致")
		return
	}

	if err := s.auth.ChangePassword(
		r.Context(),
		w,
		r,
		input.CurrentPassword,
		input.NewPassword,
	); err != nil {
		switch {
		case errors.Is(err, auth.ErrCurrentPassword):
			writeError(w, http.StatusBadRequest, "当前密码错误")
		case errors.Is(err, auth.ErrWeakPassword):
			writeError(w, http.StatusBadRequest, "新密码长度需为 8-72 位")
		case errors.Is(err, auth.ErrNotAuthenticated):
			writeError(w, http.StatusUnauthorized, "请先登录")
		case err.Error() == "new password must be different":
			writeError(w, http.StatusBadRequest, "新密码不能与当前密码相同")
		default:
			writeError(w, http.StatusInternalServerError, "修改密码失败")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{
		"ok": true,
	})
}

func (s *Server) authSessions(w http.ResponseWriter, r *http.Request) {
	items, err := s.auth.ListSessions(r.Context(), r)
	if err != nil {
		if errors.Is(err, auth.ErrNotAuthenticated) {
			writeError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取登录会话失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) authLogoutOtherSessions(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.LogoutOtherSessions(r.Context(), r); err != nil {
		if errors.Is(err, auth.ErrNotAuthenticated) {
			writeError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		writeError(w, http.StatusInternalServerError, "退出其他设备失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
