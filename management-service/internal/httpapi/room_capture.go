package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
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
	if recordingID := r.URL.Query().Get("recording_id"); recordingID != "" {
		query.Set("recording_id", recordingID)
	}
	resp, err := s.core.DoRoom(
		r.Context(),
		tenantID,
		roomID,
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d/capture/audio/download", roomID),
		query,
		nil,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "录音文件暂不可下载")
		return
	}
	defer resp.Body.Close()
	redirectRecordingDownload(w, r, resp)
}

func redirectRecordingDownload(w http.ResponseWriter, r *http.Request, resp *http.Response) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 16*1024))
		return
	}
	var result struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(&result); err != nil {
		writeError(w, http.StatusBadGateway, "获取录音下载地址失败")
		return
	}
	address, err := url.Parse(result.URL)
	if err != nil || address.Scheme != "https" || address.Hostname() == "" || address.User != nil {
		writeError(w, http.StatusBadGateway, "录音下载地址无效")
		return
	}
	// The browser follows this redirect. Management never fetches the WAV.
	http.Redirect(w, r, address.String(), http.StatusFound)
}
