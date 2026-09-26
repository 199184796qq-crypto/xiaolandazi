package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

func (s *Server) roomCaptureScope(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return 0, 0, false
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return 0, 0, false
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return 0, 0, false
	}
	return tenantID, roomID, true
}

func (s *Server) roomCaptureProxy(
	w http.ResponseWriter,
	r *http.Request,
	method string,
	suffix string,
	body any,
) {
	tenantID, roomID, ok := s.roomCaptureScope(w, r)
	if !ok {
		return
	}
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		r.Context(),
		tenantID,
		roomID,
		method,
		fmt.Sprintf("/internal/v1/rooms/%d/capture%s", roomID, suffix),
		query,
		body,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "核心服务采集功能暂不可用")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) roomCaptureStatus(w http.ResponseWriter, r *http.Request) {
	s.roomCaptureProxy(w, r, http.MethodGet, "", nil)
}

func (s *Server) roomCaptureAudioStart(w http.ResponseWriter, r *http.Request) {
	s.roomCaptureProxy(w, r, http.MethodPost, "/audio/start", nil)
}

func (s *Server) roomCaptureAudioStop(w http.ResponseWriter, r *http.Request) {
	s.roomCaptureProxy(w, r, http.MethodPost, "/audio/stop", nil)
}

func (s *Server) roomCaptureAudioFile(w http.ResponseWriter, r *http.Request) {
	tenantID, roomID, ok := s.roomCaptureScope(w, r)
	if !ok {
		return
	}
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.StreamRoom(
		r.Context(),
		tenantID,
		roomID,
		fmt.Sprintf("/internal/v1/rooms/%d/capture/audio/file", roomID),
		query,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "录音文件暂不可下载")
		return
	}
	defer resp.Body.Close()
	copyResponse(w, resp)
}
