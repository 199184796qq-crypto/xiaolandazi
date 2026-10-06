package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Real accepted segments, not a simulated typewriter or provisional unsafe text.
type anchorPreviewStream struct {
	writer  http.ResponseWriter
	flusher *http.ResponseController
}

// Middleware may deliberately expose only ResponseWriter + Unwrap. Detect the
// underlying capability without bypassing middleware writes/status accounting.
func anchorResponseCanFlush(w http.ResponseWriter) bool {
	for depth := 0; depth < 16; depth++ {
		if _, ok := w.(interface{ FlushError() error }); ok {
			return true
		}
		if _, ok := w.(http.Flusher); ok {
			return true
		}
		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return false
		}
		w = unwrapper.Unwrap()
	}
	return false
}

func openAnchorPreviewStream(w http.ResponseWriter, r *http.Request) *anchorPreviewStream {
	if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		return nil
	}
	if !anchorResponseCanFlush(w) {
		return nil
	}
	flusher := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = flusher.Flush()
	return &anchorPreviewStream{w, flusher}
}

func (s *anchorPreviewStream) send(event string, value any) {
	if s == nil {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(s.writer, "event: %s\ndata: %s\n\n", event, data)
	_ = s.flusher.Flush()
}

// Convert the handler's existing final JSON/error response into a terminal SSE
// event. Keep headers local: Content-Type must remain text/event-stream.
type anchorStreamResultWriter struct {
	stream  *anchorPreviewStream
	headers http.Header
	status  int
}

func (s *anchorPreviewStream) resultWriter() http.ResponseWriter {
	return &anchorStreamResultWriter{s, http.Header{}, http.StatusOK}
}
func (w *anchorStreamResultWriter) Header() http.Header    { return w.headers }
func (w *anchorStreamResultWriter) WriteHeader(status int) { w.status = status }
func (w *anchorStreamResultWriter) Write(data []byte) (int, error) {
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, err
	}
	event := "complete"
	if w.status >= 400 {
		event = "error"
	}
	w.stream.send(event, payload)
	return len(data), nil
}
