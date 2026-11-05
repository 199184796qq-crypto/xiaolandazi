package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var editableRoomNumber = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func normalizeEditedRoomNumber(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host != "live.douyin.com" || parsed.User != nil {
			return "", fmt.Errorf("请输入有效的抖音直播链接或房间号")
		}
		value = strings.Trim(parsed.Path, "/")
	}
	if !editableRoomNumber.MatchString(value) {
		return "", fmt.Errorf("请输入128个字符以内的有效房间号，仅支持字母、数字、下划线和短横线")
	}
	return value, nil
}

func (s *Server) updateRoomNumber(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "仅终端账号可修改自己的直播间房间号")
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	var input struct {
		ExternalRoomID         string `json:"external_room_id"`
		ExpectedExternalRoomID string `json:"expected_external_room_id"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	number, err := normalizeEditedRoomNumber(input.ExternalRoomID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.ExpectedExternalRoomID == "" || len(input.ExpectedExternalRoomID) > 128 {
		writeError(w, http.StatusBadRequest, "缺少原房间号，请刷新后重试")
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	active, err := s.store.GetLiveRuntimeByRoom(r.Context(), tenantID, roomID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusServiceUnavailable, "无法确认直播状态，请稍后重试")
		return
	}
	if err == nil && (active.Status == "running" || active.Status == "paused") {
		writeError(w, http.StatusConflict, "请先结束直播并停止直播间连接，再修改房间号")
		return
	}
	input.ExternalRoomID = number
	query := url.Values{"tenant_id": {strconv.FormatInt(tenantID, 10)}}
	response, err := s.core.DoRoom(r.Context(), tenantID, roomID, http.MethodPatch, fmt.Sprintf("/internal/v1/rooms/%d/number", roomID), query, input)
	if err != nil {
		writeError(w, http.StatusBadGateway, "核心服务暂不可用，房间号未确认保存，请稍后重试")
		return
	}
	s.copyCoreResponse(w, response)
}
