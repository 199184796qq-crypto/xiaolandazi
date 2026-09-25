package roombrain

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"livecompanion/core/internal/model"
	"livecompanion/core/internal/timeline"
)

func testEvent(roomID int64, eventType, userID, content string, at time.Time, payload map[string]any) model.RoomEvent {
	raw, _ := json.Marshal(payload)
	return model.RoomEvent{
		RoomID: roomID, EventType: eventType, UserID: userID,
		Content: content, OccurredAt: at, Payload: raw,
	}
}

func TestPriceQuestionsBecomeAggregateBucket(t *testing.T) {
	now := time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)
	m := NewManager()
	m.now = func() time.Time { return now }

	m.Ingest(testEvent(1, "room", "", "", now, map[string]any{"online_count": 100}))
	for i := 0; i < 15; i++ {
		m.Ingest(testEvent(1, "chat", "u"+strconv.Itoa(i), "多少钱？", now, nil))
	}
	view, err := m.Snapshot(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Intelligence.TopTopics) == 0 || view.Intelligence.TopTopics[0].Topic != "PRICE" {
		t.Fatalf("topics=%#v", view.Intelligence.TopTopics)
	}
	if !view.Intelligence.PreferAggregateQNA {
		t.Fatalf("expected aggregate QNA: %#v", view.Intelligence)
	}
	if view.Director.Progress != "HOLD_FOR_QNA" {
		t.Fatalf("director=%#v", view.Director)
	}
}

func TestNegativeAfterSaleSuppressesHumanization(t *testing.T) {
	now := time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)
	m := NewManager()
	m.now = func() time.Time { return now }
	m.Ingest(testEvent(2, "chat", "u1", "收到坏了，我要退款投诉，可以吗？", now, nil))
	view, err := m.Snapshot(2)
	if err != nil {
		t.Fatal(err)
	}
	if view.Intelligence.NegativeFeedback30s == 0 {
		t.Fatal("negative feedback not detected")
	}
	if view.Director.Humanization.Enabled {
		t.Fatalf("humanization should be suppressed: %#v", view.Director.Humanization)
	}
}

func TestPinMarksTopicAnsweredAndSpendsDebt(t *testing.T) {
	now := time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)
	m := NewManager()
	m.now = func() time.Time { return now }
	m.Ingest(testEvent(3, "chat", "u1", "多少钱？", now, nil))

	m.RecordPin(3, timeline.Pin{
		At: now, Kind: timeline.PinAnswer,
		Topic: "PRICE", Strategy: "answer.price",
	}, timeline.DebtQuestion, 2*time.Minute)

	view, err := m.Snapshot(3)
	if err != nil {
		t.Fatal(err)
	}
	debt := view.Timeline.Debts["QUESTION"]
	if debt.Value != 0 || !debt.CooldownUntil.After(now) {
		t.Fatalf("debt=%#v", debt)
	}
	found := false
	for _, topic := range view.Intelligence.TopTopics {
		if topic.Topic == "PRICE" && topic.LastAnsweredAt.Equal(now) {
			found = true
		}
	}
	if !found {
		t.Fatalf("price bucket was not marked answered: %#v", view.Intelligence.TopTopics)
	}
}

func TestSessionStartResetsTimelineAndRoomMetricFeedsBrain(t *testing.T) {
	first := time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)
	second := first.Add(30 * time.Minute)
	m := NewManager()
	m.now = func() time.Time { return second }

	m.Ingest(testEvent(9, "chat", "u1", "多少钱？", first, nil))
	m.RecordPin(9, timeline.Pin{
		At: first.Add(time.Minute), Kind: timeline.PinCTA, Strategy: "conversion.old",
	}, timeline.DebtConversion, time.Minute)

	m.Ingest(testEvent(9, "session_start", "", "", second, nil))
	m.Ingest(testEvent(9, "room", "", "", second, map[string]any{"online_count": 321}))

	view, err := m.Snapshot(9)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Timeline.StartedAt.Equal(second) {
		t.Fatalf("session start=%v want=%v", view.Timeline.StartedAt, second)
	}
	if len(view.Timeline.HotPins) != 0 {
		t.Fatalf("old pins leaked into new session: %#v", view.Timeline.HotPins)
	}
	if len(view.Intelligence.TopTopics) != 0 {
		t.Fatalf("old topic buckets leaked into new session: %#v", view.Intelligence.TopTopics)
	}
	if view.Intelligence.OnlineCount != 321 {
		t.Fatalf("online=%d want=321", view.Intelligence.OnlineCount)
	}
}
