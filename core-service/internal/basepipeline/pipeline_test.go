package basepipeline

import (
	"testing"
	"time"

	"livecompanion/core/internal/model"
	"livecompanion/core/internal/questionqueue"
	"livecompanion/core/internal/roombrain"
)

func TestBasePipelineKeepsQuestionProcessingWithoutPaidAI(t *testing.T) {
	brain := roombrain.NewManager()
	questions := questionqueue.New()
	pipeline := New(brain, questions, nil)

	pipeline.Handle(model.RoomEvent{
		RoomID:     12,
		EventType:  "session_start",
		OccurredAt: time.Now().UTC(),
	})
	signal := pipeline.Handle(model.RoomEvent{
		ID:         100,
		RoomID:     12,
		EventType:  "chat",
		UserID:     "u-1",
		Content:    "什么时候发货？",
		OccurredAt: time.Now().UTC(),
	})

	if !signal.IsQuestion {
		t.Fatalf("expected free base pipeline to identify question: %#v", signal)
	}
	if signal.Topic == "" {
		t.Fatal("expected free base pipeline to assign rough question topic")
	}
	items := questions.List(12)
	if len(items) != 1 || items[0].Question != "什么时候发货？" {
		t.Fatalf("question queue=%#v", items)
	}

	view, err := brain.Snapshot(12)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Intelligence.TopTopics) == 0 {
		t.Fatalf("expected free clustering to update without paid AI: %#v", view.Intelligence.TopTopics)
	}
}
