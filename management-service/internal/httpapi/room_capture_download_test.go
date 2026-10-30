package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRoomCaptureDownloadRedirectDoesNotFetchAudio(t *testing.T) {
	var downloads atomic.Int32
	oss := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		t.Error("Management fetched OSS recording")
	}))
	defer oss.Close()
	address := oss.URL + "/recording.wav?test-signature=local"
	raw, _ := json.Marshal(map[string]string{"url": address})
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(raw)))}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/33/capture/audio/file", nil)
	redirectRecordingDownload(w, r, response)
	if w.Code != http.StatusFound || w.Header().Get("Location") != address || w.Header().Get("Cache-Control") != "no-store" || downloads.Load() != 0 {
		t.Fatalf("not an OSS redirect: %d", w.Code)
	}
	if w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("referrer protection missing")
	}
}

func TestRoomCaptureDownloadUnreadyExpiredAndInvalidLinks(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   int
	}{
		{409, `{"error":"uploading"}`, 409}, {410, `{"error":"expired"}`, 410}, {503, `{"error":"unavailable"}`, 503},
		{200, `{"url":"http://recording.example.test/test.wav"}`, 502},
		{200, `{"url":"https://user:pass@recording.example.test/test.wav"}`, 502},
		{200, `{"url":"javascript:alert(1)"}`, 502}, {200, `not json`, 502},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/33/capture/audio/file", nil)
		redirectRecordingDownload(w, r, &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))})
		if w.Code != tc.want || w.Header().Get("Location") != "" {
			t.Fatalf("status=%d body=%s got=%d", tc.status, tc.body, w.Code)
		}
	}
}
