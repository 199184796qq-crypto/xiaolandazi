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
		Track:          TrackInterrupt,
		Status:         StatusPlaying,
		QuestionText:   "什么时候发货",
		ReplyText:      "今天下单按顺序安排发货。这个点说清楚了，咱们接着看产品。",
		ResumeStrategy: "BRIDGE",
		BridgeText:     "这个点说清楚了，咱们接着看产品。",
		BridgeUsed:     true,
		Source:         "question_bucket",
		DecisionID:     "d-12",
		MissionID:      "m-12",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Interrupt.QuestionText != "什么时候发货" || snapshot.Interrupt.ResumeStrategy != "BRIDGE" || !snapshot.Interrupt.BridgeUsed || snapshot.Interrupt.BridgeText != "这个点说清楚了，咱们接着看产品。" {
		t.Fatalf("unexpected interrupt: %#v", snapshot.Interrupt)
	}
	if snapshot.Interrupt.DecisionID != "d-12" || snapshot.Interrupt.MissionID != "m-12" {
		t.Fatalf("decision/mission trace ids lost: %#v", snapshot.Interrupt)
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

func TestInterruptTimelineTracksPlaybackProgress(t *testing.T) {
	now := time.Date(2026, 9, 29, 16, 0, 0, 0, time.UTC)
	registry := newRegistry(func() time.Time { return now })
	startedAt := now
	text := "像西藏这种偏远地区，快递确实可能受限。建议下单前先跟客服确认，别白跑一趟。"
	first, err := registry.Update(18, UpdateInput{
		Track:      TrackInterrupt,
		Status:     StatusPlaying,
		ReplyText:  text,
		Text:       text,
		DurationMS: 9000,
		StartedAt:  &startedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Interrupt.Timeline) < 2 {
		t.Fatalf("timeline segments=%d want at least 2: %#v", len(first.Interrupt.Timeline), first.Interrupt.Timeline)
	}
	if first.Interrupt.CurrentSegment == nil || first.Interrupt.CurrentSegment.Index != 0 {
		t.Fatalf("unexpected initial segment: %#v", first.Interrupt.CurrentSegment)
	}

	now = now.Add(5 * time.Second)
	snapshot, err := registry.Snapshot(18)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Interrupt.CurrentMS != 5000 {
		t.Fatalf("current_ms=%d want 5000", snapshot.Interrupt.CurrentMS)
	}
	if snapshot.Interrupt.CurrentSegment == nil || snapshot.Interrupt.CurrentSegment.Index == 0 {
		t.Fatalf("playback should have advanced to a later segment: %#v", snapshot.Interrupt.CurrentSegment)
	}
	if snapshot.Interrupt.CurrentSegment.StartMS > snapshot.Interrupt.CurrentMS || snapshot.Interrupt.CurrentSegment.EndMS < snapshot.Interrupt.CurrentMS {
		t.Fatalf("current segment does not contain progress: current=%d segment=%#v", snapshot.Interrupt.CurrentMS, snapshot.Interrupt.CurrentSegment)
	}
}
