package coreaudio

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/roomaudio"
)

type interactionReturn struct {
	pendingMainlineTask string
	snapshot            audioout.InteractionReturnSnapshot
	resumeTrack         int
	resumeMS            int
	bridgePCM           []byte
	bridgeText          string
	closed              bool
	finished            bool
}
type BridgeReady struct {
	TaskID     string `json:"task_id"`
	Token      string `json:"token"`
	Text       string `json:"text"`
	PCM        []byte `json:"pcm"`
	DurationMS int    `json:"duration_ms"`
}

func buildInteractionReturn(program *programState, taskID, reply string, cutMS, durationMS int, allowRepeat bool) *interactionReturn {
	state := &interactionReturn{resumeTrack: program.TrackIndex, resumeMS: cutMS, snapshot: audioout.InteractionReturnSnapshot{ProgramID: program.ID, TaskID: taskID, ReplyText: reply, AllowConsecutiveRepeat: allowRepeat, AnswerDurationMS: durationMS, Units: []audioout.ReturnUnit{}}}
	for trackIndex := program.TrackIndex; trackIndex < len(program.Tracks) && len(state.snapshot.Units) < 8; trackIndex++ {
		track := program.Tracks[trackIndex]
		if trackIndex == program.TrackIndex {
			for _, unit := range track.Timeline {
				if unit.EndMS <= cutMS {
					state.snapshot.BeforeText += unit.Text
				}
			}
			if state.snapshot.BeforeText == "" && cutMS >= track.DurationMS {
				state.snapshot.BeforeText = track.Text
			}
		}
		if len(track.Timeline) == 0 && strings.TrimSpace(track.Text) != "" && (trackIndex > program.TrackIndex || cutMS == 0) {
			state.snapshot.Units = append(state.snapshot.Units, audioout.ReturnUnit{ID: track.ID + "/FULL", TrackID: track.ID, TrackIndex: trackIndex, EndMS: track.DurationMS, Text: track.Text})
		}
		var pending *audioout.ReturnUnit
		for _, unit := range track.Timeline {
			if trackIndex == program.TrackIndex && unit.StartMS < cutMS {
				continue
			}
			if pending == nil {
				pending = &audioout.ReturnUnit{ID: track.ID + "/" + unit.SegmentID, TrackID: track.ID, TrackIndex: trackIndex, StartMS: unit.StartMS}
			}
			pending.Text += unit.Text
			pending.EndMS = unit.EndMS
			if unit.SafeCut {
				state.snapshot.Units = append(state.snapshot.Units, *pending)
				pending = nil
				if len(state.snapshot.Units) >= 8 {
					break
				}
			}
		}
		// An incomplete tail cannot be skipped or used as a bridge return entry.

	}
	// Never skip the last buffered unit without a known complete return entry.
	for !allowRepeat && len(state.snapshot.Units) > 1 && !program.Tracks[state.snapshot.Units[0].TrackIndex].AllowConsecutiveRepeat && audioout.CompleteUnitCovered(reply, state.snapshot.Units[0].Text) {
		state.snapshot.SkippedUnits = append(state.snapshot.SkippedUnits, state.snapshot.Units[0].ID)
		state.snapshot.Units = state.snapshot.Units[1:]
	}
	if len(state.snapshot.Units) > 0 {
		state.resumeTrack, state.resumeMS = state.snapshot.Units[0].TrackIndex, state.snapshot.Units[0].StartMS
		state.snapshot.ReturnAllowsRepeat = program.Tracks[state.resumeTrack].AllowConsecutiveRepeat
	}
	raw, _ := json.Marshal([]any{program.ID, program.Generation, program.VersionID, taskID, program.TrackIndex, cutMS, allowRepeat, state.snapshot.Units})
	state.snapshot.Token = fmt.Sprintf("%x", sha256.Sum256(raw))
	return state
}

func (c *Client) InteractionReturnSnapshot(_ context.Context, roomID int64, taskID string) (audioout.InteractionReturnSnapshot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	program := c.programs[roomID]
	if program == nil || !program.Running || program.InteractionReturn == nil || program.InteractionReturn.finished || (taskID != "" && program.InteractionReturn.snapshot.TaskID != taskID) || (program.Suspended && program.CurrentTaskID != program.InteractionReturn.snapshot.TaskID) || (!program.Suspended && program.CurrentTaskID != program.InteractionReturn.pendingMainlineTask) {
		return audioout.InteractionReturnSnapshot{}, fmt.Errorf("interaction return no longer active")
	}
	result := program.InteractionReturn.snapshot
	result.Units = append([]audioout.ReturnUnit(nil), result.Units...)
	result.SkippedUnits = append([]string(nil), result.SkippedUnits...)
	return result, nil
}

