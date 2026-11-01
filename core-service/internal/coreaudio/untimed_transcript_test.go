package coreaudio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/roomaudio"
)

func TestUntimedMainlinePreservesOriginalTextWithoutGeneratingCaptions(t *testing.T) {
	client, _ := newTestClient(t, 1000)
	engine := roomaudio.New()
	client.SetRoomAudioEngine(engine)
	frames, unsubscribe, _ := engine.Subscribe(902)
	defer unsubscribe()
	audio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(testWAV(1000)) }))
	defer audio.Close()
	snapshot, err := client.StartProgram(t.Context(), audioout.StartProgramInput{RoomID: 902, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "untimed", Text: "这里只是没有时间轴的完整原文。", AudioURL: audio.URL, DurationMS: 1000, UntimedTranscript: true}}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.StopProgram(context.Background(), 902)
	if snapshot.TrackText != "这里只是没有时间轴的完整原文。" || len(snapshot.Timeline) != 0 {
		t.Fatal(snapshot)
	}
	select {
	case frame := <-frames:
		if len(frame.PCM) == 0 {
			t.Fatal("no audio")
		}
	case <-time.After(time.Second):
		t.Fatal("untimed mainline did not play")
	}
	if feed := engine.Snapshot(902).SpeechFeed; feed.Current != nil || feed.Next != nil {
		t.Fatal("original text became timed captions", feed)
	}
}
