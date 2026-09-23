package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "livecompanion/management/internal/db"
)

type updateInviteCodeStatusRequest struct {
	Status    string  `json:"status"`
	MaxUses   *uint64 `json:"max_uses,omitempty"`
	ExpiresAt *string `json:"expires_at,omitempty"`
}

func (s *Server) authInvitePreview(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.PathValue("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "邀请码不能为空")
		return
	}

	item, err := s.store.GetInvitePreview(r.Context(), code)
	if err != nil {
		if errors.Is(err, appdb.ErrInviteCodeInvalid) {
			writeError(w, http.StatusNotFound, "邀请码无效、已停用或已过期")
			return
		}
		writeError(w, http.StatusInternalServerError, "校验邀请码失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) invitationDashboard(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}

	item, err := s.store.GetInvitationDashboard(r.Context(), actor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取邀请与推荐数据失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) updateOwnInviteCodeStatus(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}

	var input updateInviteCodeStatusRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Status = strings.TrimSpace(input.Status)
	if input.Status != "active" && input.Status != "disabled" {
		writeError(w, http.StatusBadRequest, "邀请码状态无效")
		return
	}

	if err := s.store.UpdateOwnInviteCodeStatus(r.Context(), actor.UserID, input.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "邀请码不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "更新邀请码状态失败")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) adminUpdateInviteCodeStatus(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsPlatformAdmin() {
		writeError(w, http.StatusForbidden, "仅超级系统管理员可管理全平台邀请码")
		return
	}

	codeID, err := strconv.ParseInt(r.PathValue("codeID"), 10, 64)
	if err != nil || codeID <= 0 {
		writeError(w, http.StatusBadRequest, "邀请码 ID 无效")
		return
	}

	var input updateInviteCodeStatusRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Status = strings.TrimSpace(input.Status)
	if input.Status != "active" && input.Status != "disabled" {
		writeError(w, http.StatusBadRequest, "邀请码状态无效")
		return
	}

	maxUses := uint64(0)
	if input.MaxUses != nil {
		maxUses = *input.MaxUses
	} else {
		current, err := s.store.GetInviteCodeByID(r.Context(), codeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "邀请码不存在")
				return
			}
			writeError(w, http.StatusInternalServerError, "读取邀请码失败")
			return
		}
		maxUses = current.MaxUses
	}

	var expiresAt *time.Time
	if input.ExpiresAt != nil && strings.TrimSpace(*input.ExpiresAt) != "" {
		value, err := time.Parse(time.RFC3339, strings.TrimSpace(*input.ExpiresAt))
		if err != nil {
			writeError(w, http.StatusBadRequest, "过期时间格式无效")
			return
		}
		expiresAt = &value
	} else if input.ExpiresAt == nil {
		current, err := s.store.GetInviteCodeByID(r.Context(), codeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "邀请码不存在")
				return
			}
			writeError(w, http.StatusInternalServerError, "读取邀请码失败")
			return
		}
		expiresAt = current.ExpiresAt
	}

	if err := s.store.UpdateInviteCodePolicy(
		r.Context(),
		codeID,
		input.Status,
		maxUses,
		expiresAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "邀请码不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "更新邀请码失败")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
