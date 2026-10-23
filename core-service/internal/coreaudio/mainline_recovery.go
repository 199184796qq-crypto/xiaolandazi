package coreaudio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"livecompanion/core/internal/audioout"
)

func (c *Client) openMainlineAudio(ctx context.Context, program *programState, taskID string, position programTrackPosition, track programTrack) (io.ReadCloser, error) {
	c.mu.Lock()
	index, err := position.index(program)
	if err != nil || c.programs[program.RoomID] != program || program.CurrentTaskID != taskID || !program.Running || program.Suspended || program.ManualPaused {
		c.mu.Unlock()
		return nil, context.Canceled
	}
	if program.PrefetchedTrack == index && len(program.PrefetchedAudio) > 0 {
		data := program.PrefetchedAudio
		program.PrefetchedTrack, program.PrefetchedAudio = -1, nil
		c.mu.Unlock()
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	c.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, track.AudioURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("声音文件暂时不可用（状态%d）", resp.StatusCode)
	}
	return resp.Body, nil
}

type mainlineRecovery struct {
	cancel context.CancelFunc
}

func cancelMainlineRecoveryLocked(program *programState) {
	if program.Recovery != nil {
		program.Recovery.cancel()
		program.Recovery = nil
	}
}

func waitMainlineRecovery(ctx context.Context, attempt int) bool {
	delay := 250 * time.Millisecond * time.Duration(1<<min(attempt, 4))
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Retry the same already accepted audio, never text generation or TTS. Every
// attempt rechecks both ownership and immutable track identity before publish.
func (c *Client) scheduleMainlineRecovery(program *programState, taskID string, sequence uint64, offsetMS int, retryRefreshBoundary ...bool) {
	c.mu.Lock()
	if c.programs[program.RoomID] != program || !program.Running || program.Suspended || program.ManualPaused || program.CurrentTaskID != taskID || program.Recovery != nil {
		c.mu.Unlock()
		return
	}
	position := programPositionAt(program, program.TrackIndex)
	boundary := len(retryRefreshBoundary) > 0 && retryRefreshBoundary[0]
	if _, err := position.index(program); err != nil {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	recovery := &mainlineRecovery{cancel: cancel}
	program.Recovery = recovery
	c.mu.Unlock()
	go func() {
		defer cancel()
		defer func() {
			c.mu.Lock()
			if program.Recovery == recovery {
				program.Recovery = nil
			}
			c.mu.Unlock()
		}()
		for attempt := 0; waitMainlineRecovery(ctx, attempt); attempt++ {
			c.mu.Lock()
			index, err := position.index(program)
			owned := c.programs[program.RoomID] == program && program.Running && !program.Suspended && !program.ManualPaused && program.CurrentTaskID == taskID && program.Recovery == recovery && err == nil
			if !owned {
				c.mu.Unlock()
				return
			}
			program.TrackIndex = index
			c.mu.Unlock()
			if boundary {
				// A failed refresh publish rolled back the playlist. Re-enter its
				// accepted boundary, rather than replaying the consumed old track.
				c.advanceMainline(program, taskID)
				c.mu.RLock()
				waiting := program.WaitingForContent
				c.mu.RUnlock()
				if waiting {
					return
				}
				continue
			}
			if _, err = c.publishMainline(program, sequence, offsetMS, c.now().UTC().Add(-time.Duration(offsetMS)*time.Millisecond), taskID); err == nil {
				log.Printf("core audio mainline recovered room=%d track=%s", program.RoomID, position.TrackID)
				return
			}
			if attempt == 0 || attempt%8 == 0 {
				log.Printf("core audio mainline recovery pending room=%d track=%s: %v", program.RoomID, position.TrackID, err)
			}
		}
	}()
}

// Explicit timeout/failure compensation. Normal receiver completion must not
// declare unfinished answer PCM complete; this route resumes the owned mainline
// without manufacturing a COMPLETED answer receipt.
func (c *Client) RecoverProgramInteraction(ctx context.Context, roomID int64, taskID string) (audioout.RoomProgramSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return audioout.RoomProgramSnapshot{}, err
	}
	c.mu.Lock()
	program := c.programs[roomID]
	if program == nil || !program.Running || !program.Suspended || program.ManualPaused || program.CurrentTaskID != taskID {
		c.mu.Unlock()
		return c.ProgramSnapshot(ctx, roomID)
	}
	if state := program.InteractionReturn; state != nil {
		if _, err := state.resumePosition.index(program); err != nil {
			c.mu.Unlock()
			return audioout.RoomProgramSnapshot{}, fmt.Errorf("互动恢复位置已失效：%w", err)
		}
		state.finished, state.closed = true, true
		state.bridgePCM, state.bridgeText = nil, ""
		state.snapshot.BridgeStaged = false
		state.snapshot.BridgeDropReason = "interaction_output_failed"
	}
	if err := ensureProgramTrackLocked(program); err != nil {
		c.mu.Unlock()
		return audioout.RoomProgramSnapshot{}, err
	}
	c.mu.Unlock()
	log.Printf("core audio interaction recovery room=%d task=%s reason=output_failed", roomID, taskID)
	c.resumeAfterInteraction(program, taskID)
	return c.ProgramSnapshot(ctx, roomID)
}

func (c *Client) failMainlineAudio(program *programState, taskID string, position programTrackPosition, cause error) {
	c.mu.Lock()
	_, err := position.index(program)
	owned := c.programs[program.RoomID] == program && program.Running && !program.Suspended && !program.ManualPaused && program.CurrentTaskID == taskID && err == nil
	if owned {
		program.FailedMainlineTask = taskID
		// Terminal releases the failed unit, but Complete is derived solely from
		// real PCM coverage. Missing or partial audio never becomes fully played.
		finishPlaybackReceipt(program, position.TrackID)
	}
	c.mu.Unlock()
	if owned {
		log.Printf("core audio mainline segment failed room=%d track=%s task=%s incomplete=true: %v", program.RoomID, position.TrackID, taskID, cause)
		c.advanceMainline(program, taskID)
	}
}
