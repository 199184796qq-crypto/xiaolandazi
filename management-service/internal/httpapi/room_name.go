package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func validEditedRoomName(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) > 0 && utf8.RuneCountInString(value) <= 128 && strings.IndexFunc(value, unicode.IsControl) < 0
}

func (s *Server) updateRoomName(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "仅终端账号可修改自己的直播间名称")
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	var input struct {
		Name         string  `json:"name"`
		ExpectedName *string `json:"expected_name"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if !validEditedRoomName(input.Name) || input.ExpectedName == nil || !utf8.ValidString(*input.ExpectedName) || utf8.RuneCountInString(*input.ExpectedName) > 128 {
		writeError(w, http.StatusBadRequest, "请输入1至128个字符的直播间名称，不能含换行或控制字符")
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	query := url.Values{"tenant_id": {strconv.FormatInt(tenantID, 10)}}
	response, err := s.core.DoRoom(r.Context(), tenantID, roomID, http.MethodPatch, fmt.Sprintf("/internal/v1/rooms/%d/name", roomID), query, input)
	if err != nil {
		writeError(w, http.StatusBadGateway, "核心服务暂不可用，名称未确认保存，请稍后重试")
		return
	}
	s.copyCoreResponse(w, response)
}
