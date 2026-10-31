package agentdecision

import (
	"encoding/json"
	"time"
)

// AdmitWelcome moves a prepared welcome from the generation lease to its one
// fixed playback lease. Cached audio and safe-cut retries cannot renew it.
func (q *Queue) AdmitWelcome(roomID int64, id string, deadline time.Time, knowledge json.RawMessage) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	state := q.roomLocked(roomID)
	for _, item := range state.items {
		if item.ID != id {
			continue
		}
		var old, next struct {
			Protocol, Task, Epoch string
			Welcome               bool      `json:"welcome_quota"`
			Ready                 time.Time `json:"welcome_ready_at"`
		}
		if json.Unmarshal(item.PurposeKnowledge, &old) != nil || json.Unmarshal(knowledge, &next) != nil || old.Protocol != "interaction_v2" || !old.Welcome || !next.Welcome || next.Task != old.Task || next.Epoch != old.Epoch || next.Ready.IsZero() || !deadline.Equal(next.Ready.Add(30*time.Second)) || !q.now().Before(deadline) || item.PlaybackTaskID != "" {
			return false
		}
		if !old.Ready.IsZero() {
			return old.Ready.Equal(next.Ready) && item.ExpiresAt.Equal(deadline)
		}
		if item.Status != StatusClaimed || !q.now().Before(itemDeadline(item)) {
			return false
		}
		item.ExpiresAt = deadline
		item.PreparedUntil = deadline
		item.AudioPrepared = true
		item.PurposeKnowledge = append(json.RawMessage(nil), knowledge...)
		item.InteractionDecision.DeadlineAt = deadline
		return true
	}
	return false
}
