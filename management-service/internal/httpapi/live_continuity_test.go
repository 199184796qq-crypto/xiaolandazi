package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
)

func TestLiveContinuityRetryRetainsOperationAfterPanicAndBacksOff(t *testing.T) {
	calls := 0
	var delays []time.Duration
	err := retryLiveOperation(context.Background(), "test", 0, func(_ context.Context, d time.Duration) error { delays = append(delays, d); return nil }, func() error {
		calls++
		if calls == 1 {
			return io.EOF
		}
		if calls == 2 {
			panic("injected before committing")
		}
		return nil
	})
	if err != nil || calls != 3 || !reflect.DeepEqual(delays, []time.Duration{time.Second, 2 * time.Second}) {
		t.Fatal(err, calls, delays)
	}
	if liveContinuityDelay(100) != 16*time.Second {
		t.Fatal("backoff not capped")
	}
}

func TestLiveContinuityStopWinsOverRetry(t *testing.T) {
	for _, ended := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		err := retryLiveOperation(ctx, "test", 0, func(context.Context, time.Duration) error { cancel(); return ctx.Err() }, func() error {
			calls++
			if ended {
				return anchorcycle.ErrStale
			}
			return io.EOF
		})
		cancel()
		if calls != 1 || (ended && !errors.Is(err, anchorcycle.ErrStale)) || (!ended && !errors.Is(err, context.Canceled)) {
			t.Fatal(ended, calls, err)
		}
	}
}

