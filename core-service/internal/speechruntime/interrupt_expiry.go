package speechruntime

import (
	"strings"
	"time"
)

// FailExpiredInterrupt is a compare-and-transition for the receiver watchdog.
// It must not overwrite a new task, a pause, or a later playback deadline.
func (r *Registry) FailExpiredInterrupt(roomID int64, taskID, decisionID string, fallbackStart time.Time, grace time.Duration) (Snapshot, bool) {
	if roomID <= 0 || strings.TrimSpace(taskID) == "" {
		return Snapshot{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.rooms[roomID]
	if state == nil {
		return Snapshot{}, false
	}
	now := r.now().UTC()
	track := &state.interrupt
	if track.SpeechTaskID != taskID || track.DecisionID != decisionID || (track.Status != StatusPlaying && track.Status != StatusReady) {
		return snapshotLocked(roomID, state, now), false
	}
	started := fallbackStart
	if track.StartedAt != nil {
		started = *track.StartedAt
	}
	if started.IsZero() || track.DurationMS <= 0 || now.Before(started.Add(time.Duration(track.DurationMS)*time.Millisecond+grace)) {
		return snapshotLocked(roomID, state, now), false
	}
	track.Status = StatusFailed
	track.UpdatedAt = timePtr(now)
	state.revision++
	state.updatedAt = timePtr(now)
	return snapshotLocked(roomID, state, now), true
}
