package coreaudio

import (
	"errors"
	"fmt"
)

var ErrProgramPositionInvalid = errors.New("当前播放位置暂时无法恢复")
var ErrProgramStartCancelled = errors.New("直播启动已取消或被新的操作替代")
var ErrProgramUnitNotAccepted = errors.New("本段声音尚未通过播放准备校验")

func (c *Client) beginProgramStart(roomID int64) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.startEpochs == nil {
		c.startEpochs = make(map[int64]uint64)
	}
	c.startEpochs[roomID]++
	return c.startEpochs[roomID]
}

// A queue offset is only a view. Playback and interaction returns retain an
// immutable identity, so removing consumed queue entries cannot move the target.
type programTrackPosition struct {
	ProgramID  string
	Generation uint64
	VersionID  int64
	TrackID    string
}

func programPositionAt(program *programState, index int) programTrackPosition {
	if program == nil || index < 0 || index >= len(program.Tracks) {
		return programTrackPosition{}
	}
	return programTrackPosition{program.ID, program.Generation, program.VersionID, program.Tracks[index].ID}
}

func (position programTrackPosition) index(program *programState) (int, error) {
	if program == nil || position.ProgramID != program.ID || position.Generation != program.Generation || position.VersionID != program.VersionID || position.TrackID == "" {
		return -1, errors.New("播放位置所属节目或版本已变化")
	}
	for i, track := range program.Tracks {
		if track.ID == position.TrackID {
			return i, nil
		}
	}
	return -1, errors.New("原定播放片段已不在当前队列中")
}

// Caller holds c.mu. Only a previously pinned identity may repair an invalid
// cursor. Falling back to track zero would replay unrelated accepted content.
func ensureProgramTrackLocked(program *programState) error {
	if program.TrackIndex >= 0 && program.TrackIndex < len(program.Tracks) {
		program.MainlinePosition = programPositionAt(program, program.TrackIndex)
		return nil
	}
	index, err := program.MainlinePosition.index(program)
	if err != nil {
		return fmt.Errorf("%w：%v", ErrProgramPositionInvalid, err)
	}
	program.TrackIndex = index
	return nil
}

// Caller holds c.mu. All live references are migrated together, including a
// return reserved before the interaction has actually started. Its token stays
// valid: only the derived array offsets change, never unit IDs or content.
func compactProgramTracksLocked(program *programState) {
	if program == nil || program.TrackIndex < 64 || program.TrackIndex >= len(program.Tracks) || program.Transitioning || program.PendingRefresh != nil || program.ApplyingRefresh != nil {
		return
	}
	remove := program.TrackIndex - 4
	if state := program.InteractionReturn; state != nil {
		index, err := state.resumePosition.index(program)
		if err != nil {
			return // An unresolvable owner must never authorize deleting content.
		}
		remove = min(remove, index)
		for _, unit := range state.snapshot.Units {
			index, err = (programTrackPosition{program.ID, program.Generation, program.VersionID, unit.TrackID}).index(program)
			if err != nil {
				return
			}
			remove = min(remove, index)
		}
	}
	if remove <= 0 {
		return
	}
	oldCount := len(program.Tracks)
	program.Tracks = append([]programTrack(nil), program.Tracks[remove:]...)
	program.TrackIndex -= remove
	remap := func(index int) int {
		if index < remove || index >= oldCount {
			return -1
		}
		return index - remove
	}
	program.PlannedNextTrack = remap(program.PlannedNextTrack)
	if program.PlannedNextTrack < 0 {
		program.PlannedForTaskID = ""
		program.PlannedNextScore = 0
		program.PlannedNextReason = ""
	}
	program.PrefetchedTrack = remap(program.PrefetchedTrack)
	if program.PrefetchedTrack < 0 {
		program.PrefetchedAudio = nil
	}
	if state := program.InteractionReturn; state != nil {
		for i := range state.snapshot.Units {
			unit := &state.snapshot.Units[i]
			unit.TrackIndex, _ = (programTrackPosition{program.ID, program.Generation, program.VersionID, unit.TrackID}).index(program)
		}
	}
	program.QueueRevision++
}
