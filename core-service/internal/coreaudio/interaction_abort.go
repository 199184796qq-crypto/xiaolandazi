package coreaudio

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"livecompanion/core/internal/audioout"
)

// ErrInteractionNotActive means the room either has no current interaction or
// the caller is holding a stale task id. Recovery is deliberately scoped to the
// exact task: a late watchdog must never abort a newer interaction.
var ErrInteractionNotActive = errors.New("互动任务已不处于可回收状态")

// AbortProgramInteraction is the last-resort return path. Unlike
// resumeAfterInteraction it does not require the return bridge to be finished
// and it does not refuse a program that already stopped. It guarantees one of
// two terminal states:
//
//   - running program: publish the mainline again at the saved offset;
//   - stopped/empty/manual-paused program: clear the interaction and move the
//     room audio engine to a non-destructive idle state.
//
// It is idempotent for the same task. A second call returns
// ErrInteractionNotActive rather than publishing another mainline task.
func (c *Client) AbortProgramInteraction(ctx context.Context, roomID int64, interactionTaskID string) (audioout.RoomProgramSnapshot, error) {
	if c == nil || roomID <= 0 || strings.TrimSpace(interactionTaskID) == "" {
		return audioout.RoomProgramSnapshot{}, ErrInteractionNotActive
	}
	interactionTaskID = strings.TrimSpace(interactionTaskID)

	c.mu.Lock()
	program := c.programs[roomID]
	if program == nil || program.CurrentTaskID != interactionTaskID {
		c.mu.Unlock()
		return audioout.RoomProgramSnapshot{}, ErrInteractionNotActive
	}
	programID, sessionID, controlRevision := program.ID, program.SessionID, program.ControlRevision
	returnToken := ""
	if state := program.InteractionReturn; state != nil {
		returnToken = strings.TrimSpace(state.snapshot.Token)
	}
	if returnToken != "" {
		if state := program.InteractionReturn; state == nil || strings.TrimSpace(state.snapshot.Token) != returnToken {
			c.mu.Unlock()
			return audioout.RoomProgramSnapshot{}, ErrInteractionNotActive
		}
	}
	if program.ManualPaused {
		// The operator owns the pause. Remove the dead interaction state but do
		// not start audio behind their back.
		program.InteractionReturn = nil
		program.Suspended = false
		program.CurrentTaskID = ""
		snapshot := c.programSnapshotLocked(program, c.now().UTC())
		c.mu.Unlock()
		c.hub.Expire(interactionTaskID)
		if c.roomAudio != nil {
			c.roomAudio.IdleWaitingContent(roomID)
		}
		log.Printf("core audio interaction forced idle room=%d task=%s reason=manual_paused", roomID, interactionTaskID)
		return snapshot, nil
	}
	if !program.Running || len(program.Tracks) == 0 {
		program.InteractionReturn = nil
		program.Suspended = false
		program.CurrentTaskID = ""
		snapshot := c.programSnapshotLocked(program, c.now().UTC())
		c.mu.Unlock()
		c.hub.Expire(interactionTaskID)
		if c.roomAudio != nil {
			c.roomAudio.IdleWaitingContent(roomID)
		}
		log.Printf("core audio interaction forced idle room=%d task=%s reason=program_stopped", roomID, interactionTaskID)
		return snapshot, nil
	}

	resumeMS := program.ResumeOffsetMS
	if state := program.InteractionReturn; state != nil {
		// Use the bridge's prepared resume point even if the bridge never sent
		// its terminal receipt. A late mainline is better than permanent silence.
		if state.resumeMS >= 0 {
			resumeMS = state.resumeMS
		}
		if resumeTrack, err := state.resumePosition.index(program); err == nil {
			for i := program.TrackIndex; i < resumeTrack && i < len(program.Tracks); i++ {
				if i >= 0 {
					finishPlaybackReceipt(program, program.Tracks[i].ID)
				}
			}
			program.TrackIndex = resumeTrack
			program.MainlinePosition = state.resumePosition
		}
		program.InteractionReturn = nil
	}
	if err := ensureProgramTrackLocked(program); err != nil {
		program.Suspended = false
		program.CurrentTaskID = ""
		snapshot := c.programSnapshotLocked(program, c.now().UTC())
		c.mu.Unlock()
		c.hub.Expire(interactionTaskID)
		if c.roomAudio != nil {
			c.roomAudio.IdleWaitingContent(roomID)
		}
		log.Printf("core audio interaction forced idle room=%d task=%s reason=%v", roomID, interactionTaskID, err)
		return snapshot, nil
	}
	nextSequence := program.Sequence + 1
	program.Suspended = false
	resumeMS = prepareProgramResumeLocked(program, resumeMS)
	if program.Sequential && program.WaitingForContent && program.ApplyingRefresh == nil {
		program.CurrentTaskID = ""
		snapshot := c.programSnapshotLocked(program, c.now().UTC())
		c.mu.Unlock()
		c.hub.Expire(interactionTaskID)
		if c.roomAudio != nil {
			c.roomAudio.IdleWaitingContent(roomID)
		}
		log.Printf("core audio interaction forced idle room=%d task=%s reason=waiting_for_content", roomID, interactionTaskID)
		return snapshot, nil
	}
	resumeSegmentID := ""
	if program.TrackIndex >= 0 && program.TrackIndex < len(program.Tracks) {
		resumeSegmentID = roomAudioSegmentAt(program.Tracks[program.TrackIndex].Timeline, resumeMS)
	}
	c.mu.Unlock()

	// Revalidate the exact generation before any external side effect. A pause,
	// program replacement or newer interaction that landed while preparing the
	// resume must win over this recovery.
	c.mu.Lock()
	current := c.programs[roomID]
	if current != program || current == nil || current.ID != programID || current.SessionID != sessionID || current.ControlRevision != controlRevision || current.CurrentTaskID != interactionTaskID || current.ManualPaused {
		snapshot := audioout.RoomProgramSnapshot{RoomID: roomID, ServerTime: c.now().UTC()}
		if current != nil {
			snapshot = c.programSnapshotLocked(current, c.now().UTC())
		}
		c.mu.Unlock()
		return snapshot, ErrInteractionNotActive
	}
	c.mu.Unlock()

	c.hub.Expire(interactionTaskID)
	if c.roomAudio != nil {
		if _, err := c.roomAudio.PrepareResume(roomID, resumeSegmentID); err == nil {
			_, _ = c.roomAudio.StartResume(roomID, resumeSegmentID)
		}
	}
	now := c.now().UTC()
	if _, err := c.publishMainline(program, nextSequence, resumeMS, now.Add(-time.Duration(resumeMS)*time.Millisecond), interactionTaskID); err != nil {
		// publishMainline rolls the task back on a race. If it was a genuine
		// failure, leave the engine idle rather than interrupt.
		c.mu.Lock()
		if current := c.programs[roomID]; current == program && current.CurrentTaskID == interactionTaskID {
			current.InteractionReturn = nil
			current.Suspended = false
			current.CurrentTaskID = ""
		}
		snapshot := c.programSnapshotLocked(program, c.now().UTC())
		c.mu.Unlock()
		if c.roomAudio != nil {
			c.roomAudio.IdleWaitingContent(roomID)
		}
		log.Printf("core audio interaction forced idle room=%d task=%s reason=publish_failed err=%v", roomID, interactionTaskID, err)
		return snapshot, err
	}
	log.Printf("core audio interaction forced mainline room=%d task=%s resume_ms=%d sequence=%d", roomID, interactionTaskID, resumeMS, nextSequence)
	return c.ProgramSnapshot(ctx, roomID)
}
