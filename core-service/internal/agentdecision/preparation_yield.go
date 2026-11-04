package agentdecision

import (
	"fmt"
	"strings"
	"time"
)

func (q *Queue) FailPlayback(room int64, id, task, reason string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	state := q.rooms[room]
	if state == nil {
		return false
	}
	for i, item := range state.items {
		if item.ID == id && item.Status == StatusClaimed && item.PlaybackTaskID == task {
			q.recordTerminalLocked(state, item, "FAILED", reason, q.now().UTC())
			state.items = append(state.items[:i], state.items[i+1:]...)
			return true
		}
	}
	return false
}

// YieldPreparation releases an unbound execution lease without spending a
// generation retry, extending original validity, or pretending audio exists.
func (q *Queue) YieldPreparation(room int64, id, reason string) (*Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	state := q.rooms[room]
	if state == nil {
		return nil, false
	}
	now := q.now().UTC()
	for _, item := range state.items {
		if item.ID != id || item.PlaybackTaskID != "" || (item.Status != StatusPending && item.Status != StatusClaimed) {
			continue
		}
		if item.Status == StatusClaimed && item.Attempts > 0 {
			item.Attempts--
		}
		item.Status = StatusPending
		item.ClaimedAt = nil
		item.RetryAfter = now.Add(5 * time.Second)
		q.addNoteLocked(state, now, "preparation_yield", fmt.Sprintf("decision=%s reason=%s retry_ms=5000", id, strings.TrimSpace(reason)))
		copy := cloneItem(*item)
		q.sortLocked(state)
		return &copy, true
	}
	return nil, false
}

// ClaimableHead matches ClaimNext's retry/claimed gates, so a deferred P0 does
// not make the HTTP layer validate the wrong candidate before claiming.
func (q *Queue) ClaimableHead(room int64) *Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	state := q.roomLocked(room)
	now := q.now().UTC()
	q.pruneLocked(room, state, now)
	q.sortLocked(state)
	for _, item := range state.items {
		if item.Status == StatusClaimed {
			return nil
		}
	}
	for _, item := range state.items {
		if item.Status == StatusPending && !now.Before(item.RetryAfter) {
			count := 0
			for _, played := range state.played {
				if played.Scene == item.Scene && now.Sub(played.AnsweredAt) < time.Minute {
					count++
				}
			}
			if item.RateLimitPerMinute > 0 && count >= item.RateLimitPerMinute {
				continue
			}
			c := cloneItem(*item)
			return &c
		}
	}
	return nil
}
