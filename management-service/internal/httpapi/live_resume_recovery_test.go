package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
)

func newResumeRecoveryFixture() (*liveResumeRecovery, *liveResumeProgram, *coreAgentRuntimeState, *model.LiveRuntimeSession) {
	paused, revision := true, uint64(7)
	program := &liveResumeProgram{ProgramID: "program-386", VersionID: 208, SessionID: "recording-stream-386", Running: true, ManualPaused: &paused, ControlRevision: &revision}
	agentRevision := uint64(11)
	agent := &coreAgentRuntimeState{BootID: "boot-1", RunGeneration: 3, RoomID: 29, State: "working", ControlRevision: &agentRevision}
	session := &model.LiveRuntimeSession{ID: 386, TenantID: 14, RoomID: 29, ExecutionRealm: "prod", Status: "running"}
	r := &liveResumeRecovery{Session: *session, Realm: "prod", HasProgram: true, Fence: liveResumeFence{ProgramID: program.ProgramID, VersionID: program.VersionID, ControlRevision: revision, BootID: agent.BootID, RunGeneration: agent.RunGeneration},
		AgentBaseline: liveResumeAgentFence{BootID: "boot-1", RunGeneration: 2, ControlRevision: 10}, AgentControlRevision: 11,
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
	for _, change := range []string{"stop", "pause", "replacement", "realm", "program", "revision", "boot", "generation", "agent-revision"} {
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
				case "agent-revision":
					(*agent.ControlRevision)++
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
	agent.RunGeneration = 2
	agent.State = "stopped"
	*agent.ControlRevision = 10
	calls, waits, starts := 0, 0, 0
	r.Start = func(_ context.Context, baseline liveResumeAgentFence) (coreAgentRuntimeState, error) {
		starts++
		if baseline != r.AgentBaseline {
			t.Fatal("start intent baseline changed", baseline)
		}
		if starts == 1 {
			return coreAgentRuntimeState{}, io.EOF
		}
		agent.RunGeneration = 3
		*agent.ControlRevision = 11
		agent.State = "working"
		return *agent, nil
	}
	r.Resume = func(context.Context, liveResumeFence) (liveResumeProgram, error) {
		calls++
		*program.ManualPaused = false
		return *program, nil
	}
	r.Wait = func(context.Context, time.Duration) error {
		waits++
		return nil
	}
	if err := r.run(context.Background()); err != nil || calls != 1 || waits != 1 || starts != 2 {
		t.Fatal(err, calls, waits, starts)
	}
	// A later manual start is a new run, not authority for this old worker.
	agent.RunGeneration = 4
	if err := r.attempt(context.Background()); !errors.Is(err, anchorcycle.ErrStale) {
		t.Fatal(err)
	}
}

func TestLiveResumeUnknownStartAcceptedAckNeverStartsAgain(t *testing.T) {
	r, program, _, _ := newResumeRecoveryFixture()
	r.StartUnconfirmed = true
	r.Start = func(context.Context, liveResumeAgentFence) (coreAgentRuntimeState, error) {
		t.Fatal("already accepted start replayed")
		return coreAgentRuntimeState{}, nil
	}
	r.Resume = func(context.Context, liveResumeFence) (liveResumeProgram, error) {
		*program.ManualPaused = false
		return *program, nil
	}
	if err := r.run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLiveResumeNewManualIntentPreventsUnknownStartRetry(t *testing.T) {
	r, _, agent, _ := newResumeRecoveryFixture()
	r.StartUnconfirmed = true
	agent.State = "stopped"
	agent.RunGeneration = 2
	*agent.ControlRevision = 11 // New manual stop, although the generation did not change.
	r.Start = func(context.Context, liveResumeAgentFence) (coreAgentRuntimeState, error) {
		t.Fatal("new manual stop ignored")
		return coreAgentRuntimeState{}, nil
	}
	if err := r.run(context.Background()); !errors.Is(err, anchorcycle.ErrStale) {
		t.Fatal(err)
	}
}

func TestLiveResumeGuardConflictReconcilesOnlyExactAcceptedStart(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual-stop", true: "accepted-after-read"}[accepted], func(t *testing.T) {
			r, program, agent, _ := newResumeRecoveryFixture()
			r.StartUnconfirmed = true
			agent.State = "stopped"
			agent.RunGeneration = 2
			*agent.ControlRevision = 10
			starts, audio := 0, 0
			r.Start = func(context.Context, liveResumeAgentFence) (coreAgentRuntimeState, error) {
				starts++
				*agent.ControlRevision = 11
				if accepted {
					agent.State = "working"
					agent.RunGeneration = 3
				}
				return coreAgentRuntimeState{}, anchorcycle.ErrStale
			}
			r.Resume = func(context.Context, liveResumeFence) (liveResumeProgram, error) {
				audio++
				*program.ManualPaused = false
				return *program, nil
			}
			err := r.run(context.Background())
			if starts != 1 || (accepted && (err != nil || audio != 1)) || (!accepted && (!errors.Is(err, anchorcycle.ErrStale) || audio != 0)) {
				t.Fatal(err, starts, audio)
			}
		})
	}
}

func TestLiveResumeConditionalAgentStartHTTPContractAndConflict(t *testing.T) {
	baseline := liveResumeAgentFence{BootID: "boot-1", RunGeneration: 2, ControlRevision: 10}
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			liveResumeAgentFence
			Command            string `json:"command"`
			BaseWorkingSeconds uint64 `json:"base_working_seconds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.liveResumeAgentFence != baseline || body.Command != "start" || body.BaseWorkingSeconds != 123 {
			t.Error(err, body)
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"agent_start_baseline_conflict","error":"恢复操作已被新的操作替代"}`))
	}))
	defer h.Close()
	s := &Server{core: coreclient.New(h.URL, "")}
	if _, err := s.startLiveResumeAgent(context.Background(), 14, 29, baseline, 123); !errors.Is(err, anchorcycle.ErrStale) {
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
	ctx, done := s.liveResumeRecoveryContext(14, 29, 386, 1)
	defer done()
	s.cancelLiveResumeRecovery(14, 29)
	if !errors.Is(ctx.Err(), context.Canceled) || formalCtx.Err() != nil {
		t.Fatal(ctx.Err(), formalCtx.Err())
	}
	ctx2, done2 := s.liveResumeRecoveryContext(14, 29, 386, 2)
	defer done2()
	cancelApp()
	if !errors.Is(ctx2.Err(), context.Canceled) {
		t.Fatal(ctx2.Err())
	}
}

func TestLiveResumeConcurrentRegistrationKeepsLatestDurableVersion(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		s := &Server{anchorProducerContext: context.Background()}
		var contexts [2]context.Context
		var dones [2]func()
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range contexts {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				<-start
				contexts[index], dones[index] = s.liveResumeRecoveryContext(14, 29, 386, uint64(index+1))
			}(i)
		}
		close(start)
		wg.Wait()
		if contexts[0].Err() == nil || contexts[1].Err() != nil {
			t.Fatal("older registration cancelled the newer owner", contexts[0].Err(), contexts[1].Err())
		}
		late, lateDone := s.liveResumeRecoveryContext(14, 29, 386, 1)
		if late.Err() == nil || contexts[1].Err() != nil {
			t.Fatal("late old version cancelled new worker")
		}
		lateDone()
		newSession, newDone := s.liveResumeRecoveryContext(14, 29, 387, 1)
		if newSession.Err() != nil || contexts[1].Err() == nil {
			t.Fatal("new session did not supersede prior session")
		}
		for _, done := range dones {
			done()
		}
		newDone()
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
