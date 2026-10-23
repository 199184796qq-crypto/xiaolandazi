package httpapi

import (
	"context"
	"log"
	"time"

	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/speechruntime"
)

type audioInteractionRecovery interface {
	RecoverProgramInteraction(context.Context, int64, string) (audioout.RoomProgramSnapshot, error)
}

func (s *Server) expireRoomAudioInteraction(roomID int64, task audioout.SpeechTask, decisionID string, startedAt time.Time, waitForMainline bool) {
	if s.speechRuntime == nil {
		return
	}
	snapshot, expired := s.speechRuntime.FailExpiredInterrupt(roomID, task.ID, decisionID, startedAt, 4*time.Second)
	if !expired {
		if snapshot.Interrupt.SpeechTaskID == task.ID && snapshot.Interrupt.DecisionID == decisionID && (snapshot.Interrupt.Status == speechruntime.StatusPlaying || snapshot.Interrupt.Status == speechruntime.StatusReady) {
			task.StartedAt = startedAt
			if snapshot.Interrupt.StartedAt != nil {
				task.StartedAt = *snapshot.Interrupt.StartedAt
			}
			task.DurationMS = snapshot.Interrupt.DurationMS
			s.scheduleRoomAudioInteractionCompletion(roomID, task, decisionID, waitForMainline)
		}
		return
	}
	// A missing terminal receipt fails this answer, not the mainline. The
	// Core recovery method verifies the exact still-suspended task and respects
	// pause/stop/replacement before it releases the return gate.
	s.recoverExpiredRoomAudioInteraction(roomID, task.ID, decisionID, 0)
	if decisionID != "" && s.agentDecisions != nil {
		_, _ = s.agentDecisions.Release(roomID, decisionID)
	}
}

func (s *Server) recoverExpiredRoomAudioInteraction(roomID int64, taskID, decisionID string, attempt int) {
	if s.speechRuntime == nil {
		return
	}
	speech, err := s.speechRuntime.Snapshot(roomID)
	if err != nil || speech.Interrupt.SpeechTaskID != taskID || speech.Interrupt.DecisionID != decisionID || speech.Interrupt.Status != speechruntime.StatusFailed {
		return
	}
	state := s.audioDevState()
	if state == nil || state.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	program, err := state.client.ProgramSnapshot(ctx, roomID)
	if err == nil && (!program.Running || program.Task == nil || program.Task.ID != taskID) {
		return
	}
	if recovery, ok := state.client.(audioInteractionRecovery); err == nil && ok {
		_, err = recovery.RecoverProgramInteraction(ctx, roomID, taskID)
	} else if completer, ok := state.client.(audioInteractionCompleter); err == nil && ok {
		_, err = completer.CompleteProgramInteraction(ctx, roomID, taskID)
	}
	if err == nil {
		return
	}
	log.Printf("interaction watchdog mainline recovery room=%d task=%s attempt=%d failed: %v", roomID, taskID, attempt+1, err)
	delay := time.Second << min(attempt, 4)
	time.AfterFunc(delay, func() {
		s.recoverExpiredRoomAudioInteraction(roomID, taskID, decisionID, min(attempt+1, 4))
	})
}
