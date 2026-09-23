package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/model"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Captcha  string `json:"captcha"`
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

	actor, err := s.auth.Login(
		r.Context(),
		w,
		r,
		input.Username,
		input.Password,
		input.Captcha,
	)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrCaptchaInvalid):
			writeError(w, http.StatusBadRequest, "验证码错误或已过期，请重新输入")
		case errors.Is(err, auth.ErrRateLimited):
			writeError(w, http.StatusTooManyRequests, "登录尝试过多，请 15 分钟后再试")
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "账号或密码错误")
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

	writeJSON(w, http.StatusOK, map[string]any{
		"actor": actor,
	})
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
			writeError(w, http.StatusBadRequest, "联系电话不能为空")
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

	writeJSON(w, http.StatusCreated, map[string]any{
		"actor": actor,
	})
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
