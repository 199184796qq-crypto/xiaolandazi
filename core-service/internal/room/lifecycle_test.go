package room

import (
	"context"
	"errors"
	"livecompanion/core/internal/model"
	"sync/atomic"
	"testing"
	"time"
)

type lifecycleFixture struct {
	manager  *LifecycleManager
	room     model.Room
	reporter CollectorReporter
	clock    atomic.Int64
	ended    atomic.Int64
}

func newLifecycleFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	f := &lifecycleFixture{room: model.Room{ID: 27, TenantID: 14, Status: "live", MonitorEnabled: true}}
	f.clock.Store(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC).UnixNano())
	f.manager = newLifecycleManager(nil)
	f.manager.now = func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }
	f.manager.SetEndObserver(func(room model.Room, reason string) {
		// Real observers query the authority. This must not deadlock.
		if status := f.manager.Snapshot(room).Status; status != "offline" && status != "stopped" {
			t.Errorf("observer saw %s", status)
		}
		f.ended.Add(1)
	})
	var err error
	f.reporter, err = f.manager.BeginCollection(context.Background(), f.room)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.manager.Close)
	return f
}
func (f *lifecycleFixture) advance(d time.Duration) { f.clock.Add(int64(d)) }
func (f *lifecycleFixture) report(t *testing.T, kind string) LifecycleSnapshot {
	t.Helper()
	state, err := f.manager.report(context.Background(), f.reporter, kind)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestLifecycleOneAbsoluteSixtySecondDeadline(t *testing.T) {
	f := newLifecycleFixture(t)
	first := f.report(t, "disconnected")
	for _, delay := range []time.Duration{20 * time.Second, 20 * time.Second, 19 * time.Second} {
		f.advance(delay)
		f.report(t, "connecting")
		again := f.report(t, "disconnected")
		if again.Status != "live" || !again.Deadline.Equal(*first.Deadline) {
			t.Fatalf("retry changed status/deadline: %+v", again)
		}
	}
	if err := f.manager.expire(f.reporter); err != nil {
		t.Fatal(err)
	}
	if f.ended.Load() != 0 {
		t.Fatal("59 seconds ended broadcast")
	}
	f.advance(time.Second)
	// No collector runner return is needed for deadline expiry.
	if err := f.manager.expire(f.reporter); err != nil {
		t.Fatal(err)
	}
	state := f.manager.Snapshot(f.room)
	if state.Status != "offline" || state.EndRevision != 1 || f.ended.Load() != 1 {
		t.Fatal(state)
	}
	f.report(t, "ended")
	if f.ended.Load() != 1 {
		t.Fatal("duplicate terminal event")
	}
}

func TestLifecycleRecoveryResetsOnlyAfterLiveEvidence(t *testing.T) {
	f := newLifecycleFixture(t)
	f.report(t, "disconnected")
	f.advance(55 * time.Second)
	state := f.report(t, "live")
	if state.Deadline != nil || state.Collection != "connected" {
		t.Fatal(state)
	}
	if err := f.manager.expire(f.reporter); !errors.Is(err, ErrCollectorReplaced) {
		t.Fatal("old timer accepted", err)
	}
	f.advance(time.Second)
	second := f.report(t, "disconnected")
	f.advance(59 * time.Second)
	if err := f.manager.expire(f.reporter); err != nil {
		t.Fatal(err)
	}
	if f.ended.Load() != 0 || second.Deadline == nil {
		t.Fatal("new disconnect ended too soon")
	}
	f.advance(time.Second)
	if err := f.manager.expire(f.reporter); err != nil {
		t.Fatal(err)
	}
	if f.ended.Load() != 1 {
		t.Fatal("new disconnect did not expire")
	}
}

func TestLifecycleReplacementPreservesDeadlineAndFencesLateWorker(t *testing.T) {
	f := newLifecycleFixture(t)
	first := f.report(t, "disconnected")
	old := f.reporter
	f.advance(40 * time.Second)
	var err error
	f.reporter, err = f.manager.BeginCollection(context.Background(), f.room)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.manager.report(context.Background(), old, "live"); !errors.Is(err, ErrCollectorReplaced) {
		t.Fatal("old worker revived room")
	}
	if err = f.manager.expire(old); !errors.Is(err, ErrCollectorReplaced) {
		t.Fatal("old timer authoritative")
	}
	if next := f.manager.Snapshot(f.room); !next.Deadline.Equal(*first.Deadline) {
		t.Fatal("replacement added grace")
	}
	f.advance(20 * time.Second)
	if err = f.manager.expire(f.reporter); err != nil {
		t.Fatal(err)
	}
	if f.ended.Load() != 1 {
		t.Fatal("original deadline lost")
	}
}

func TestLifecycleManualStopImmediateAndLateEvidenceRejected(t *testing.T) {
	f := newLifecycleFixture(t)
	f.report(t, "disconnected")
	update := func(status string) error {
		if status != "stopped" {
			t.Fatal(status)
		}
		return nil
	}
	if err := f.manager.changeMonitoring(context.Background(), f.room, false, update); err != nil {
		t.Fatal(err)
	}
	if f.ended.Load() != 1 || f.manager.Snapshot(f.room).Status != "stopped" {
		t.Fatal("manual stop delayed")
	}
	if _, err := f.manager.report(context.Background(), f.reporter, "live"); !errors.Is(err, ErrCollectorReplaced) {
		t.Fatal("late live accepted")
	}
	if err := f.manager.expire(f.reporter); !errors.Is(err, ErrCollectorReplaced) {
		t.Fatal("late timeout accepted")
	}
	if err := f.manager.changeMonitoring(context.Background(), f.room, false, update); err != nil {
		t.Fatal(err)
	}
	if f.ended.Load() != 1 {
		t.Fatal("duplicate manual stop")
	}
}

func TestLifecycleExplicitEndImmediateAndRecoveryRetainsEndRevision(t *testing.T) {
	f := newLifecycleFixture(t)
	f.report(t, "ended")
	if f.ended.Load() != 1 || f.manager.Snapshot(f.room).Status != "offline" {
		t.Fatal("explicit end waited")
	}
	recovered := f.report(t, "live")
	if recovered.Status != "live" || recovered.EndRevision != 1 {
		t.Fatal(recovered)
	}
	// EndRevision triggers session_reopen, not automatic paid-agent startup.
}

func TestLifecyclePersistenceFailureDoesNotAnnounceOrExtendEnd(t *testing.T) {
	f := newLifecycleFixture(t)
	first := f.report(t, "disconnected")
	f.manager.persist = func(context.Context, int64, int64, string) error { return errors.New("database unavailable") }
	f.advance(CollectionGrace)
	if err := f.manager.expire(f.reporter); err == nil {
		t.Fatal("write failure ignored")
	}
	state := f.manager.Snapshot(f.room)
	if state.Status != "live" || f.ended.Load() != 0 || !state.Deadline.Equal(*first.Deadline) {
		t.Fatal(state)
	}
	f.manager.persist = nil
	if err := f.manager.expire(f.reporter); err != nil {
		t.Fatal(err)
	}
	if f.ended.Load() != 1 {
		t.Fatal("retry did not end")
	}
}

func TestLifecycleCaptureBeforeTerminalNotificationAndReadableObserver(t *testing.T) {
	f := newLifecycleFixture(t)
	entered, release, captured, ended := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	f.manager.SetEndObserver(func(room model.Room, reason string) {
		_ = f.manager.Snapshot(room)
		select {
		case <-captured:
		default:
			t.Error("session_end overtook capture")
		}
		close(ended)
	})
	go func() {
		_, _ = f.manager.reportAndApply(context.Background(), f.reporter, "live", func(LifecycleSnapshot) error {
			close(entered)
			<-release
			close(captured)
			return nil
		})
	}()
	<-entered
	go func() { _, _ = f.manager.report(context.Background(), f.reporter, "ended") }()
	close(release)
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("observer/status deadlock")
	}
}

