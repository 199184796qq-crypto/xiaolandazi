package coreaudio

import (
	"context"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/roomaudio"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestActualPCMTrackEndInterruptCompletesAndReturnsToNextTrack(t *testing.T) {
	client, _ := newTestClient(t, 600)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		duration := 600
		if r.URL.Path == "/b" {
			duration = 2000
		}
		if r.URL.Path == "/answer" {
			duration = 200
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(testWAV(duration))
	}))
	defer server.Close()
	engine := roomaudio.New()
	client.SetRoomAudioEngine(engine)
	captain := messagecaptain.New()
	client.SetMessageCaptain(captain)
	started := make(chan messagecaptain.Message, 2)
	captain.Register("test-start", func(_ context.Context, m messagecaptain.Message) error {
		if m.Kind == messagecaptain.MainlineStarted {
			started <- m
		}
		return nil
	})
	frames, unsubscribe, err := engine.Subscribe(91)
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	start, err := client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 91, SessionID: "phase1-pcm-test", Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "a", Text: "第一轨完整句。", AudioURL: server.URL + "/a", DurationMS: 600, Timeline: []audioout.ProgramTimelineSegment{{SegmentID: "a1", Index: 1, EndMS: 600, Text: "第一轨完整句。", SafeCut: true}}}, {ID: "b", Text: "回归后的下一完整单元。", AudioURL: server.URL + "/b", DurationMS: 2000}}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.StopProgram(context.Background(), 91)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("no actual output start broadcast")
	}
	snapshot, err := client.ProgramSnapshot(context.Background(), 91)
	if err != nil || snapshot.ActualStartedAt.IsZero() || len(snapshot.FutureWindow) != 2 || snapshot.FutureWindow[1].OffsetMS != 600 {
		t.Fatal("continuous stream snapshot missing", snapshot, err)
	}
	cut := 600
	answer, err := client.InsertTestProgramInteraction(context.Background(), audioout.InsertInteractionInput{RoomID: 91, ExpectedTaskID: start.Task.ID, AudioURL: server.URL + "/answer", Text: "完整问题回答。", SwitchAtMS: &cut, ResumeOffsetMS: &cut})
	if err != nil || !answer.Suspended {
		t.Fatal("track end cut lost to mainline advance", answer, err)
	}
	deadline := time.After(2 * time.Second)
	interruptFrames := 0
	resumed := false
	for !resumed {
		select {
		case frame := <-frames:
			if frame.Source == roomaudio.SourceInterrupt {
				interruptFrames++
			}
			current, _ := client.ProgramSnapshot(context.Background(), 91)
			if current.TrackID == "b" && !current.Suspended && frame.Source == roomaudio.SourceMainline {
				resumed = true
			}
		case <-deadline:
			t.Fatal("did not return to next complete track")
		}
	}
	if interruptFrames < 10 {
		t.Fatalf("answer truncated: frames=%d", interruptFrames)
	}
	select {
	case <-started:
		t.Fatal("next track restarted opening observation")
	default:
	}
}

func TestCreationAndStaleRunDoNotStartObservation(t *testing.T) {
	client, program, _, _ := sequentialFixture(t)
	if !program.ActualStartedAt.IsZero() {
		t.Fatal("program creation counted as real sound")
	}
	client.recordActualStart(program, "stale-task")
	if !program.ActualStartedAt.IsZero() {
		t.Fatal("late old task started observation")
	}
	client.recordActualStart(program, program.CurrentTaskID)
	first := program.ActualStartedAt
	client.recordActualStart(program, program.CurrentTaskID)
	if first.IsZero() || !first.Equal(program.ActualStartedAt) {
		t.Fatal("actual start renewed per PCM frame")
	}
}
