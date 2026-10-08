package coreaudio

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"livecompanion/core/internal/audioout"
	"strings"
)

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
		return audioout.RoomProgramSnapshot{}, errors.New("ready queue is full; wait for playback")
	}
	for index, unit := range input.Tracks {
		if _, ok := program.AppendedUnits[unit.ID]; ok {
			continue
		}
		program.Tracks = append(program.Tracks, tracks[index])
		program.AppendedUnits[unit.ID] = digests[unit.ID]
	}
	// Keep a small past context without allowing a long broadcast's audio queue
	// to grow indefinitely. Current task, safe points and resume offset remain.
	if program.TrackIndex >= 64 && !program.Transitioning {
		remove := program.TrackIndex - 4
		program.Tracks = append([]programTrack(nil), program.Tracks[remove:]...)
		program.TrackIndex -= remove
		if program.PlannedNextTrack >= 0 {
			program.PlannedNextTrack -= remove
		}
		if program.PrefetchedTrack >= 0 {
			program.PrefetchedTrack -= remove
		}
	}
	if program.WaitingForContent && !program.Suspended {
		go c.advanceMainline(program, program.CurrentTaskID)
	}
	return c.programSnapshotLocked(program, c.now().UTC()), nil
}
