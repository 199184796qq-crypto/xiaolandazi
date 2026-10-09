package agentdecision

import (
	"encoding/json"
	"strings"
)

func (q *Queue) SetExecutionReceipt(roomID int64, id, grade string, plan json.RawMessage, plans ...json.RawMessage) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	state := q.rooms[roomID]
	if state == nil {
		return false
	}
	for _, item := range state.items {
		if item.ID == id && item.Status == StatusClaimed {
			item.CutLevel = grade
			if len(plans) > 0 {
				item.PreparationCutPlans = append(json.RawMessage(nil), plans[0]...)
			}
			item.SelectedContentPlan = append(json.RawMessage(nil), plan...)
			return true
		}
	}
	return false
}

// SetContentPlan stores generation intent on the existing claimed queue item.
// Only Complete moves it into actual played history.
func (q *Queue) SetContentPlan(roomID int64, id, goal, angle string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	state := q.rooms[roomID]
	if state == nil {
		return false
	}
	for _, item := range state.items {
		if item.ID == id && item.Status == StatusClaimed {
			item.ContentGoal = strings.TrimSpace(goal)
			item.ContentAngle = strings.TrimSpace(angle)
			return true
		}
	}
	return false
}

func (q *Queue) BindPlayback(roomID int64, id, taskID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	state := q.rooms[roomID]
	if state == nil {
		return false
	}
	for _, item := range state.items {
		if item.ID == id && item.Status == StatusClaimed {
			item.PlaybackTaskID = taskID
			return true
		}
	}
	return false
}
