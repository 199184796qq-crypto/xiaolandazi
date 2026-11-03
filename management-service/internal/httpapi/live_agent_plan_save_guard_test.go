package httpapi

import (
	"errors"
	appdb "livecompanion/management/internal/db"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlanSaveGuardShowsOccupyingRoomsAndFailsClosed(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		text   string
	}{
		{&appdb.LiveAgentPlanInUseError{RoomIDs: []int64{26, 29}}, http.StatusConflict, "直播间 26、29"},
		{errors.New("private database detail"), http.StatusServiceUnavailable, "本次未保存"},
	} {
		response := httptest.NewRecorder()
		if !writePlanSaveGuardError(response, test.err) || response.Code != test.status || !strings.Contains(response.Body.String(), test.text) || strings.Contains(response.Body.String(), "private database detail") {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if writePlanSaveGuardError(httptest.NewRecorder(), nil) {
		t.Fatal("idle plan blocked")
	}
}
