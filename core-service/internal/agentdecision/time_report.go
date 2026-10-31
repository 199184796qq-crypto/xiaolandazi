package agentdecision

import "time"

// Bind the content's original semantic expiry to the current execution lease.
// It is not a fresh TTL and cannot be extended just because audio was retried.
func (q *Queue) SetTimeReportValidity(roomID int64, id string, until time.Time) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if until.IsZero() || !q.now().Before(until) {
		return false
	}
	state := q.rooms[roomID]
	if state == nil {
		return false
	}
	for _, item := range state.items {
		if item.ID == id && item.Status == StatusClaimed && item.MissionKind == "time_report" {
			item.TimeReportValidUntil = until
			return true
		}
	}
	return false
}