func TestLiveContinuityUnknownLeaseBlocksWorkUntilVerified(t *testing.T) {
	var permit atomic.Int64
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), liveLeasePermitKey{}, &permit))
	defer cancel()
	called := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- retryLiveOperation(ctx, "test", 0, nil, func() error { called <- struct{}{}; return nil })
	}()
	select {
	case <-called:
		t.Fatal("unverified owner performed work")
	case <-time.After(80 * time.Millisecond):
	}
	permit.Store(time.Now().Add(time.Second).UnixNano())
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("same owner did not resume")
	}
	permit.Store(0)
	cancel()
	if err := waitLiveLeasePermit(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRecordingAppendRetriesExactReadyUnitAfterLostOrInvalidAck(t *testing.T) {
	for _, fault := range []string{"eof", "truncated", "empty", "server"} {
		t.Run(fault, func(t *testing.T) {
			attempts, commits := 0, 0
			var original []coreclient.ProgramRefreshTrack
			snapshot := coreclient.ProgramRefreshSnapshot{ProgramID: "same-program", VersionID: 208, Running: true, Sequential: true}
			h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_ = json.NewEncoder(w).Encode(snapshot)
					return
				}
				var body struct {
					ExpectedProgramID string                           `json:"expected_program_id"`
					ExpectedVersionID int64                            `json:"expected_version_id"`
					Tracks            []coreclient.ProgramRefreshTrack `json:"tracks"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.ExpectedProgramID != "same-program" || body.ExpectedVersionID != 208 {
					t.Error("owner changed")
				}
				attempts++
				if attempts == 1 {
					original = body.Tracks
					commits++
				} else if !reflect.DeepEqual(original, body.Tracks) {
					t.Error("resend regenerated the ready unit or its ID")
				}
				if attempts == 1 {
					switch fault {
					case "eof":
						conn, _, _ := w.(http.Hijacker).Hijack()
						_ = conn.Close()
					case "truncated":
						_, _ = io.WriteString(w, `{"program_id":`)
					case "empty":
						_, _ = io.WriteString(w, `{}`)
					case "server":
						w.WriteHeader(500)
						_, _ = io.WriteString(w, `{"error":"temporary fault"}`)
					}
					return
				}
				_ = json.NewEncoder(w).Encode(snapshot)
			}))
			defer h.Close()
			s := &Server{core: coreclient.New(h.URL, "test")}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			track := coreclient.ProgramRefreshTrack{ID: "ready-unit-once", Text: "已审核正文。", AudioURL: "https://media.invalid/ready.wav", DurationMS: 1000}
			generation, err := s.appendRecordingStreamTrack(ctx, 14, 29, model.LiveAgentPlanVersion{ID: 208}, "same-program", track)
			if err != nil || generation != 0 || attempts != 2 || commits != 1 {
				t.Fatal(err, generation, attempts, commits)
			}
		})
	}
}

func TestRecordingAppendNeverRebindsReplacementProgram(t *testing.T) {
	posts := 0
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			w.WriteHeader(409)
			return
		}
		_, _ = io.WriteString(w, `{"running":true,"program_id":"replacement","version_id":208}`)
	}))
	defer h.Close()
	s := &Server{core: coreclient.New(h.URL, "test")}
	_, err := s.appendRecordingStreamTrack(context.Background(), 14, 29, model.LiveAgentPlanVersion{ID: 208}, "old", coreclient.ProgramRefreshTrack{ID: "old-unit"})
	if !errors.Is(err, anchorcycle.ErrStale) || posts != 1 {
		t.Fatal(err, posts)
	}
}

func TestReadyProgramLostStartAckReconcilesWithoutSecondStart(t *testing.T) {
	posts, running := 0, false
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			running = true
			_, _ = io.WriteString(w, `{"program_id":`)
			return
		}
		if running {
			_, _ = io.WriteString(w, `{"running":true,"sequential":true,"program_id":"started-once","version_id":208,"task":{"session_id":"recording-stream-476"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"running":false}`)
	}))
	defer h.Close()
	s := &Server{core: coreclient.New(h.URL, "test")}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snapshot, err := s.startRecordingStreamProgram(ctx, 14, 29, 476, model.LiveAgentPlanVersion{ID: 208}, coreclient.ProgramRefreshTrack{ID: "first"})
	if err != nil || snapshot.ProgramID != "started-once" || posts != 1 {
		t.Fatal(err, snapshot, posts)
	}
}

func TestReadyProgramCannotClaimRacingSameVersionSession(t *testing.T) {
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"running":false}`)
			return
		}
		_, _ = io.WriteString(w, `{"running":true,"sequential":true,"program_id":"new-session","version_id":208,"task":{"session_id":"recording-stream-477"}}`)
	}))
	defer h.Close()
	s := &Server{core: coreclient.New(h.URL, "test")}
	_, err := s.startRecordingStreamProgram(context.Background(), 14, 29, 476, model.LiveAgentPlanVersion{ID: 208}, coreclient.ProgramRefreshTrack{ID: "old"})
	if !errors.Is(err, anchorcycle.ErrStale) {
		t.Fatal(err)
	}
}

func TestReadyProgramExplicitCancelledStartCannotReviveSession(t *testing.T) {
	posts := 0
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"running":false}`)
			return
		}
		posts++
		w.WriteHeader(409)
		_, _ = io.WriteString(w, `{"error":"启动已取消","code":"audio_start_cancelled","stage":"播放服务","retryable":false}`)
	}))
	defer h.Close()
	s := &Server{core: coreclient.New(h.URL, "test")}
	_, err := s.startRecordingStreamProgram(context.Background(), 14, 29, 476, model.LiveAgentPlanVersion{ID: 208}, coreclient.ProgramRefreshTrack{ID: "old"})
	if !errors.Is(err, anchorcycle.ErrStale) || posts != 1 {
		t.Fatal(err, posts)
	}
}

func TestFormalSupplyReadyVoiceSurvivesWorkerRetryWithoutTTS(t *testing.T) {
	p, repo := deliveryFailureFixture(t)
	voice := &anchorcycle.VoiceAsset{Status: "ready", Bundle: model.LiveAgentPlanVersionVariant{Text: "已审核音文一致。", AudioDurationMS: 1000}}
	repo.state.Accepted[0].Voice = voice
	var tts atomic.Int32
	p.synthesize = func(context.Context, *anchorcycle.State, string) (*anchorcycle.VoiceAsset, error) {
		tts.Add(1)
		return nil, errors.New("should not synthesize ready voice")
	}
	delivered := make(chan string, 1)
	p.onReady = func(_ context.Context, _ *anchorcycle.State, seg anchorcycle.Segment) error {
		delivered <- seg.Selected.ID
		return nil
	}
	done := make(chan struct{})
	go func() { defer close(done); p.voiceLoop() }()
	select {
	case id := <-delivered:
		if id != "failed" {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Fatal("ready undelivered voice was lost")
	}
	p.stop()
	<-done
	if tts.Load() != 0 {
		t.Fatal("TTS repeated", tts.Load())
	}
}

func TestFormalSupplyTechnicalFailureDoesNotCancelSession(t *testing.T) {
	p, _ := deliveryFailureFixture(t)
	p.fail(io.EOF)
	if p.ctx.Err() != nil || p.failure != "" || p.paused {
		t.Fatal("technical failure stopped session")
	}
	p.fail(anchorcycle.ErrStale)
	if p.ctx.Err() == nil {
		t.Fatal("replacement owner did not cancel old producer")
	}
}

func TestFormalSupplyWorkerPanicResumesSameRunAndClearsReservation(t *testing.T) {
	p, _ := deliveryFailureFixture(t)
	var calls atomic.Int32
	resumed := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.superviseWorker("文字", func() {
			if calls.Add(1) == 1 {
				p.mu.Lock()
				p.reserved = 90000
				p.mu.Unlock()
				panic("injected model worker panic")
			}
			p.mu.Lock()
			reserved := p.reserved
			p.mu.Unlock()
			if reserved != 0 {
				t.Error("abandoned reservation blocked refill", reserved)
			}
			resumed <- struct{}{}
			p.stop()
		})
	}()
	select {
	case <-resumed:
	case <-time.After(2 * time.Second):
		t.Fatal("worker never resumed")
	}
	<-done
	if calls.Load() != 2 || p.initial.RunID != "delivery-test" {
		t.Fatal("worker restarted another run", calls.Load(), p.initial.RunID)
	}
}

func TestStartupReadinessDoesNotSliceOneOrTwoReadyUnits(t *testing.T) {
	for _, count := range []int{1, 2} {
		units := make([]recordingReadyUnit, count)
		for i := range units {
			units[i].Track.DurationMS = 10000
		}
		if recordingStartReady(units, time.Now()) {
			t.Fatal("short buffer incorrectly passed startup gate")
		}
		if take := min(3, len(units)); len(toRecordingCacheEntries(units[:take])) != count {
			t.Fatal("short ready queue was not preserved")
		}
	}
}

func TestRecordingAppendRejectsBadUnitAndAllowsFollowingReadyUnit(t *testing.T) {
	var seen []string
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tracks []coreclient.ProgramRefreshTrack `json:"tracks"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen = append(seen, body.Tracks[0].ID)
		if body.Tracks[0].ID == "bad" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":"invalid safe point"}`)
			return
		}
		_, _ = io.WriteString(w, `{"running":true,"sequential":true,"program_id":"same","version_id":208}`)
	}))
	defer h.Close()
	s := &Server{core: coreclient.New(h.URL, "test")}
	_, err := s.appendRecordingStreamTrack(context.Background(), 14, 29, model.LiveAgentPlanVersion{ID: 208}, "same", coreclient.ProgramRefreshTrack{ID: "bad"})
	var rejected *mainlineNotSubmitted
	if !errors.As(err, &rejected) {
		t.Fatal("bad unit was not isolated", err)
	}
	_, err = s.appendRecordingStreamTrack(context.Background(), 14, 29, model.LiveAgentPlanVersion{ID: 208}, "same", coreclient.ProgramRefreshTrack{ID: "good"})
	if err != nil || !reflect.DeepEqual(seen, []string{"bad", "good"}) {
		t.Fatal(err, seen)
	}
}

func TestRecordingAppendLaterRejectionCannotRefundUnknownEarlierAttempt(t *testing.T) {
	attempts := 0
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"running":true,"program_id":"same","version_id":208}`)
			return
		}
		attempts++
		switch attempts {
		case 1:
			_, _ = io.WriteString(w, `{"program_id":`)
		case 2:
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":"音频地址暂时不可用","code":"audio_unit_not_accepted","retryable":false}`)
		default:
			_, _ = io.WriteString(w, `{"running":true,"program_id":"same","version_id":208}`)
		}
	}))
	defer h.Close()
	s := &Server{core: coreclient.New(h.URL, "test")}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	_, err := s.appendRecordingStreamTrack(ctx, 14, 29, model.LiveAgentPlanVersion{ID: 208}, "same", coreclient.ProgramRefreshTrack{ID: "may-have-been-accepted"})
	if err != nil || attempts != 3 {
		t.Fatal("unknown outcome incorrectly isolated/refunded", err, attempts)
	}
}
