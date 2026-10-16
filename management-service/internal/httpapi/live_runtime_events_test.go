package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/auth"
	appdb "livecompanion/management/internal/db"
)

func TestLiveRuntimeEventFailureDistinguishesInputFromStorage(t *testing.T) {
	for _, test := range []struct {
		err     error
		status  int
		message string
	}{
		{appdb.ErrUnsupportedLiveRuntimeEvent, 400, "不支持的直播操作记录"},
		{fmt.Errorf("wrapped: %w", appdb.ErrUnsupportedLiveRuntimeEvent), 400, "不支持的直播操作记录"},
		{errors.New("private database detail"), 500, "保存直播操作记录失败"},
	} {
		w := httptest.NewRecorder()
		writeLiveRuntimeRecordEventError(w, test.err)
		if w.Code != test.status || !strings.Contains(w.Body.String(), test.message) || strings.Contains(w.Body.String(), "private database detail") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestLiveRuntimeRecordEventStillRequiresAuthentication(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/26/runtime/events", strings.NewReader(`{"event_code":"LIVE_AGENT_PLAN_SELECTED","detail":{"plan_id":5}}`))
	r.SetPathValue("roomID", "26")
	s.liveRuntimeRecordEvent(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
}
