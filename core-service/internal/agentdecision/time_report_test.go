package agentdecision

import (
	"testing"
	"time"
)

func TestTimeReportValidityRequiresOwnedClaim(t *testing.T) {
	now := time.Now().UTC()
	q := NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 5)
	item := q.Enqueue(1, Candidate{Source: SourceAgent, Topic: "INTERACTION:TIME_REPORT:run:1", MissionKind: "time_report", Question: "报时"}).Item
	if q.SetTimeReportValidity(1, item.ID, now.Add(time.Minute)) {
		t.Fatal("unclaimed report accepted deadline")
	}
	q.ClaimNext(1)
	if q.SetTimeReportValidity(2, item.ID, now.Add(time.Minute)) || q.SetTimeReportValidity(1, item.ID, now) {
		t.Fatal("wrong room or expired content accepted")
	}
	if !q.SetTimeReportValidity(1, item.ID, now.Add(time.Minute)) {
		t.Fatal("claimed report rejected")
	}
	copy, _ := q.Get(1, item.ID)
	if !copy.TimeReportValidUntil.Equal(now.Add(time.Minute)) {
		t.Fatal(copy)
	}
	q.Complete(1, item.ID)
	if q.SetTimeReportValidity(1, item.ID, now.Add(time.Hour)) {
		t.Fatal("completed report revived")
	}
}
