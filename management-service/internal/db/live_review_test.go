package db

import (
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestBuildLiveReviewSummaryRebuildsSessionFromArchive(t *testing.T) {
	start := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Minute)
	resume := end.Add(30 * time.Minute)
	finalEnd := resume.Add(45 * time.Minute)

	events := []model.LiveReviewEvent{
		{RoomID: 11, EventType: "session_start", OccurredAt: start},
		{RoomID: 11, EventType: "member", OccurredAt: start.Add(time.Minute)},
		{RoomID: 11, EventType: "chat", Content: "什么时候发货？", Nickname: "A", OccurredAt: start.Add(2 * time.Minute)},
		{RoomID: 11, EventType: "chat", Content: "什么时候发货", Nickname: "B", OccurredAt: start.Add(3 * time.Minute)},
		{RoomID: 11, EventType: "like", PayloadJSON: `{"count":5}`, OccurredAt: start.Add(4 * time.Minute)},
		{RoomID: 11, EventType: "session_end", OccurredAt: end},
		{RoomID: 11, EventType: "session_resume", OccurredAt: resume},
		{RoomID: 11, EventType: "order_signal", OccurredAt: resume.Add(time.Minute)},
		{RoomID: 11, EventType: "session_end", OccurredAt: finalEnd},
	}

	summary := buildLiveReviewSummary(11, start, events)
	if summary.InterruptedSeconds != int64((30*time.Minute).Seconds()) {
		t.Fatalf("interrupted_seconds=%d", summary.InterruptedSeconds)
	}
	wantActive := int64((2*time.Hour + 15*time.Minute).Seconds())
	if summary.ActiveSeconds != wantActive {
		t.Fatalf("active_seconds=%d want=%d", summary.ActiveSeconds, wantActive)
	}
	if summary.Entries != 1 || summary.Chats != 2 || summary.Likes != 5 || summary.OrderSignals != 1 {
		t.Fatalf("unexpected counts: %#v", summary)
	}
	if len(summary.QuestionGroups) != 1 || summary.QuestionGroups[0].Count != 2 {
		t.Fatalf("question groups=%#v", summary.QuestionGroups)
	}
}
