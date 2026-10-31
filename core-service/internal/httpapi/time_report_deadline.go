package httpapi

import (
	"time"

	"livecompanion/core/internal/agentdecision"
)

func bindTimeReportDeadline(queue *agentdecision.Queue, room int64, item agentdecision.Item, until, now time.Time) bool {
	if item.MissionKind != "time_report" {
		return until.IsZero()
	}
	return !until.IsZero() && now.Before(until) && queue.SetTimeReportValidity(room, item.ID, until)
}
