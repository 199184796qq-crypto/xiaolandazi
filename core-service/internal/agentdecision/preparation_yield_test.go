package agentdecision

import (
	"testing"
	"time"
)

func TestDeferredReportYieldsToExecutableQuestionWithoutRenewal(t *testing.T) {
	now := time.Now()
	q := NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 20)
	report := q.Enqueue(29, Candidate{Topic: "report", MissionKind: "time_report", Question: "报时", PriorityClass: "P0", Priority: 100, TTLSeconds: 60}).Item
	question := q.Enqueue(29, Candidate{Topic: "question", Question: "发货", PriorityClass: "P1", Priority: 50, TTLSeconds: 60}).Item
	claimed, _ := q.ClaimExpected(29, report.ID)
	if claimed == nil {
		t.Fatal("claim")
	}
	x, ok := q.YieldPreparation(29, report.ID, "no_pcm_cut")
	if !ok || x.AudioPrepared || x.Attempts != 0 || !x.ExpiresAt.Equal(report.ExpiresAt) {
		t.Fatal("yield changed paid/original lifetime", x)
	}
	if h := q.ClaimableHead(29); h == nil || h.ID != question.ID {
		t.Fatal("PENDING P0 blocks ready question", h)
	}
	if _, ok := q.ClaimExpected(29, report.ID); ok {
		t.Fatal("preflight validated different item")
	}
	q.ClaimExpected(29, question.ID)
	q.BindPlayback(29, question.ID, "pcm")
	if _, ok := q.YieldPreparation(29, question.ID, "no_pcm_cut"); ok {
		t.Fatal("yield released dispatched voice")
	}
	if q.ClaimableHead(29) != nil {
		t.Fatal("overlapped active playback")
	}
	if !q.FailPlayback(29, question.ID, "pcm", "source_start_timeout") {
		t.Fatal("failure not recorded")
	}
	if q.FailPlayback(29, question.ID, "pcm", "source_start_timeout") {
		t.Fatal("duplicate failure")
	}
	now = now.Add(5 * time.Second)
	if h := q.ClaimableHead(29); h == nil || h.ID != report.ID {
		t.Fatal("report starved after finite retry", h)
	}
}

func TestReportWaitSixtySecondsDoesNotOccupyQuestionPlaybackLane(t *testing.T) {
	now := time.Now()
	q := NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 20)
	report := q.Enqueue(29, Candidate{Topic: "report", MissionKind: "time_report", Question: "报时", PriorityClass: "P0", Priority: 100, TTLSeconds: 600}).Item
	deadline := report.ExpiresAt
	for i := 0; i < 12; i++ {
		if _, ok := q.ClaimExpected(29, report.ID); !ok {
			t.Fatal("lost report entitlement", i)
		}
		q.YieldPreparation(29, report.ID, "no_pcm_cut")
		question := q.Enqueue(29, Candidate{Topic: time.Duration(i).String(), Question: "观众问题", PriorityClass: "P1", TTLSeconds: 30}).Item
		if question == nil {
			t.Fatal("question rejected")
		}
		if _, ok := q.ClaimExpected(29, question.ID); !ok {
			t.Fatal("report blocked question", i)
		}
		q.BindPlayback(29, question.ID, question.ID)
		if q.BindPlayback(29, question.ID, "other") {
			t.Fatal("playback binding overwritten")
		}
		if _, ok := q.Complete(29, question.ID); !ok {
			t.Fatal("question not complete")
		}
		now = now.Add(5 * time.Second)
	}
	got, ok := q.Get(29, report.ID)
	if !ok || !got.ExpiresAt.Equal(deadline) || got.Attempts != 0 || len(q.Snapshot(29).PlayedHistory) != 12 {
		t.Fatal("report was renewed or lane stalled", got)
	}
}
