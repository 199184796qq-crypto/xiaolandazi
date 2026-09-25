package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (s *Server) listRoomBlockedUsers(w http.ResponseWriter, r *http.Request) {
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

	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		r.Context(),
		tenantID,
		roomID,
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d/blocked-users", roomID),
		query,
		nil,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "屏蔽池暂时不可用")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) blockRoomUser(w http.ResponseWriter, r *http.Request) {
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

	var input struct {
		UserID   string `json:"user_id"`
		Nickname string `json:"nickname"`
		Reason   string `json:"reason"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "屏蔽信息格式错误")
		return
	}
	input.UserID = strings.TrimSpace(input.UserID)
	input.Nickname = strings.TrimSpace(input.Nickname)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.UserID == "" && input.Nickname == "" {
		writeError(w, http.StatusBadRequest, "缺少用户标识")
		return
	}
	if input.Reason == "" {
		input.Reason = "人工屏蔽"
	}

	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		r.Context(),
		tenantID,
		roomID,
		http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/blocked-users", roomID),
		query,
		input,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "屏蔽用户失败")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) restoreRoomBlockedUser(w http.ResponseWriter, r *http.Request) {
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

	var input struct {
		UserID   string `json:"user_id"`
		Nickname string `json:"nickname"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "恢复信息格式错误")
		return
	}
	input.UserID = strings.TrimSpace(input.UserID)
	input.Nickname = strings.TrimSpace(input.Nickname)
	if input.UserID == "" && input.Nickname == "" {
		writeError(w, http.StatusBadRequest, "缺少用户标识")
		return
	}

	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		r.Context(),
		tenantID,
		roomID,
		http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/blocked-users/restore", roomID),
		query,
		input,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "恢复用户失败")
		return
	}
	s.copyCoreResponse(w, resp)
}
