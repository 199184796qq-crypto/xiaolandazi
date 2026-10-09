package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

func (s *Server) roomCompositeSubtitles(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	room, ok := pathID(w, r)
	if !ok {
		return
	}
	tenant, ok := s.tenantForRoom(w, r, actor, room)
	if !ok {
		return
	}
	response, err := s.core.DoRoom(r.Context(), tenant, room, http.MethodGet, fmt.Sprintf("/internal/v1/rooms/%d/composite.srt", room), url.Values{"tenant_id": {strconv.FormatInt(tenant, 10)}}, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "字幕输出暂不可用")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		writeError(w, response.StatusCode, "字幕输出暂不可用")
		return
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		writeError(w, http.StatusBadGateway, "字幕读取失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"srt": string(data)})
}
