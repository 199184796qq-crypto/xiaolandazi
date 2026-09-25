package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

func (s *Server) getRoomSessionStats(w http.ResponseWriter, r *http.Request) {
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
	resp, err := s.core.DoRoom(r.Context(), tenantID, roomID, http.MethodGet, fmt.Sprintf("/internal/v1/rooms/%d/session-stats", roomID), query, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "直播采集统计暂时不可用")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) resolveRoomSessionDecision(w http.ResponseWriter, r *http.Request) {
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
		Action string `json:"action"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "直播续接参数格式错误")
		return
	}
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		r.Context(),
		tenantID,
		roomID,
		http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/session-decision", roomID),
		query,
		map[string]any{"action": input.Action},
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "直播续接处理暂时不可用")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) getRoomSpeechRuntime(w http.ResponseWriter, r *http.Request) {
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
		fmt.Sprintf("/internal/v1/rooms/%d/speech-runtime", roomID),
		query,
		nil,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "直播口播状态暂时不可用")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) getRoomBrain(w http.ResponseWriter, r *http.Request) {
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
		fmt.Sprintf("/internal/v1/rooms/%d/brain", roomID),
		query,
		nil,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "直播语义状态暂时不可用")
		return
	}
	s.copyCoreResponse(w, resp)
}
