package httpapi

import (
	"context"
	"fmt"
	"io"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (s *Server) archiveMainlineReceipts(ctx context.Context, tenant, room, session int64, p coreclient.ProgramRefreshSnapshot) {
	for _, receipt := range p.MainlinePlaybackReceipts {
		at, err := time.Parse(time.RFC3339Nano, receipt.StartedAt)
		if err != nil {
			continue
		}
		_ = s.store.MarkSessionSpeechPlayed(ctx, tenant, room, session, model.SessionMainlineRef(p.ProgramID, receipt.TrackID), at)
	}
}

func (s *Server) sessionDownloadScope(w http.ResponseWriter, r *http.Request) (model.LiveRuntimeSession, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.LiveRuntimeSession{}, false
	}
	room, ok := pathID(w, r)
	if !ok {
		return model.LiveRuntimeSession{}, false
	}
	tenant, ok := s.tenantForRoom(w, r, actor, room)
	if !ok {
		return model.LiveRuntimeSession{}, false
	}
	id := int64(0)
	if raw := r.URL.Query().Get("session_id"); raw != "" {
		var err error
		id, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, 400, "无效的直播场次")
			return model.LiveRuntimeSession{}, false
		}
	}
	session, err := s.store.GetGeneratedSpeechHistorySession(r.Context(), tenant, room, id)
	if err != nil {
		writeError(w, 404, "本场还没有直播记录")
		return session, false
	}
	return session, true
}

func parseSessionDownloadRange(from, to string) (*time.Time, *time.Time, error) {
	var start, end *time.Time
	for i, raw := range []string{from, to} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, nil, fmt.Errorf("请选择有效的开始和结束时间")
		}
		if i == 0 {
			start = &at
		} else {
			end = &at
		}
	}
	if start != nil && end != nil && !start.Before(*end) {
		return nil, nil, fmt.Errorf("结束时间须晚于开始时间")
	}
	return start, end, nil
}

func sessionTranscript(items []model.SessionSpeechAsset) string {
	texts := []string{}
	for _, a := range items {
		if a.PlayedAt != nil && strings.TrimSpace(a.Text) != "" {
			texts = append(texts, strings.TrimSpace(a.Text))
		}
	}
	return strings.Join(texts, "\n\n")
}

func (s *Server) sessionDownloads(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	session, ok := s.sessionDownloadScope(w, r)
	if !ok {
		return
	}
	// The current Core receipt is used only to complete immutable history. It
	// cannot restart a program or reconstruct transient scheduling state.
	s.syncSessionArchive(r.Context(), session)
	if r.URL.Query().Get("download") == "transcript" {
		items, err := s.store.ListSessionSpeechAssets(r.Context(), session.TenantID, session.RoomID, session.ID, nil, nil)
		if err != nil {
			writeError(w, 500, "读取本场文案失败")
			return
		}
		if len(items) == 0 {
			writeError(w, 404, "本场还没有已播放的文案")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="live-room-%d-session-%d.txt"`, session.RoomID, session.ID))
		_, _ = io.WriteString(w, sessionTranscript(items))
		return
	}
	from, to, err := parseSessionDownloadRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	items, err := s.store.ListSessionSpeechAssets(r.Context(), session.TenantID, session.RoomID, session.ID, from, to)
	if err != nil {
		writeError(w, 500, "读取声音列表失败")
		return
	}
	page := parseGeneratedSpeechInt(r.URL.Query().Get("page"), 1)
	size := 50
	total := len(items)
	start := min(total, (min(page, 1_000_000)-1)*size)
	end := min(total, start+size)
	writeJSON(w, 200, map[string]any{"session_id": session.ID, "started_at": session.StartedAt, "ended_at": session.EndedAt, "items": items[start:end], "total": total, "page": page, "page_size": size})
}

func sessionAudioURLAllowed(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && strings.HasSuffix(strings.ToLower(u.Hostname()), ".aliyuncs.com")
}

// DashScope can return signed HTTP result URLs even though the same object and
// signature support HTTPS. Upgrade at the download boundary, including saved
// historical rows; never fetch plaintext or relax the HTTPS redirect policy.
func sessionAudioDownloadURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if u.Scheme == "http" {
		host := strings.ToLower(u.Hostname())
		if u.User != nil || u.Port() != "" || !strings.HasPrefix(host, "dashscope-result-") || !strings.Contains(host, ".oss-") || !strings.HasSuffix(host, ".aliyuncs.com") {
			return "", false
		}
		u.Scheme = "https"
		raw = u.String()
	}
	return raw, sessionAudioURLAllowed(raw)
}

func sessionAudioClient() *http.Client {
	return &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || !sessionAudioURLAllowed(req.URL.String()) {
			return fmt.Errorf("invalid audio redirect")
		}
		return nil
	}}
}

func (s *Server) downloadSessionAudio(w http.ResponseWriter, r *http.Request) {
	session, ok := s.sessionDownloadScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("assetID"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, 400, "无效的声音记录")
		return
	}
	a, err := s.store.GetSessionSpeechAsset(r.Context(), session.TenantID, session.RoomID, session.ID, id)
	if err != nil {
		writeError(w, 404, "本场声音记录不存在")
		return
	}
	if !a.Available {
		writeError(w, 410, "声音文件已到期或不可用")
		return
	}
	address, allowed := sessionAudioDownloadURL(a.AudioURL)
	if !allowed {
		writeError(w, 502, "声音文件地址不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	client := sessionAudioClient()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, 502, "声音下载暂不可用")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		writeError(w, 410, "声音文件已到期或不可用")
		return
	}
	if resp.ContentLength > 256<<20 {
		writeError(w, 502, "声音文件过大，暂不可下载")
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="live-room-%d-session-%d-audio-%d.wav"`, session.RoomID, session.ID, a.ID))
	w.Header().Set("Cache-Control", "private, no-store")
	if resp.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	_, _ = io.Copy(w, resp.Body)
}
