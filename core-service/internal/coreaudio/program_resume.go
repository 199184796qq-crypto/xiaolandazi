package coreaudio

import (
	"context"
	"errors"
	"time"

	"livecompanion/core/internal/audioout"
)

var ErrProgramResumeBaselineConflict = errors.New("本次声音恢复已被新的暂停、停止或场次替代")

// A failed publication retains its exact prepared target. In particular, an
// end-of-track resume must not advance the queue again on every retry.
type manualProgramResume struct {
	revision       uint64
	position       programTrackPosition
	offsetMS       int
	sequence       uint64
	refresh        *programRefresh
	inFlight       bool
	done           chan struct{}
	runtimeAllowed func() bool
}

func manualResumeOwnedLocked(program *programState, resume *manualProgramResume) bool {
	return program.ManualResume == resume && program.Running && program.ManualPaused && program.Suspended && program.ControlRevision == resume.revision && (resume.runtimeAllowed == nil || resume.runtimeAllowed())
}

// The runtime callback is read-only and is checked inside the program lock both
// before and after publication. It must not call back into the audio client.
func (c *Client) ResumeProgramIfCurrent(ctx context.Context, roomID int64, guard audioout.ProgramResumeGuard, runtimeAllowed func() bool) (audioout.RoomProgramSnapshot, error) {
	if guard.ExpectedProgramID == "" || guard.ExpectedVersionID <= 0 || guard.ExpectedBootID == "" || guard.ExpectedRunGeneration == 0 || runtimeAllowed == nil {
		return audioout.RoomProgramSnapshot{}, ErrProgramResumeBaselineConflict
	}
	return c.resumeProgram(ctx, roomID, &guard, runtimeAllowed)
}

func (c *Client) resumeProgram(ctx context.Context, roomID int64, guard *audioout.ProgramResumeGuard, runtimeAllowed func() bool) (audioout.RoomProgramSnapshot, error) {
	if roomID <= 0 {
		return audioout.RoomProgramSnapshot{}, errors.New("room_id is required")
	}
	for {
		if err := ctx.Err(); err != nil {
			return audioout.RoomProgramSnapshot{}, err
		}
		c.mu.Lock()
		program := c.programs[roomID]
		if guard != nil && (program == nil || !program.Running || program.ID != guard.ExpectedProgramID || program.VersionID != guard.ExpectedVersionID || program.ControlRevision != guard.ExpectedControlRevision || !runtimeAllowed()) {
			c.mu.Unlock()
			return audioout.RoomProgramSnapshot{}, ErrProgramResumeBaselineConflict
		}
		if program == nil || !program.Running {
			c.mu.Unlock()
			return audioout.RoomProgramSnapshot{}, errors.New("room program is not running")
		}
		if !program.ManualPaused {
			if program.Suspended {
				c.mu.Unlock()
				return audioout.RoomProgramSnapshot{}, errors.New("room program is suspended by an active interaction")
			}
			snapshot := c.programSnapshotLocked(program, c.now().UTC())
			c.mu.Unlock()
			return snapshot, nil
		}
		resume := program.ManualResume
		if resume != nil && resume.revision == program.ControlRevision && resume.inFlight {
			done := resume.done
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return audioout.RoomProgramSnapshot{}, ctx.Err()
			case <-done:
				continue
			}
		}
		if program.WaitingForContent && program.PendingRefresh == nil {
			program.Suspended, program.ManualPaused = false, false
			if program.TrackIndex+1 < len(program.Tracks) {
				go c.advanceMainline(program, program.CurrentTaskID)
			}
			snapshot := c.programSnapshotLocked(program, c.now().UTC())
			c.mu.Unlock()
			return snapshot, nil
		}
		if resume == nil || resume.revision != program.ControlRevision {
			if err := ensureProgramTrackLocked(program); err != nil {
				c.mu.Unlock()
				return audioout.RoomProgramSnapshot{}, err
			}
			offset := prepareProgramResumeLocked(program, program.ResumeOffsetMS)
			if program.Sequential && program.WaitingForContent && program.ApplyingRefresh == nil {
				program.Suspended, program.ManualPaused = false, false
				snapshot := c.programSnapshotLocked(program, c.now().UTC())
				c.mu.Unlock()
				return snapshot, nil
			}
			resume = &manualProgramResume{revision: program.ControlRevision, position: programPositionAt(program, program.TrackIndex), offsetMS: offset, sequence: program.Sequence + 1, refresh: program.ApplyingRefresh}
			program.ManualResume = resume
		} else if resume.refresh != nil && program.ApplyingRefresh == nil {
			if program.PendingRefresh != resume.refresh {
				c.mu.Unlock()
				return audioout.RoomProgramSnapshot{}, ErrProgramResumeBaselineConflict
			}
			applyProgramRefreshLocked(program)
		}
		index, err := resume.position.index(program)
		if err != nil {
			c.mu.Unlock()
			return audioout.RoomProgramSnapshot{}, ErrProgramResumeBaselineConflict
		}
		program.TrackIndex = index
		program.MainlinePosition = resume.position
		resume.inFlight, resume.done = true, make(chan struct{})
		resume.runtimeAllowed = func() bool {
			return ctx.Err() == nil && (runtimeAllowed == nil || runtimeAllowed())
		}
		c.mu.Unlock()
		err = c.executeManualResume(program, resume)
		if err != nil {
			return audioout.RoomProgramSnapshot{}, err
		}
		return c.ProgramSnapshot(ctx, roomID)
	}
}

func (c *Client) executeManualResume(program *programState, resume *manualProgramResume) (err error) {
	committed := false
	defer func() {
		// Also release concurrent waiters if a request-local panic is caught by
		// the HTTP boundary. The failed stable target remains retryable.
		c.mu.Lock()
		resume.inFlight = false
		close(resume.done)
		if committed && program.ManualResume == resume {
			program.ManualResume = nil
		}
		c.mu.Unlock()
	}()
	now := c.now().UTC()
	_, err = c.publishMainlineGuarded(program, resume.sequence, resume.offsetMS, now.Add(-time.Duration(resume.offsetMS)*time.Millisecond), resume)
	committed = err == nil
	return err
}
