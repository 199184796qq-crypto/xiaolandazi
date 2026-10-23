package coreaudio

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"livecompanion/core/internal/audioout"
	"log"
	"strings"
)

var ErrProgramQueueFull = errors.New("播放队列暂时已满，请等待已有内容播放")

func (c *Client) AppendProgram(ctx context.Context, input audioout.AppendProgramInput) (audioout.RoomProgramSnapshot, error) {
	if input.RoomID <= 0 || input.ExpectedProgramID == "" || input.ExpectedVersionID <= 0 || len(input.Tracks) == 0 || len(input.Tracks) > 4 {
		return audioout.RoomProgramSnapshot{}, errors.New("invalid sequential append")
	}
	seen := map[string]bool{}
	digests := map[string][32]byte{}
	for _, unit := range input.Tracks {
		if strings.TrimSpace(unit.ID) == "" || seen[unit.ID] {
			return audioout.RoomProgramSnapshot{}, errors.New("ready unit requires a unique ID")
		}
		seen[unit.ID] = true
		data, _ := json.Marshal(unit)
		digests[unit.ID] = sha256.Sum256(data)
	}
	// Reconcile a lost acknowledgement before probing media again. An accepted
	// unit remains accepted even if its signed URL later expires or the origin
	// becomes unavailable; a retry must not turn that receipt into a rejection.
	c.mu.RLock()
	current := c.programs[input.RoomID]
	if current == nil || !current.Running || !current.Sequential || current.ID != input.ExpectedProgramID || current.VersionID != input.ExpectedVersionID {
		c.mu.RUnlock()
		return audioout.RoomProgramSnapshot{}, ErrProgramRefreshConflict
	}
	allReceived := true
	for _, unit := range input.Tracks {
		previous, received := current.AppendedUnits[unit.ID]
		if received && previous != digests[unit.ID] {
			c.mu.RUnlock()
			return audioout.RoomProgramSnapshot{}, errors.New("ready unit ID was reused for different content")
		}
		allReceived = allReceived && received
	}
	if allReceived {
		snapshot := c.programSnapshotLocked(current, c.now().UTC())
		c.mu.RUnlock()
		return snapshot, nil
	}
	c.mu.RUnlock()
	tracks, err := c.prepareProgramTracks(ctx, input.Tracks)
	if err != nil {
		return audioout.RoomProgramSnapshot{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	program := c.programs[input.RoomID]
	if program == nil || !program.Running || !program.Sequential || program.ID != input.ExpectedProgramID || program.VersionID != input.ExpectedVersionID || program.PendingRefresh != nil || program.ApplyingRefresh != nil {
		return audioout.RoomProgramSnapshot{}, ErrProgramRefreshConflict
	}
	if err := ensureProgramTrackLocked(program); err != nil {
		return audioout.RoomProgramSnapshot{}, err
	}
	if program.AppendedUnits == nil {
		program.AppendedUnits = map[string][32]byte{}
	}
	for _, unit := range input.Tracks {
		if previous, ok := program.AppendedUnits[unit.ID]; ok && previous != digests[unit.ID] {
			return audioout.RoomProgramSnapshot{}, errors.New("ready unit ID was reused for different content")
		}
		if _, ok := program.AppendedUnits[unit.ID]; !ok {
			for _, existing := range program.Tracks {
				if existing.ID == unit.ID {
					return audioout.RoomProgramSnapshot{}, errors.New("ready unit already belongs to the initial program")
				}
			}
		}
	}
	newCount := 0
	for _, unit := range input.Tracks {
		if _, ok := program.AppendedUnits[unit.ID]; !ok {
			newCount++
		}
	}
	if len(program.Tracks)-program.TrackIndex+newCount > 256 {
		return audioout.RoomProgramSnapshot{}, ErrProgramQueueFull
	}
	for index, unit := range input.Tracks {
		if _, ok := program.AppendedUnits[unit.ID]; ok {
			continue
		}
		program.AppendedUnits[unit.ID] = digests[unit.ID]
		// Keep the ID receipt for retry idempotency even when no clip is queued.
		if len(program.Tracks) > 0 && !tracks[index].AllowConsecutiveRepeat && audioout.SameSpeech(program.Tracks[len(program.Tracks)-1].Text, tracks[index].Text) {
			log.Printf("mainline omitted room=%d track=%s reason=consecutive_exact_duplicate", input.RoomID, unit.ID)
			continue
		}
		program.Tracks = append(program.Tracks, tracks[index])
	}
	compactProgramTracksLocked(program)
	if program.WaitingForContent && !program.Suspended {
		go c.advanceMainline(program, program.CurrentTaskID)
	}
	return c.programSnapshotLocked(program, c.now().UTC()), nil
}
