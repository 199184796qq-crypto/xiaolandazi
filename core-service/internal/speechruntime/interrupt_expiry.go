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
	if track.SpeechTaskID != taskID || track.DecisionID != decisionID || track.Status != StatusPlaying {
		return snapshotLocked(roomID, state, now), false
	}
	var started time.Time
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

// FailUnstartedInterrupt uses only the finite preparation/start deadline.
func (r *Registry) FailUnstartedInterrupt(roomID int64, taskID, decisionID string, until time.Time) (Snapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.rooms[roomID]
	if state == nil {
		return Snapshot{}, false
	}
	now := r.now().UTC()
	t := &state.interrupt
	if t.SpeechTaskID != taskID || t.DecisionID != decisionID || t.Status != StatusReady || t.StartedAt != nil || until.IsZero() || now.Before(until) {
		return snapshotLocked(roomID, state, now), false
	}
	t.Status = StatusFailed
	t.UpdatedAt = timePtr(now)
	state.revision++
	state.updatedAt = timePtr(now)
	return snapshotLocked(roomID, state, now), true
}

func (r *Registry) FailCurrentInterrupt(roomID int64, taskID, decisionID string) (Snapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.rooms[roomID]
	if s == nil {
		return Snapshot{}, false
	}
	now := r.now().UTC()
	t := &s.interrupt
	if t.SpeechTaskID != taskID || t.DecisionID != decisionID || (t.Status != StatusReady && t.Status != StatusPlaying && t.Status != StatusReturning) {
		return snapshotLocked(roomID, s, now), false
	}
	t.Status = StatusFailed
	t.UpdatedAt = timePtr(now)
	s.revision++
	s.updatedAt = timePtr(now)
	return snapshotLocked(roomID, s, now), true
}
