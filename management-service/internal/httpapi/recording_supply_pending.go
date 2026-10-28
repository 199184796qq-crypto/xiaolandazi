package httpapi

import (
	"context"
	"fmt"
	"log"

	"livecompanion/management/internal/model"
)

// replayPendingRecordingSupply completes the one append that may have been
// accepted by Core just before a crash. Core AppendProgram is idempotent by
// unit ID and payload digest, so resending the exact pending track is safe; a
// different track is never regenerated here.
func (s *Server) replayPendingRecordingSupply(
	ctx context.Context,
	tenant, room, runtimeID int64,
	version model.LiveAgentPlanVersion,
	queue *recordingReadyQueue,
	pending *recordingSupplyPendingAppend,
) error {
	if pending == nil || pending.Track.AudioURL == "" {
		return nil
	}
	sessionID := fmt.Sprintf("recording-stream-%d", runtimeID)
	if pending.ProgramID != "" {
		generation, err := s.appendRecordingStreamTrack(ctx, tenant, room, version, pending.ProgramID, pending.Track)
		if err != nil {
			return err
		}
		queue.programID, queue.generation, queue.blockSeq = pending.ProgramID, generation, pending.BlockSeq
		log.Printf("recording supply replay pending append tenant=%d room=%d session=%d program=%s block=%d track=%s", tenant, room, runtimeID, pending.ProgramID, pending.BlockSeq, pending.Track.ID)
		return nil
	}
	// The pending append was the first program track. If Core already started
	// this exact session, adopt it rather than starting a second program.
	if current, err := s.core.GetRoomProgramSnapshot(ctx, tenant, room); err == nil && current.Running && current.VersionID == version.ID && current.Task.SessionID == sessionID {
		queue.programID, queue.generation, queue.blockSeq = current.ProgramID, current.Generation, pending.BlockSeq
		log.Printf("recording supply adopted pending start tenant=%d room=%d session=%d program=%s", tenant, room, runtimeID, current.ProgramID)
		return nil
	}
	snapshot, err := s.startRecordingStreamProgram(ctx, tenant, room, runtimeID, version, pending.Track)
	if err != nil {
		return err
	}
	queue.programID, queue.generation, queue.blockSeq = snapshot.ProgramID, snapshot.Generation, pending.BlockSeq
	log.Printf("recording supply replayed pending start tenant=%d room=%d session=%d program=%s", tenant, room, runtimeID, snapshot.ProgramID)
	return nil
}
