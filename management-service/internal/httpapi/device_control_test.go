package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/auth"
	appdb "livecompanion/management/internal/db"
)

func TestDeviceControlErrorsAreChineseAndDoNotClaimHandshake(t *testing.T) {
	for _, tc := range []struct {
		err     error
		status  int
		message string
	}{
		{appdb.ErrLiveDeviceOffline, http.StatusConflict, "等待自动重连"},
		{fmt.Errorf("private wrapper: %w", appdb.ErrLiveDeviceOffline), http.StatusConflict, "尚未收到盒子的有效连接"},
		{appdb.ErrLiveDeviceNotBound, http.StatusConflict, "设备没有绑定到该直播间"},
		{sql.ErrNoRows, http.StatusNotFound, "设备不存在或不属于当前客户"},
		{errors.New("private password transport stack trace"), http.StatusInternalServerError, "设备控制失败"},
	} {
		w := httptest.NewRecorder()
		writeLiveDeviceControlError(w, tc.err)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.message) {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, leak := range []string{"private", "password", "transport", "stack trace", "握手成功"} {
			if strings.Contains(w.Body.String(), leak) {
				t.Fatal("misleading or leaked error", w.Body.String())
			}
		}
	}
}

func TestBrowserDevicePresenceAndControlStillRequireAuthentication(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	for _, handler := range []http.HandlerFunc{s.liveDeviceHeartbeat, s.liveDeviceControl} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/live/devices/7/control", strings.NewReader(`{"room_id":26,"action":"connect"}`))
		r.SetPathValue("deviceID", "7")
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