func (c *Client) markInteractionAudioStarted(roomID int64, taskID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	program := c.programs[roomID]
	if program == nil || program.CurrentTaskID != taskID || program.InteractionReturn == nil {
		return
	}
	state := program.InteractionReturn
	if state.snapshot.AudioStartedAt.IsZero() {
		state.snapshot.AudioStartedAt = c.now().UTC()
		state.snapshot.Deadline = state.snapshot.AudioStartedAt.Add(time.Duration(state.snapshot.AnswerDurationMS-800) * time.Millisecond)
	}
}

func (c *Client) handleReturnMessage(ctx context.Context, message messagecaptain.Message) error {
	if message.Kind != messagecaptain.ReturnBridgeReady {
		return nil
	}
	var ready BridgeReady
	if err := json.Unmarshal(message.Payload, &ready); err != nil {
		return err
	}
	if ready.DurationMS%roomaudio.FrameDurationMS != 0 || ready.DurationMS < 20 || ready.DurationMS > 8000 || len(ready.PCM) != ready.DurationMS/roomaudio.FrameDurationMS*roomaudio.PCMBytesPerFrame {
		return fmt.Errorf("invalid bridge PCM")
	}
	c.mu.Lock()
	program := c.programs[message.RoomID]
	if program == nil || !program.Running || program.ID != message.RunID || program.InteractionReturn == nil || program.InteractionReturn.snapshot.TaskID != ready.TaskID || (program.Suspended && program.CurrentTaskID != ready.TaskID) || (!program.Suspended && program.CurrentTaskID != program.InteractionReturn.pendingMainlineTask) || program.PendingRefresh != nil {
		c.mu.Unlock()
		return fmt.Errorf("bridge run changed")
	}
	state := program.InteractionReturn
	if state == nil || state.closed || state.snapshot.Token != ready.Token || state.snapshot.BridgeStaged || state.snapshot.Deadline.IsZero() || !c.now().Before(state.snapshot.Deadline) {
		c.mu.Unlock()
		return fmt.Errorf("bridge identity/deadline changed")
	}
	if state.snapshot.DuplicatesBridge(ready.Text) {
		state.snapshot.BridgeDropReason = "consecutive_exact_duplicate"
		c.mu.Unlock()
		log.Printf("bridge omitted room=%d task=%s reason=consecutive_exact_duplicate", message.RoomID, ready.TaskID)
		return nil
	}
	state.bridgePCM = append([]byte(nil), ready.PCM...)
	state.bridgeText = ready.Text
	state.snapshot.BridgeStaged = true
	state.snapshot.BridgeText = ready.Text
	duration := state.snapshot.AnswerDurationMS + ready.DurationMS
	captain := c.messages
	playing := program.Suspended
	c.mu.Unlock()
	if !playing {
		return nil
	}
	c.hub.ExtendTaskDuration(ready.TaskID, duration)
	if captain != nil {
		raw, _ := json.Marshal(map[string]any{"task_id": ready.TaskID, "text": ready.Text, "duration_ms": duration})
		captain.Publish(ctx, messagecaptain.Message{Kind: messagecaptain.ReturnBridgeStaged, RoomID: message.RoomID, RunID: message.RunID, TaskID: ready.TaskID, Payload: raw})
	}
	return nil
}

func (c *Client) takeReturnBridge(roomID int64, taskID string) ([]byte, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	program := c.programs[roomID]
	if program == nil || program.CurrentTaskID != taskID || program.InteractionReturn == nil {
		return nil, ""
	}
	state := program.InteractionReturn
	state.closed = true
	if program.PendingRefresh != nil {
		return nil, ""
	}
	if state.snapshot.DuplicatesBridge(state.bridgeText) {
		state.snapshot.BridgeDropReason = "consecutive_exact_duplicate"
		state.snapshot.BridgeStaged = false
		state.snapshot.BridgeText = ""
		state.bridgePCM, state.bridgeText = nil, ""
	}
	return state.bridgePCM, state.bridgeText
}

func (c *Client) finishReturn(roomID int64, taskID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if program := c.programs[roomID]; program != nil && program.CurrentTaskID == taskID && program.InteractionReturn != nil {
		program.InteractionReturn.finished = true
	}
}
