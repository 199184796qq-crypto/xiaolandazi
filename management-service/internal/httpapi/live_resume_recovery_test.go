package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
)

func newResumeRecoveryFixture() (*liveResumeRecovery, *liveResumeProgram, *coreAgentRuntimeState, *model.LiveRuntimeSession) {
	paused, revision := true, uint64(7)
	program := &liveResumeProgram{ProgramID: "program-386", VersionID: 208, SessionID: "recording-stream-386", Running: true, ManualPaused: &paused, ControlRevision: &revision}
	agent := &coreAgentRuntimeState{BootID: "boot-1", RunGeneration: 3, RoomID: 29, State: "working"}
	session := &model.LiveRuntimeSession{ID: 386, TenantID: 14, RoomID: 29, ExecutionRealm: "prod", Status: "running"}
	r := &liveResumeRecovery{Session: *session, Realm: "prod", HasProgram: true, Fence: liveResumeFence{ProgramID: program.ProgramID, VersionID: program.VersionID, ControlRevision: revision, BootID: agent.BootID, RunGeneration: agent.RunGeneration},
		ReadSession: func(context.Context) (model.LiveRuntimeSession, error) { return *session, nil },
		ReadAgent:   func(context.Context) (coreAgentRuntimeState, error) { return *agent, nil },
		ReadProgram: func(context.Context) (liveResumeProgram, error) { return *program, nil },
		Wait:        func(context.Context, time.Duration) error { return nil },
	}
	return r, program, agent, session
}

