package coreaudio

import (
	"context"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/roomaudio"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestActualTrackReceiptIgnoresStagingResumeAndStaleMirrors(t *testing.T) {
	c, p, _, _ := sequentialFixture(t)
	if len(p.ActualTrackReceipts) != 0 {
		t.Fatal("staging counted")
	}
	c.recordActualTrackStart(p, "stale", "a")
	c.recordActualTrackStart(p, p.CurrentTaskID, "a")
	c.recordActualTrackStart(p, p.CurrentTaskID, "a")
	if len(p.ActualTrackReceipts) != 1 || p.ActualTrackSequence != 1 {
		t.Fatal(p.ActualTrackReceipts)
	}
	c.programs[p.RoomID] = &programState{RoomID: p.RoomID, Running: true}
	c.recordActualTrackStart(p, p.CurrentTaskID, "b")
	if len(p.ActualTrackReceipts) != 1 {
		t.Fatal("late old mirror counted")
	}
}

func TestActualPCMCreatesReceiptWithoutReceiverAcknowledgement(t *testing.T) {
	c, _ := newTestClient(t, 300)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(testWAV(300)) }))
	defer server.Close()
	c.SetRoomAudioEngine(roomaudio.New())
	_, err := c.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 92, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "actual", AudioURL: server.URL, DurationMS: 300}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.StopProgram(context.Background(), 92)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := c.ProgramSnapshot(context.Background(), 92)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.MainlinePlaybackReceipts) > 0 {
			r := snapshot.MainlinePlaybackReceipts[0]
			if r.TrackID != "actual" || r.StartedAt.IsZero() || r.Sequence != 1 {
				t.Fatal(r)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("PCM output did not produce receipt")
}
