package httpapi

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"livecompanion/management/internal/coreclient"
)

func readyQueueFixture() (*recordingReadyQueue, *[]string) {
	var sent []string
	q := &recordingReadyQueue{startedAt: time.Now()}
	q.combine = func(_ context.Context, units []recordingReadyUnit, _ int) (coreclient.ProgramRefreshTrack, int, error) {
		return units[0].Track, 1, nil
	}
	q.start = func(_ context.Context, track coreclient.ProgramRefreshTrack) (coreclient.ProgramRefreshSnapshot, error) {
		sent = append(sent, track.ID)
		return coreclient.ProgramRefreshSnapshot{ProgramID: "same", VersionID: 208}, nil
	}
	q.append = func(_ context.Context, _ string, track coreclient.ProgramRefreshTrack) (uint64, error) {
		sent = append(sent, track.ID)
		return 0, nil
	}
	q.buffer = func(context.Context, string) error { return nil }
	return q, &sent
}

func readyQueueUnit(id string, ms int) recordingReadyUnit {
	return recordingReadyUnit{Text: id, Track: coreclient.ProgramRefreshTrack{ID: id, AudioURL: "ready://" + id, DurationMS: ms}}
}

func TestRecordingReadyQueueShortScriptStartsCompleteFirstPassWithoutSlicePanic(t *testing.T) {
	for _, count := range []int{1, 2} {
		q, sent := readyQueueFixture()
		for i := 0; i < count; i++ {
			q.pending = append(q.pending, readyQueueUnit(string(rune('a'+i)), 10000))
		}
		if err := q.flush(context.Background(), false); err != nil || len(*sent) != 0 || len(q.pending) != count {
			t.Fatal(err, *sent, q.pending)
		}
		if err := q.flush(context.Background(), true); err != nil || len(*sent) != count || len(q.pending) != 0 {
			t.Fatal(err, *sent, q.pending)
		}
	}
}

func TestRecordingReadyQueueCachedUnitsUseSameDurationGateAndDrainInOrder(t *testing.T) {
	q, sent := readyQueueFixture()
	for _, id := range []string{"cached-a", "cached-b"} {
		q.pending = append(q.pending, readyQueueUnit(id, 20000))
		if err := q.flush(context.Background(), false); err != nil {
			t.Fatal(err)
		}
	}
	if len(*sent) != 0 {
		t.Fatal("cache bypassed startup buffer", *sent)
	}
	q.pending = append(q.pending, readyQueueUnit("new-c", 20000))
	if err := q.flush(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*sent, []string{"cached-a", "cached-b", "new-c"}) || q.programID != "same" || len(q.pending) != 0 {
		t.Fatal(*sent, q)
	}
}

func TestRecordingReadyQueueUnknownAppendRetainsSameTrackWithoutRecombine(t *testing.T) {
	q, _ := readyQueueFixture()
	q.programID = "same"
	q.pending = []recordingReadyUnit{readyQueueUnit("once", 1000)}
	combines, attempts := 0, 0
	q.combine = func(_ context.Context, units []recordingReadyUnit, _ int) (coreclient.ProgramRefreshTrack, int, error) {
		combines++
		return units[0].Track, 1, nil
	}
	q.append = func(_ context.Context, _ string, track coreclient.ProgramRefreshTrack) (uint64, error) {
		attempts++
		if track.ID != "once" {
			t.Fatal(track)
		}
		if attempts == 1 {
			return 0, io.EOF
		}
		return 0, nil
	}
	if err := q.flush(context.Background(), false); err != nil || attempts != 2 || combines != 1 || len(q.pending) != 0 {
		t.Fatal(err, attempts, combines, q.pending)
	}
}

func TestRecordingReadyQueueRejectsBadStartAndContinuesFollowingUnit(t *testing.T) {
	q, sent := readyQueueFixture()
	q.pending = []recordingReadyUnit{readyQueueUnit("bad", 1000), readyQueueUnit("good", 1000)}
	start := q.start
	q.start = func(ctx context.Context, track coreclient.ProgramRefreshTrack) (coreclient.ProgramRefreshSnapshot, error) {
		if track.ID == "bad" {
			return coreclient.ProgramRefreshSnapshot{}, beforeMainlineSubmit(errors.New("坏音频未被接收"))
		}
		return start(ctx, track)
	}
	if err := q.flush(context.Background(), true); err != nil || !reflect.DeepEqual(*sent, []string{"good"}) || q.delivered != 1 || len(q.pending) != 0 {
		t.Fatal(err, *sent, q)
	}
}

func TestRecordingReadyQueueCancelledRetryDoesNotConsumeOrRegenerate(t *testing.T) {
	q, _ := readyQueueFixture()
	q.programID = "same"
	q.pending = []recordingReadyUnit{readyQueueUnit("retained", 1000)}
	ctx, cancel := context.WithCancel(context.Background())
	q.append = func(context.Context, string, coreclient.ProgramRefreshTrack) (uint64, error) {
		cancel()
		return 0, io.EOF
	}
	err := q.flush(ctx, false)
	if !errors.Is(err, context.Canceled) || len(q.pending) != 1 || q.blockSeq != 0 || q.pending[0].Track.ID != "retained" {
		t.Fatal(err, q)
	}
}
