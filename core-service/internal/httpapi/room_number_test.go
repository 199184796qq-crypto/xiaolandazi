package httpapi

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/core/internal/agentwork"
	roomstore "livecompanion/core/internal/room"
)

func TestRoomNumberHTTPRequiresScopeAndStoppedRuntime(t *testing.T) {
	for _, kind := range []string{"token", "tenant", "invalid", "unknown_field", "working", "paused", "audio"} {
		t.Run(kind, func(t *testing.T) {
			db, _ := sql.Open("phase1-return-room", "")
			defer db.Close()
			s := New(roomstore.NewStore(db), nil, nil, nil, nil, "production", "number-token")
			path := "/internal/v1/rooms/94/number?tenant_id=7"
			body := `{"external_room_id":"123","expected_external_room_id":"phase1-test"}`
			want := http.StatusConflict
			switch kind {
			case "token":
				want = http.StatusUnauthorized
			case "tenant":
				path = "/internal/v1/rooms/94/number"
				want = http.StatusBadRequest
			case "invalid":
				body = `{"external_room_id":"../bad","expected_external_room_id":"phase1-test"}`
				want = http.StatusBadRequest
			case "unknown_field":
				body = `{"external_room_id":"123","name":"silently renamed"}`
				want = http.StatusBadRequest
			case "working":
				s.agentWork.StartAgent(94, 0)
			case "paused":
				s.agentWork.StartAgent(94, 0)
				s.agentWork.StopAgent(94, agentwork.StopReasonManualPause)
			case "audio":
				s.roomAudio.StartMainline(94, "still playing")
			}
			r := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
			if kind != "token" {
				r.Header.Set("X-Core-Token", "number-token")
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != want {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
