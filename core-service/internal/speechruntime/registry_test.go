package speechruntime

import (
	"testing"
	"time"
)

func TestSnapshotDefaultsToIdleTracks(t *testing.T) {
	registry := New()
	snapshot, err := registry.Snapshot(9)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Mainline.Status != StatusIdle || snapshot.Interrupt.Status != StatusIdle {
		t.Fatalf("unexpected empty snapshot: %#v", snapshot)
	}
	if snapshot.Revision != 0 {
		t.Fatalf("revision=%d want 0", snapshot.Revision)
	}
}

func TestUpdateKeepsMainlineAndInterruptIndependent(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })

	snapshot, err := registry.Update(12, UpdateInput{
		Track:  TrackMainline,
		Status: StatusPlaying,
		Text:   "主线正在讲产品卖点",
		Source: "tts",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Mainline.Text != "主线正在讲产品卖点" || snapshot.Mainline.Status != StatusPlaying {
		t.Fatalf("unexpected mainline: %#v", snapshot.Mainline)
	}
	if snapshot.Interrupt.Status != StatusIdle {
		t.Fatalf("interrupt should remain idle: %#v", snapshot.Interrupt)
	}

	now = now.Add(time.Second)
	snapshot, err = registry.Update(12, UpdateInput{
		Track:        TrackInterrupt,
		Status:       StatusPlaying,
		QuestionText: "什么时候发货",
		ReplyText:    "今天下单按顺序安排发货。",
		Source:       "question_bucket",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Interrupt.ReplyText != "今天下单按顺序安排发货。" || snapshot.Interrupt.QuestionText != "什么时候发货" {
		t.Fatalf("unexpected interrupt: %#v", snapshot.Interrupt)
	}
	if snapshot.Mainline.Text != "主线正在讲产品卖点" {
		t.Fatalf("interrupt must not erase mainline: %#v", snapshot.Mainline)
	}
	if snapshot.Revision != 2 {
		t.Fatalf("revision=%d want 2", snapshot.Revision)
	}
}

func TestIdleClearsTrackPayload(t *testing.T) {
	registry := New()
	if _, err := registry.Update(3, UpdateInput{
		Track:     TrackInterrupt,
		Status:    StatusPlaying,
		ReplyText: "临时回答",
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := registry.Update(3, UpdateInput{
		Track:  TrackInterrupt,
		Status: StatusIdle,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Interrupt.Status != StatusIdle || snapshot.Interrupt.ReplyText != "" || snapshot.Interrupt.QuestionText != "" {
		t.Fatalf("idle track should be cleared: %#v", snapshot.Interrupt)
	}
}
