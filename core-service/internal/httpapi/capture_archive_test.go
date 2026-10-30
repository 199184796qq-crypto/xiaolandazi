package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"livecompanion/core/internal/capture"
)

func TestCaptureArchiveErrorStatus(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{capture.ErrRecordingExpired, http.StatusGone}, {capture.ErrArchiveNotReady, http.StatusConflict}, {capture.ErrArchiveUnavailable, http.StatusServiceUnavailable}} {
		w := httptest.NewRecorder()
		writeCaptureError(w, tc.err)
		if w.Code != tc.status {
			t.Fatalf("%v: status=%d", tc.err, w.Code)
		}
	}
}