func TestLiveResumeLostAckReconcilesSameCursorWithoutSecondResume(t *testing.T) {
	r, program, _, _ := newResumeRecoveryFixture()
	calls := 0
	r.Resume = func(_ context.Context, f liveResumeFence) (liveResumeProgram, error) {
		calls++
		if f != r.Fence {
			t.Fatal("baseline changed")
		}
		*program.ManualPaused = false // Accepted once; only the response was lost.
		return liveResumeProgram{}, io.EOF
	}
	if err := r.run(context.Background()); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestLiveResumeTemporaryFailureRetainsFenceAndBackoff(t *testing.T) {
	r, program, _, _ := newResumeRecoveryFixture()
	calls := 0
	var delays []time.Duration
	r.Wait = func(_ context.Context, d time.Duration) error { delays = append(delays, d); return nil }
	r.Resume = func(_ context.Context, f liveResumeFence) (liveResumeProgram, error) {
		calls++
		if f != r.Fence {
			t.Fatal("baseline changed")
		}
		if calls < 3 {
			return liveResumeProgram{}, io.EOF
		}
		*program.ManualPaused = false
		return *program, nil
	}
	if err := r.run(context.Background()); err != nil || calls != 3 || !reflect.DeepEqual(delays, []time.Duration{time.Second, 2 * time.Second}) {
		t.Fatal(err, calls, delays)
	}
}

func TestLiveResumeManualStopOrReplacementCancelsPendingOperation(t *testing.T) {
	for _, change := range []string{"stop", "pause", "replacement", "realm", "program", "revision", "boot", "generation"} {
		t.Run(change, func(t *testing.T) {
			r, program, agent, session := newResumeRecoveryFixture()
			calls := 0
			r.Resume = func(context.Context, liveResumeFence) (liveResumeProgram, error) {
				calls++
				return liveResumeProgram{}, io.EOF
			}
			r.Wait = func(context.Context, time.Duration) error {
				switch change {
				case "stop":
					session.Status = "stopped"
				case "pause":
					session.Status = "paused"
				case "replacement":
					session.ID++
				case "realm":
					session.ExecutionRealm = "other"
				case "program":
					program.ProgramID = "new-program"
				case "revision":
					(*program.ControlRevision)++
				case "boot":
					agent.BootID = "new-boot"
				case "generation":
					agent.RunGeneration++
				}
				return nil
			}
			if err := r.run(context.Background()); !errors.Is(err, anchorcycle.ErrStale) || calls != 1 {
				t.Fatal(err, calls)
			}
		})
	}
}

func TestLiveResumeUnknownStartOnlyConfirmsAdjacentGeneration(t *testing.T) {
	r, program, agent, _ := newResumeRecoveryFixture()
	r.StartUnconfirmed = true
	r.PreviousGeneration = 2
	agent.RunGeneration = 2
	agent.State = "stopped"
	calls, waits := 0, 0
	r.Resume = func(context.Context, liveResumeFence) (liveResumeProgram, error) {
		calls++
		*program.ManualPaused = false
		return *program, nil
	}
	r.Wait = func(context.Context, time.Duration) error {
		waits++
		agent.RunGeneration = 3
		agent.State = "working"
		return nil
	}
	if err := r.run(context.Background()); err != nil || calls != 1 || waits != 1 {
		t.Fatal(err, calls, waits)
	}
	// A later manual start is a new run, not authority for this old worker.
	agent.RunGeneration = 4
	if err := r.attempt(context.Background()); !errors.Is(err, anchorcycle.ErrStale) {
		t.Fatal(err)
	}
}

func TestLiveResumeControlConflictCannotAdoptNewBaseline(t *testing.T) {
	r, _, _, _ := newResumeRecoveryFixture()
	calls := 0
	r.Resume = func(context.Context, liveResumeFence) (liveResumeProgram, error) {
		calls++
		return liveResumeProgram{}, anchorcycle.ErrStale
	}
	if err := r.run(context.Background()); !errors.Is(err, anchorcycle.ErrStale) || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestLiveResumeApplicationAndManualCancellationPreserveFormalWorker(t *testing.T) {
	app, cancelApp := context.WithCancel(context.Background())
	s := &Server{anchorProducerContext: app}
	formalCtx, formalCancel := context.WithCancel(app)
	defer formalCancel()
	s.cycleCancels.entries = map[string]cycleCancel{"formal-1": {tenant: 14, room: 29, cancel: formalCancel}}
	ctx, done := s.liveResumeRecoveryContext(14, 29)
	defer done()
	s.cancelLiveResumeRecovery(14, 29)
	if !errors.Is(ctx.Err(), context.Canceled) || formalCtx.Err() != nil {
		t.Fatal(ctx.Err(), formalCtx.Err())
	}
	ctx2, done2 := s.liveResumeRecoveryContext(14, 29)
	defer done2()
	cancelApp()
	if !errors.Is(ctx2.Err(), context.Canceled) {
		t.Fatal(ctx2.Err())
	}
}

func TestLiveResumeHTTPContractAndConflict(t *testing.T) {
	r, program, _, _ := newResumeRecoveryFixture()
	postCalls := 0
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(program)
			return
		}
		postCalls++
		var body liveResumeFence
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body != r.Fence {
			t.Error(err, body)
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"audio_resume_baseline_conflict","error":"恢复状态已变化"}`))
	}))
	defer h.Close()
	s := &Server{core: coreclient.New(h.URL, "")}
	p, err := s.resumeCoreRequest(context.Background(), 14, 29, http.MethodGet, "", nil)
	if err != nil || !p.matches(r.Fence, 386) {
		t.Fatal(err, p)
	}
	_, err = s.resumeCoreRequest(context.Background(), 14, 29, http.MethodPost, "/resume", r.Fence)
	if !errors.Is(err, anchorcycle.ErrStale) || postCalls != 1 {
		t.Fatal(err, postCalls)
	}
	w := httptest.NewRecorder()
	writeLiveResumePending(w, r.Session)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != http.StatusAccepted || body["recovery_pending"] != true || body["status"] != "running" {
		t.Fatal(err, w.Code, body)
	}
	if body["message"] != "恢复尚未确认，正在核对原场次和播放位置；如持续未恢复，请重新人工操作" {
		t.Fatal(body)
	}
}

func TestLiveResumeHTTPUnconfirmedAckUsesReadReconciliation(t *testing.T) {
	for _, reply := range []string{"null", `{"program_id":`} {
		t.Run(reply, func(t *testing.T) {
			r, program, _, _ := newResumeRecoveryFixture()
			posts := 0
			h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method == http.MethodGet {
					_ = json.NewEncoder(w).Encode(program)
					return
				}
				posts++
				*program.ManualPaused = false
				_, _ = w.Write([]byte(reply))
			}))
			defer h.Close()
			s := &Server{core: coreclient.New(h.URL, "")}
			r.ReadProgram = func(ctx context.Context) (liveResumeProgram, error) {
				return s.resumeCoreRequest(ctx, 14, 29, http.MethodGet, "", nil)
			}
			r.Resume = func(ctx context.Context, fence liveResumeFence) (liveResumeProgram, error) {
				return s.resumeCoreRequest(ctx, 14, 29, http.MethodPost, "/resume", fence)
			}
			if err := r.run(context.Background()); err != nil || posts != 1 {
				t.Fatal(err, posts)
			}
		})
	}
}
