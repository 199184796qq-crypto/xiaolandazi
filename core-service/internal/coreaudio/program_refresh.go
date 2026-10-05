package coreaudio

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/core/internal/audioout"
)

var ErrProgramRefreshConflict = errors.New("program refresh baseline conflict")

type programRefreshReceipt struct {
	Digest     [32]byte
	Generation uint64
	Cancelled  bool
}

type programRefresh struct {
	JobID              string
	Generation         uint64
	VersionID          int64
	VersionNo          int64
	Tracks             []programTrack
	ProtectedNextIndex int
	ValidUntil         time.Time
	PreviousTracks     []programTrack
	PreviousTrackIndex int
}

// RefreshProgram stages an already-generated playlist; it never creates a
// session, resets billing, interrupts a task, or changes the current audio.
// Application happens only when advanceMainline completes the current track
// and any next track that was already queued when this request was accepted.
func (c *Client) RefreshProgram(ctx context.Context, input audioout.RefreshProgramInput) (audioout.RoomProgramSnapshot, error) {
	if !c.Enabled() {
		return audioout.RoomProgramSnapshot{}, errors.New("Core声音广播未启用")
	}
	input.JobID = strings.TrimSpace(input.JobID)
	input.ExpectedProgramID = strings.TrimSpace(input.ExpectedProgramID)
	validLease := input.ValidUntil.After(c.now()) && !input.ValidUntil.After(c.now().Add(time.Minute))
	if input.RoomID <= 0 || input.JobID == "" || len(input.JobID) > 128 || input.ExpectedProgramID == "" || input.ExpectedVersionID <= 0 || input.NewVersionID <= 0 || input.NewVersionNo <= 0 || input.Generation == 0 || input.Generation != input.ExpectedGeneration+1 {
		return audioout.RoomProgramSnapshot{}, errors.New("invalid refresh job, program/version baseline or generation")
	}
	digestInput := input
	digestInput.ValidUntil = time.Time{}
	raw, err := json.Marshal(digestInput)
	if err != nil {
		return audioout.RoomProgramSnapshot{}, err
	}
	digest := sha256.Sum256(raw)
	c.mu.Lock()
	program, duplicate, err := c.checkProgramRefreshLocked(input, digest)
	if err != nil {
		c.mu.Unlock()
		return audioout.RoomProgramSnapshot{}, err
	}
	if duplicate {
		if program.PendingRefresh != nil && program.PendingRefresh.JobID == input.JobID {
			if !validLease {
				c.mu.Unlock()
				return audioout.RoomProgramSnapshot{}, errors.New("refresh validity must be within the next 60 seconds")
			}
			program.PendingRefresh.ValidUntil = input.ValidUntil
		}
		snapshot := c.programSnapshotLocked(program, c.now().UTC())
		c.mu.Unlock()
		return snapshot, nil
	}
	c.mu.Unlock()
	if !validLease {
		return audioout.RoomProgramSnapshot{}, errors.New("refresh validity must be within the next 60 seconds")
	}

	// Slow asset I/O stays outside the program lock; the same baseline is checked
	// again after validation so stop/manual switch/new jobs always win the race.
	tracks, err := c.prepareProgramTracks(ctx, input.Tracks)
	if err != nil {
		return audioout.RoomProgramSnapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return audioout.RoomProgramSnapshot{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	program, duplicate, err = c.checkProgramRefreshLocked(input, digest)
	if err != nil {
		return audioout.RoomProgramSnapshot{}, err
	}
	if !duplicate {
		protectedNext := -1
		if program.PlannedForTaskID == program.CurrentTaskID && program.PlannedNextTrack >= 0 && program.PlannedNextTrack < len(program.Tracks) {
			protectedNext = program.PlannedNextTrack
		}
		if !input.ValidUntil.After(c.now()) {
			return audioout.RoomProgramSnapshot{}, errors.New("refresh validity expired while preparing assets")
		}
		program.PendingRefresh = &programRefresh{JobID: input.JobID, Generation: input.Generation, VersionID: input.NewVersionID, VersionNo: input.NewVersionNo, Tracks: tracks, ProtectedNextIndex: protectedNext, ValidUntil: input.ValidUntil}
		if program.RefreshReceipts == nil {
			program.RefreshReceipts = make(map[string]programRefreshReceipt)
		}
		program.RefreshReceipts[input.JobID] = programRefreshReceipt{Digest: digest, Generation: input.Generation}
		for job, receipt := range program.RefreshReceipts {
			if program.Generation > 64 && receipt.Generation < program.Generation-64 {
				delete(program.RefreshReceipts, job)
			}
		}
	}
	return c.programSnapshotLocked(program, c.now().UTC()), nil
}

func (c *Client) checkProgramRefreshLocked(input audioout.RefreshProgramInput, digest [32]byte) (*programState, bool, error) {
	program := c.programs[input.RoomID]
	if program == nil || !program.Running || program.ID != input.ExpectedProgramID {
		return nil, false, fmt.Errorf("%w: program stopped or changed", ErrProgramRefreshConflict)
	}
	if receipt, ok := program.RefreshReceipts[input.JobID]; ok {
		if receipt.Cancelled {
			return nil, false, fmt.Errorf("%w: refresh job cancelled or expired", ErrProgramRefreshConflict)
		}
		if receipt.Digest != digest {
			return nil, false, fmt.Errorf("%w: job id reused with different payload", ErrProgramRefreshConflict)
		}
		return program, true, nil
	}
	if program.Transitioning || program.VersionID != input.ExpectedVersionID || program.Generation != input.ExpectedGeneration || program.PendingRefresh != nil {
		return nil, false, fmt.Errorf("%w: version/generation changed or another job is pending", ErrProgramRefreshConflict)
	}
	return program, false, nil
}

// Caller holds c.mu. This changes only future content. Task/session identifiers,
// interaction state and start time are intentionally not modified.
func applyProgramRefreshLocked(program *programState) {
	refresh := program.PendingRefresh
	if refresh == nil {
		return
	}
	refresh.PreviousTracks = program.Tracks
	refresh.PreviousTrackIndex = program.TrackIndex
	program.ApplyingRefresh = refresh
	program.Transitioning = true
	previousID := ""
	if program.TrackIndex >= 0 && program.TrackIndex < len(program.Tracks) {
		previousID = program.Tracks[program.TrackIndex].ID
	}
	program.Tracks = refresh.Tracks
	program.TrackIndex = -1
	for index, track := range program.Tracks {
		if track.ID == previousID {
			program.TrackIndex = index
			break
		}
	}
	program.PlannedNextTrack = -1
	program.PlannedForTaskID = ""
	program.PlannedNextScore = 0
	program.PlannedNextReason = ""
	program.PrefetchedTrack = -1
	program.PrefetchedAudio = nil
}

func finishProgramRefreshLocked(program *programState) {
	refresh := program.ApplyingRefresh
	if refresh == nil {
		return
	}
	program.Generation = refresh.Generation
	program.VersionID = refresh.VersionID
	program.VersionNo = refresh.VersionNo
	program.LastAppliedJobID = refresh.JobID
	program.PendingRefresh = nil
	program.ApplyingRefresh = nil
}

func rollbackProgramRefreshLocked(program *programState) {
	refresh := program.ApplyingRefresh
	if refresh == nil {
		return
	}
	program.Tracks = refresh.PreviousTracks
	program.TrackIndex = refresh.PreviousTrackIndex
	program.ApplyingRefresh = nil
	program.Transitioning = false
}

func expireProgramRefreshLocked(program *programState, now time.Time) {
	refresh := program.PendingRefresh
	if refresh == nil || program.ApplyingRefresh != nil || now.Before(refresh.ValidUntil) {
		return
	}
	result := program.RefreshReceipts[refresh.JobID]
	result.Cancelled = true
	program.RefreshReceipts[refresh.JobID] = result
	program.PendingRefresh = nil
}

// Also tombstone a request still preparing assets, so a probe cannot later
// reinstall a revoked refresh. Content already applied is never rolled back.
func (c *Client) CancelProgramRefresh(_ context.Context, input audioout.CancelProgramRefreshInput) (audioout.RoomProgramSnapshot, error) {
	if c == nil || input.RoomID <= 0 || strings.TrimSpace(input.JobID) == "" || len(input.JobID) > 128 || strings.TrimSpace(input.ExpectedProgramID) == "" {
		return audioout.RoomProgramSnapshot{}, errors.New("invalid refresh cancellation")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	program := c.programs[input.RoomID]
	if program == nil || program.ID != input.ExpectedProgramID {
		return audioout.RoomProgramSnapshot{}, fmt.Errorf("%w: program changed", ErrProgramRefreshConflict)
	}
	if program.ApplyingRefresh != nil && program.ApplyingRefresh.JobID == input.JobID {
		return audioout.RoomProgramSnapshot{}, fmt.Errorf("%w: refresh already entering playback boundary", ErrProgramRefreshConflict)
	}
	if program.RefreshReceipts == nil {
		program.RefreshReceipts = make(map[string]programRefreshReceipt)
	}
	receipt, exists := program.RefreshReceipts[input.JobID]
	if exists && !receipt.Cancelled && receipt.Generation <= program.Generation {
		return c.programSnapshotLocked(program, c.now().UTC()), nil
	}
	receipt.Cancelled = true
	program.RefreshReceipts[input.JobID] = receipt
	if program.PendingRefresh != nil && program.PendingRefresh.JobID == input.JobID {
		program.PendingRefresh = nil
	}
	return c.programSnapshotLocked(program, c.now().UTC()), nil
}

// Management renews only after rechecking consent, mode, source revision and
// room state. Signed asset URLs and the original payload remain untouched.
func (c *Client) RenewProgramRefresh(_ context.Context, input audioout.RenewProgramRefreshInput) (audioout.RoomProgramSnapshot, error) {
	if c == nil || input.RoomID <= 0 || strings.TrimSpace(input.JobID) == "" || !input.ValidUntil.After(c.now()) || input.ValidUntil.After(c.now().Add(time.Minute)) {
		return audioout.RoomProgramSnapshot{}, errors.New("invalid refresh lease renewal")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	program := c.programs[input.RoomID]
	if program == nil || !program.Running || program.ID != input.ExpectedProgramID {
		return audioout.RoomProgramSnapshot{}, fmt.Errorf("%w: program stopped or changed", ErrProgramRefreshConflict)
	}
	receipt, ok := program.RefreshReceipts[input.JobID]
	if !ok || receipt.Cancelled {
		return audioout.RoomProgramSnapshot{}, fmt.Errorf("%w: refresh job missing, cancelled or expired", ErrProgramRefreshConflict)
	}
	if program.PendingRefresh != nil && program.PendingRefresh.JobID == input.JobID {
		program.PendingRefresh.ValidUntil = input.ValidUntil
	} else if receipt.Generation > program.Generation {
		return audioout.RoomProgramSnapshot{}, fmt.Errorf("%w: refresh job no longer pending", ErrProgramRefreshConflict)
	}
	return c.programSnapshotLocked(program, c.now().UTC()), nil
}
