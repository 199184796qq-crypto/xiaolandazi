package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// 直播大屏「屏蔽相同内容」：与屏蔽用户同口径，实体存在 Core，
// Management 只做鉴权、租户归属解析和转发。

func (s *Server) listRoomBlockedContents(w http.ResponseWriter, r *http.Request) {
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
		fmt.Sprintf("/internal/v1/rooms/%d/blocked-contents", roomID),
		query,
		nil,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "屏蔽内容列表暂时不可用")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) blockRoomContent(w http.ResponseWriter, r *http.Request) {
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
		Content string `json:"content"`
		Reason  string `json:"reason"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "屏蔽内容格式错误")
		return
	}
	input.Content = strings.TrimSpace(input.Content)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Content == "" {
		writeError(w, http.StatusBadRequest, "缺少要屏蔽的内容")
		return
	}
	if input.Reason == "" {
		input.Reason = "公屏右键人工屏蔽内容"
	}

	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		r.Context(),
		tenantID,
		roomID,
		http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/blocked-contents", roomID),
		query,
		input,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "屏蔽内容失败")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) restoreRoomBlockedContent(w http.ResponseWriter, r *http.Request) {
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
		ContentKey string `json:"content_key"`
		Content    string `json:"content"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "恢复内容格式错误")
		return
	}
	input.ContentKey = strings.TrimSpace(input.ContentKey)
	input.Content = strings.TrimSpace(input.Content)
	if input.ContentKey == "" && input.Content == "" {
		writeError(w, http.StatusBadRequest, "缺少内容标识")
		return
	}

	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		r.Context(),
		tenantID,
		roomID,
		http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/blocked-contents/restore", roomID),
		query,
		input,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "恢复内容失败")
		return
	}
	s.copyCoreResponse(w, resp)
}
