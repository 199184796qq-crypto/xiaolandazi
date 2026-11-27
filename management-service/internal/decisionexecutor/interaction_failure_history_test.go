package decisionexecutor

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestClaimArchivesOnlyCurrentSessionPlaybackFailures(t *testing.T) {
	now := time.Now().UTC()
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, StartedAt: now.Add(-time.Minute)}
	failures := []interactionFailure{
		{DecisionID: "old", At: now.Add(-2 * time.Minute)},
		{DecisionID: "expired", QuestionText: "什么时候发货", At: now, Reason: "deferred_answer_deadline",
			PreparationReason: "interaction-scheduler: audio_duration_budget"},
	}
	raw, _ := json.Marshal(claimResponse{FailedInteractions: failures})
	store := &preparedTestStore{fakeStore: readyVoiceStore()}
	worker := New(store, &fakeCore{claimRaw: string(raw)}, &fakeAgent{}, &fakeTTS{})
	if _, err := worker.claim(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if len(store.history) != 1 {
		t.Fatalf("expected one failure in this session, got %+v", store.history)
	}
	entry := store.history[0]
	if entry.RuntimeSessionID != session.ID || entry.DecisionID != "expired" || entry.SourceType != "interrupt_failed" ||
		entry.QuestionText != failures[1].QuestionText || !strings.Contains(entry.GeneratedText, "插播时长额度不足") {
		t.Fatalf("incorrect failure entry: %+v", entry)
	}
	query := url.Values{}
	worker.addInteractionFailureCursor(query, session)
	if query.Get("failure_after") != "expired" {
		t.Fatal("persisted receipt was not acknowledged")
	}
	session.ID++
	query = url.Values{}
	worker.addInteractionFailureCursor(query, session)
	if query.Has("failure_after") {
		t.Fatal("previous session leaked into the new session")
	}
}
