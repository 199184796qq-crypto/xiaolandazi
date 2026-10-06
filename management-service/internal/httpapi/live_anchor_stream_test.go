package httpapi

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPreviewStreamFlushesAndKeepsTerminalJSONOutOfSpeech(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("Accept", "text/event-stream")
	stream := openAnchorPreviewStream(w, r)
	stream.send("segment", map[string]any{"text": "第一段\n继续", "segment_index": 1})
	if !w.Flushed || !strings.Contains(w.Body.String(), "event: segment") {
		t.Fatal("not flushed")
	}
	writeJSON(stream.resultWriter(), http.StatusOK, map[string]any{"text": "最终正文", "content_strategy": map[string]any{"actual_steps": []string{"scenario"}}})
	if !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(w.Body.String(), "event: complete") || w.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("invalid streaming result: %s", w.Body.String())
	}
	writeError(stream.resultWriter(), http.StatusBadGateway, "生成未完成")
	if !strings.Contains(w.Body.String(), "event: error") {
		t.Fatal("missing terminal error")
	}
}

func TestNonstreamClientsRemainJSON(t *testing.T) {
	if openAnchorPreviewStream(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil)) != nil {
		t.Fatal("forced stream on JSON client")
	}
}

func TestPreviewStreamWorksThroughActualAuditWrapper(t *testing.T) {
	w := httptest.NewRecorder()
	audit := &auditStatusWriter{ResponseWriter: w}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/live-agent-plans/2/anchor-style/test", nil)
	r.Header.Set("Accept", "text/event-stream")
	if !shouldAuditRequest(r.Method, r.URL.Path) {
		t.Fatal("test route is not audited")
	}
	if _, ok := any(audit).(http.Flusher); ok {
		t.Fatal("fixture must reproduce wrapper without direct Flusher")
	}
	stream := openAnchorPreviewStream(audit, r)
	if stream == nil {
		t.Fatal("audited preview silently fell back to whole-response JSON")
	}
	stream.send("segment", map[string]any{"text": "已返回的第一段", "segment_index": 1})
	if !w.Flushed || !strings.Contains(w.Body.String(), "event: segment") || audit.status != http.StatusOK {
		t.Fatalf("audited segment not flushed before completion: status=%d body=%s", audit.status, w.Body.String())
	}
	writeJSON(stream.resultWriter(), http.StatusOK, map[string]any{"text": "最终完整稿"})
	if !strings.Contains(w.Body.String(), "event: complete") {
		t.Fatal("missing terminal event")
	}
}

func TestPreviewStreamArrivesBeforeHandlerCompletesThroughBothWrappers(t *testing.T) {
	finish := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inbox := &inboxStatusWriter{ResponseWriter: w, status: http.StatusOK}
		audit := &auditStatusWriter{ResponseWriter: inbox, status: http.StatusOK}
		stream := openAnchorPreviewStream(audit, r)
		if stream == nil {
			writeError(audit, http.StatusInternalServerError, "stream unsupported")
			return
		}
		stream.send("segment", map[string]any{"text": "第一段", "segment_index": 1})
		select {
		case <-finish:
		case <-r.Context().Done():
			return
		}
		writeJSON(stream.resultWriter(), http.StatusOK, map[string]any{"text": "第一段和第二段"})
	}))
	defer server.Close()
	req, _ := http.NewRequest(http.MethodPost, server.URL, nil)
	req.Header.Set("Accept", "text/event-stream")
	response, err := server.Client().Do(req)
	if err != nil {
		close(finish)
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	first, err := reader.ReadString('\n')
	if err != nil || first != "event: segment\n" {
		close(finish)
		t.Fatalf("first event=%q err=%v", first, err)
	}
	// Only release completion after a real network client sees the first segment.
	close(finish)
	rest, err := io.ReadAll(reader)
	if err != nil || !strings.Contains(string(rest), "event: complete") {
		t.Fatalf("terminal frame missing: %s err=%v", rest, err)
	}
}
