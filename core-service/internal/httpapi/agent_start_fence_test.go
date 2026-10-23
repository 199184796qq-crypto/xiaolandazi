package httpapi

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/core/internal/agentwork"
	roomstore "livecompanion/core/internal/room"
)

func guardedStartHTTPFixture(t *testing.T) (*Server, agentwork.Snapshot) {
	t.Helper()
	db, err := sql.Open("phase1-return-room", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := New(roomstore.NewStore(db), nil, nil, nil, nil, "development", "start-fence-token")
	_, _ = s.agentWork.StartAgent(94, 0)
	paused, _ := s.agentWork.StopAgent(94, agentwork.StopReasonManualPause)
	return s, paused
}

func agentStartBody(snapshot agentwork.Snapshot) string {
	raw, _ := json.Marshal(map[string]any{"command": "start", "base_working_seconds": snapshot.WorkingSeconds, "expected_boot_id": snapshot.BootID, "expected_run_generation": snapshot.RunGeneration, "expected_control_revision": snapshot.ControlRevision})
	return string(raw)
}

func agentStartRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/internal/v1/rooms/94/agent-runtime?tenant_id=7", strings.NewReader(body))
	r.Header.Set("X-Core-Token", "start-fence-token")
	return r
}

func TestAgentConditionalStartHTTPCommitsOnceAndRejectsUsedBaseline(t *testing.T) {
	s, paused := guardedStartHTTPFixture(t)
	request := agentStartBody(paused)
	first := httptest.NewRecorder()
	s.Handler().ServeHTTP(first, agentStartRequest(request))
	var resumed agentwork.Snapshot
	if err := json.Unmarshal(first.Body.Bytes(), &resumed); err != nil || first.Code != http.StatusOK || resumed.State != agentwork.StateWorking || resumed.ControlRevision != paused.ControlRevision+1 || resumed.RunGeneration != paused.RunGeneration+1 {
		t.Fatal(first.Code, first.Body.String(), err)
	}
	late := httptest.NewRecorder()
	s.Handler().ServeHTTP(late, agentStartRequest(request))
	var detail audioDeliveryError
	_ = json.Unmarshal(late.Body.Bytes(), &detail)
	if late.Code != http.StatusConflict || detail.Code != "agent_start_baseline_conflict" || detail.Retryable || strings.Contains(detail.Error, "runtime") {
		t.Fatal(late.Code, late.Body.String())
	}
	legacy := httptest.NewRecorder()
	s.Handler().ServeHTTP(legacy, agentStartRequest(`{"command":"start"}`))
	if legacy.Code != http.StatusOK || s.agentWork.Get(94).RunGeneration != resumed.RunGeneration || s.agentWork.Get(94).ControlRevision != resumed.ControlRevision {
		t.Fatal("legacy working retry changed the runtime", legacy.Code, legacy.Body.String())
	}
}

func TestAgentConditionalStartHTTPCannotArriveAfterManualStop(t *testing.T) {
	s, paused := guardedStartHTTPFixture(t)
	arrived, release := make(chan struct{}), make(chan struct{})
	handler := s.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(arrived)
		<-release
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	status := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPut, server.URL+"/internal/v1/rooms/94/agent-runtime?tenant_id=7", strings.NewReader(agentStartBody(paused)))
		req.Header.Set("X-Core-Token", "start-fence-token")
		response, err := server.Client().Do(req)
		if err != nil {
			status <- 0
			return
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		status <- response.StatusCode
	}()
	<-arrived
	stopped, _ := s.agentWork.StopAgent(94, agentwork.StopReasonManual)
	close(release)
	if code := <-status; code != http.StatusConflict {
		t.Fatal("late start was not rejected", code)
	}
	current := s.agentWork.Get(94)
	if current.State != agentwork.StateStopped || current.RunGeneration != paused.RunGeneration || current.ControlRevision != stopped.ControlRevision {
		t.Fatal("late request revived the stopped agent", current)
	}
}

func TestAgentConditionalStartHTTPDoesNotTreatNullGuardAsLegacyStart(t *testing.T) {
	for _, guard := range []string{`"expected_run_generation":null`, `"expected_control_revision":null`, `"expected_boot_id":""`, `"expected_boot_id":"boot","expected_run_generation":1`} {
		s, paused := guardedStartHTTPFixture(t)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, agentStartRequest(`{"command":"start",`+guard+`}`))
		if w.Code != http.StatusConflict || s.agentWork.Get(94).State != agentwork.StateStopped || s.agentWork.Get(94).ControlRevision != paused.ControlRevision {
			t.Fatal("incomplete guard fell back to a fresh start", w.Code, w.Body.String())
		}
	}
}