func TestLifecycleCloseDeleteAndLeaseLossInvalidateWorkers(t *testing.T) {
	for _, action := range []string{"close", "delete", "lease_lost"} {
		t.Run(action, func(t *testing.T) {
			f := newLifecycleFixture(t)
			f.report(t, "disconnected")
			switch action {
			case "close":
				f.manager.Close()
			case "delete":
				f.manager.Forget(&f.room.TenantID, f.room.ID)
			case "lease_lost":
				f.manager.ReleaseCollection(f.reporter)
			}
			if _, err := f.manager.report(context.Background(), f.reporter, "live"); !errors.Is(err, ErrCollectorReplaced) {
				t.Fatal(err)
			}
			f.advance(CollectionGrace)
			if err := f.manager.expire(f.reporter); !errors.Is(err, ErrCollectorReplaced) {
				t.Fatal(err)
			}
			if f.ended.Load() != 0 {
				t.Fatal("shutdown/delete/lease loss invented end")
			}
			if action != "lease_lost" {
				if _, err := f.manager.BeginCollection(context.Background(), f.room); !errors.Is(err, ErrCollectorReplaced) {
					t.Fatal("retired worker restarted")
				}
			}
		})
	}
}

func TestLifecycleTenantIsolationAndNonOwnerReads(t *testing.T) {
	f := newLifecycleFixture(t)
	f.report(t, "disconnected")
	other := f.room
	other.TenantID = 99
	other.Status = "pending"
	if state := f.manager.view(other, true); state.Status != "pending" || state.Deadline != nil {
		t.Fatal("tenant leak")
	}
	other.Status = "live"
	if state := f.manager.view(other, true); state.Status != "live" {
		t.Fatal("non-owner first-read cache stale")
	}
	other.Status = "offline"
	if state := f.manager.view(other, true); state.Status != "offline" {
		t.Fatal("non-owner missed end")
	}
}

func TestDevelopmentInjectionDoesNotReplaceRealWorker(t *testing.T) {
	f := newLifecycleFixture(t)
	store := &Store{lifecycle: f.manager}
	if err := store.ReportDevelopmentLive(context.Background(), f.room); err != nil {
		t.Fatal(err)
	}
	f.report(t, "live")
}
