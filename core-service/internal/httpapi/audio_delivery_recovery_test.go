package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/coreaudio"
)

func TestAudioDeliveryPanicReturnsStructuredErrorAndNextRequestWorks(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer recoverRoomAudioDelivery(w)
		if calls.Add(1) == 1 {
			panic("secret https://private.example/audio?signature=private")
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
	defer server.Close()
	response, err := server.Client().Post(server.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal("panic turned into EOF", err)
	}
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	var detail audioDeliveryError
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 500 || detail.Code != "audio_delivery_internal_error" || detail.Stage != "播放服务" || !detail.Retryable || strings.Contains(string(raw), "private") || strings.Contains(string(raw), "panic") {
		t.Fatalf("unsafe or ambiguous response: %d %s", response.StatusCode, raw)
	}
	next, err := server.Client().Post(server.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	next.Body.Close()
	if next.StatusCode != 200 {
		t.Fatal("request failure damaged subsequent delivery", next.StatusCode)
	}
}

func TestAppendFailureKeepsQueuePressureRetryableAndBadUnitsIsolated(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
		retry  bool
	}{
		{coreaudio.ErrProgramQueueFull, 503, "audio_queue_full", true},
		{coreaudio.ErrProgramPositionInvalid, 503, "resume_position_invalid", true},
		{coreaudio.ErrProgramRefreshConflict, 409, "audio_program_state_changed", true},
		{io.ErrUnexpectedEOF, 400, "audio_unit_not_accepted", false},
	} {
		writer := httptest.NewRecorder()
		writeAudioAppendFailure(writer, test.err)
		var detail audioDeliveryError
		if err := json.Unmarshal(writer.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
		if writer.Code != test.status || detail.Code != test.code || detail.Retryable != test.retry {
			t.Fatal(writer.Code, detail)
		}
	}
}

func TestFormalAudioStartDoesNotReviveStoppedAgent(t *testing.T) {
	server := &Server{agentWork: agentwork.New()}
	if server.formalAudioStartAllowed(29, 208) {
		t.Fatal("old producer may restart stopped room")
	}
	if _, err := server.agentWork.StartAgent(29, 0); err != nil {
		t.Fatal(err)
	}
	if !server.formalAudioStartAllowed(29, 208) {
		t.Fatal("active room rejected")
	}
	_, _ = server.agentWork.StopAgent(29, agentwork.StopReasonManual)
	if server.formalAudioStartAllowed(29, 208) {
		t.Fatal("retry revived manually stopped room")
	}
	if !server.formalAudioStartAllowed(29, 0) {
		t.Fatal("versionless developer tests changed")
	}
}
