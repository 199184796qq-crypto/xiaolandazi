package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"livecompanion/management/internal/model"
)

func (s *Server) liveOpsListRoomQuotas(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "liveops.room_quota.view"); !ok {
		return
	}
	items, err := s.store.ListLiveOpsRoomQuotas(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取客户直播间配额失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveOpsAdjustRoomQuota(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "liveops.room_quota.manage")
	if !ok {
		return
	}
	tenantID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("tenantID")), 10, 64)
	if err != nil || tenantID <= 0 {
		writeError(w, http.StatusBadRequest, "客户 ID 无效")
		return
	}
	var input model.LiveOpsRoomQuotaAdjustInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		writeError(w, http.StatusBadRequest, "调整原因不能为空")
		return
	}
	item, err := s.store.AdjustLiveOpsRoomQuota(
		r.Context(),
		actor.UserID,
		tenantID,
		input,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "客户不存在")
		case strings.Contains(err.Error(), "lower than current room count"):
			writeError(w, http.StatusConflict, "新配额不能低于该客户当前已有直播间数量")
		case strings.Contains(err.Error(), "membership maximum"):
			writeError(w, http.StatusConflict, "新配额不能超过客户当前会员等级的直播间上限")
		case strings.Contains(err.Error(), "platform maximum"):
			writeError(w, http.StatusBadRequest, "每个客户最多允许 10 个直播间")
		case strings.Contains(err.Error(), "negative"):
			writeError(w, http.StatusBadRequest, "客户直播间配额不能小于 0")
		default:
			writeError(w, http.StatusBadRequest, "调整客户直播间配额失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}
