package coreaudio

import (
	"context"
	"fmt"
	"time"

	"livecompanion/core/internal/audioout"
)

// The ledger is written before delivering the event, so a lost listener can
// reconcile the same task without inventing completion or re-buying its audio.
func (c *Client) InteractionPlaybackSnapshot(_ context.Context, room int64, task string) (audioout.InteractionPlaybackReceipt, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.interactionPlayback[task]
	if !ok || r.RoomID != room {
		return r, fmt.Errorf("interaction PCM receipt unavailable")
	}
	return r, nil
}

func (c *Client) prepareInteractionPlayback(program *programState, task audioout.SpeechTask, decision string, until time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.interactionPlayback == nil {
		c.interactionPlayback = make(map[string]audioout.InteractionPlaybackReceipt)
	}
	for id, r := range c.interactionPlayback {
		if c.now().Sub(r.PreparedUntil) > 30*time.Minute {
			delete(c.interactionPlayback, id)
		}
	}
	if len(c.interactionPlayback) >= 2048 {
		var oldest string
		var at time.Time
		for id, r := range c.interactionPlayback {
			if r.Event.Status != "COMPLETED" && r.Event.Status != "FAILED" {
				continue
			}
			if oldest == "" || r.PreparedUntil.Before(at) {
				oldest, at = id, r.PreparedUntil
			}
		}
		if oldest != "" {
			delete(c.interactionPlayback, oldest)
		}
	}
	c.interactionPlayback[task.ID] = audioout.InteractionPlaybackReceipt{RoomID: program.RoomID, ProgramID: program.ID, TaskID: task.ID, DecisionID: decision, SessionID: task.SessionID, DurationMS: task.DurationMS, PreparedUntil: until}
}

func (c *Client) recordInteractionPCM(task string, event audioout.PlaybackEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.interactionPlayback[task]
	if !ok || r.RoomID != event.RoomID || r.Event.Status == "COMPLETED" || r.Event.Status == "FAILED" {
		return
	}
	if event.Status == "PLAYING" && r.StartedAt.IsZero() {
		r.StartedAt = event.OccurredAt
	}
	if event.Status == "STARTING" {
		return
	}
	r.Event = event
	c.interactionPlayback[task] = r
}

func interactionPlaybackDeadline(r audioout.InteractionPlaybackReceipt) time.Time {
	if r.StartedAt.IsZero() {
		return r.PreparedUntil
	}
	return r.StartedAt.Add(time.Duration(r.DurationMS)*time.Millisecond + 4*time.Second)
}

func (c *Client) watchInteractionPlayback(program *programState, task string) {
	r, err := c.InteractionPlaybackSnapshot(context.Background(), program.RoomID, task)
	if err != nil || r.Event.Status == "COMPLETED" || r.Event.Status == "FAILED" {
		return
	}
	deadline := interactionPlaybackDeadline(r)
	delay := time.Until(deadline)
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() {
		c.mu.RLock()
		current := c.programs[program.RoomID]
		active := current == program && program.Running && program.Suspended && !program.ManualPaused && program.CurrentTaskID == task
		c.mu.RUnlock()
		if !active {
			return
		}
		latest, e := c.InteractionPlaybackSnapshot(context.Background(), program.RoomID, task)
		if e != nil || latest.Event.Status == "COMPLETED" || latest.Event.Status == "FAILED" {
			return
		}
		if time.Now().Before(interactionPlaybackDeadline(latest)) {
			c.watchInteractionPlayback(program, task)
			return
		}
		reason := "terminal_receipt_missing"
		if latest.StartedAt.IsZero() {
			reason = "source_start_timeout"
		}
		c.observeInterruptTerminal(program.RoomID, task, "FAILED", fmt.Errorf("%s", reason))
		// Terminal delivery can fail too. Exact-current recovery remains guarded
		// by task/program/pause ownership inside AbortProgramInteraction.
		c.AbortProgramInteraction(context.Background(), program.RoomID, task)
	})
}
