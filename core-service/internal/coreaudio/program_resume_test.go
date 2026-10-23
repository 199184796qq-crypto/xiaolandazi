package coreaudio

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
)

func pausedResumeFixture(t *testing.T) (*Client, *programState, audioout.ProgramResumeGuard) {
	t.Helper()
	c, p, start, _ := sequentialFixture(t)
	paused, err := c.PauseProgram(context.Background(), start.RoomID)
	if err != nil {
		t.Fatal(err)
	}
	return c, p, audioout.ProgramResumeGuard{ExpectedProgramID: paused.ProgramID, ExpectedVersionID: paused.VersionID, ExpectedControlRevision: paused.ControlRevision, ExpectedBootID: "boot", ExpectedRunGeneration: 1}
}

func TestConditionalResumeFailureRetainsPreparedBoundaryTarget(t *testing.T) {
	c, p, guard := pausedResumeFixture(t)
	c.mu.Lock()
	p.ResumeOffsetMS = p.Tracks[0].DurationMS
	p.Tracks[1].DurationMS = 1000
	third := p.Tracks[1]
	third.ID = "C"
	p.Tracks = append(p.Tracks, third)
	c.mu.Unlock()
	_, err := c.hub.Publish(audiohub.Task{ID: "conflicting-active", RoomID: p.RoomID, AudioURL: p.Tracks[0].AudioURL, DurationMS: 60000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.ResumeProgramIfCurrent(context.Background(), p.RoomID, guard, func() bool { return true }); err == nil {
		t.Fatal("fixture did not reject the first publication")
	}
	c.hub.Expire("conflicting-active")
	resumed, err := c.ResumeProgramIfCurrent(context.Background(), p.RoomID, guard, func() bool { return true })
	if err != nil || resumed.TrackID != "B" || resumed.Task == nil || resumed.Task.StartMS != 0 || resumed.ManualPaused {
		t.Fatal("retry skipped a not-yet-played accepted track", resumed, err)
	}
	repeated, err := c.ResumeProgramIfCurrent(context.Background(), p.RoomID, guard, func() bool { return true })
	if err != nil || repeated.Sequence != resumed.Sequence || repeated.Task.ID != resumed.Task.ID {
		t.Fatal("ack retry republished resumed audio", repeated, err)
	}
}

func TestConditionalResumeRejectsChangedOwnerAndStop(t *testing.T) {
	for _, change := range []string{"program", "version", "revision", "runtime", "stop"} {
		t.Run(change, func(t *testing.T) {
			c, p, guard := pausedResumeFixture(t)
			allowed := true
			switch change {
			case "program":
				guard.ExpectedProgramID = "new-program"
			case "version":
				guard.ExpectedVersionID++
			case "revision":
				guard.ExpectedControlRevision++
			case "runtime":
				allowed = false
			case "stop":
				_, _ = c.StopProgram(context.Background(), p.RoomID)
			}
			_, err := c.ResumeProgramIfCurrent(context.Background(), p.RoomID, guard, func() bool { return allowed })
			if !errors.Is(err, ErrProgramResumeBaselineConflict) {
				t.Fatal("stale owner was allowed to restore sound", err)
			}
		})
	}
}

func TestConditionalResumeRechecksRuntimeAfterPublication(t *testing.T) {
	c, p, guard := pausedResumeFixture(t)
	var checks int
	_, err := c.ResumeProgramIfCurrent(context.Background(), p.RoomID, guard, func() bool {
		checks++
		return checks < 3 // HTTP admission and publish admission pass; commit fails.
	})
	if !errors.Is(err, ErrProgramResumeBaselineConflict) || checks != 3 {
		t.Fatal("a runtime change during publication was accepted", checks, err)
	}
	current, _ := c.ProgramSnapshot(context.Background(), p.RoomID)
	if !current.ManualPaused || current.Sequence != 1 {
		t.Fatal("late publication changed the paused program", current)
	}
	if _, err = c.ResumeProgramIfCurrent(context.Background(), p.RoomID, guard, func() bool { return true }); err != nil {
		t.Fatal("rejected publication left its hub task blocking recovery", err)
	}
}

func TestNewPauseAfterFailedBoundaryResumeDoesNotSkipPreparedTrack(t *testing.T) {
	c, p, guard := pausedResumeFixture(t)
	c.mu.Lock()
	p.ResumeOffsetMS = p.Tracks[0].DurationMS
	p.Tracks[1].DurationMS = 1000
	third := p.Tracks[1]
	third.ID = "C"
	p.Tracks = append(p.Tracks, third)
	c.mu.Unlock()
	_, err := c.hub.Publish(audiohub.Task{ID: "conflicting-active", RoomID: p.RoomID, AudioURL: p.Tracks[0].AudioURL, DurationMS: 60000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.ResumeProgramIfCurrent(context.Background(), p.RoomID, guard, func() bool { return true }); err == nil {
		t.Fatal("fixture did not fail publication")
	}
	paused, err := c.PauseProgram(context.Background(), p.RoomID)
	if err != nil || paused.ControlRevision == guard.ExpectedControlRevision {
		t.Fatal("new pause did not revoke the old resume", paused, err)
	}
	if _, err = c.ResumeProgramIfCurrent(context.Background(), p.RoomID, guard, func() bool { return true }); !errors.Is(err, ErrProgramResumeBaselineConflict) {
		t.Fatal("revoked request was allowed", err)
	}
	guard.ExpectedControlRevision = paused.ControlRevision
	c.hub.Expire("conflicting-active")
	resumed, err := c.ResumeProgramIfCurrent(context.Background(), p.RoomID, guard, func() bool { return true })
	if err != nil || resumed.TrackID != "B" || resumed.Task.StartMS != 0 {
		t.Fatal("new resume skipped the not-yet-played stable target", resumed, err)
	}
}

func TestConditionalResumeRefreshRetryKeepsItsNewTarget(t *testing.T) {
	c, p, guard := pausedResumeFixture(t)
	c.mu.Lock()
	p.TrackIndex = len(p.Tracks) - 1
	p.ResumeOffsetMS = p.Tracks[p.TrackIndex].DurationMS
	newTrack := p.Tracks[0]
	newTrack.ID = "new-generation-target"
	p.PendingRefresh = &programRefresh{JobID: "pending-refresh", Generation: 1, VersionID: guard.ExpectedVersionID + 1, VersionNo: 2, Tracks: []programTrack{newTrack}, ProtectedNextIndex: -1, ValidUntil: time.Now().Add(time.Minute)}
	c.mu.Unlock()
	_, err := c.hub.Publish(audiohub.Task{ID: "conflicting-active", RoomID: p.RoomID, AudioURL: newTrack.AudioURL, DurationMS: 60000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.ResumeProgramIfCurrent(context.Background(), p.RoomID, guard, func() bool { return true }); err == nil {
		t.Fatal("fixture did not fail the first refresh publish")
	}
	c.hub.Expire("conflicting-active")
	resumed, err := c.ResumeProgramIfCurrent(context.Background(), p.RoomID, guard, func() bool { return true })
	if err != nil || resumed.TrackID != newTrack.ID || resumed.Generation != 1 || resumed.VersionID != guard.ExpectedVersionID+1 {
		t.Fatal("failed refresh resume replayed consumed old content", resumed, err)
	}
}

func TestConditionalResumeConcurrentWaiterAndNewPauseFence(t *testing.T) {
	for _, pause := range []bool{false, true} {
		t.Run(map[bool]string{false: "duplicate", true: "new-pause"}[pause], func(t *testing.T) {
			c, p, guard := pausedResumeFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var first atomic.Bool
			c.now = func() time.Time {
				if first.CompareAndSwap(false, true) {
					close(entered)
					<-release
				}
				return time.Now()
			}
			result := make(chan error, 1)
			go func() {
				_, err := c.ResumeProgramIfCurrent(context.Background(), p.RoomID, guard, func() bool { return true })
				result <- err
			}()
			<-entered
			if pause {
				if _, err := c.PauseProgram(context.Background(), p.RoomID); err != nil {
					t.Fatal(err)
				}
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				defer cancel()
				if _, err := c.ResumeProgramIfCurrent(ctx, p.RoomID, guard, func() bool { return true }); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("second request did not wait for its existing owner", err)
				}
			}
			close(release)
			err := <-result
			if pause && !errors.Is(err, ErrProgramResumeBaselineConflict) || !pause && err != nil {
				t.Fatal("publication crossed a pause or failed valid resume", err)
			}
			current, _ := c.ProgramSnapshot(context.Background(), p.RoomID)
			if pause && !current.ManualPaused || !pause && current.ManualPaused {
				t.Fatal(current)
			}
		})
	}
}

func TestConditionalResumePanicReleasesWaitersWithoutSuccess(t *testing.T) {
	c, p, guard := pausedResumeFixture(t)
	var fail atomic.Bool
	oldClock := c.now
	c.now = func() time.Time {
		if fail.CompareAndSwap(false, true) {
			panic("injected publication preparation failure")
		}
		return oldClock()
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("fixture did not panic")
			}
		}()
		_, _ = c.ResumeProgramIfCurrent(context.Background(), p.RoomID, guard, func() bool { return true })
	}()
	c.mu.RLock()
	op := p.ManualResume
	paused := p.ManualPaused
	c.mu.RUnlock()
	if op == nil || op.inFlight || !paused {
		t.Fatal("panic left an owner stuck or manufactured success")
	}
	select {
	case <-op.done:
	default:
		t.Fatal("panic did not release concurrent waiters")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if resumed, err := c.ResumeProgramIfCurrent(ctx, p.RoomID, guard, func() bool { return true }); err != nil || resumed.ManualPaused {
		t.Fatal("panic poisoned later same-target recovery", resumed, err)
	}
}
