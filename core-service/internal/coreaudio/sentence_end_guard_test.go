package coreaudio

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/roomaudio"
)

func TestSentencePCMReservationHoldsNextVoiceAndRestoresCancelledTail(t *testing.T) {
	for _, cut := range []int{600, 615, 999} {
		t.Run((time.Duration(cut) * time.Millisecond).String(), func(t *testing.T) {
			const duration = 1000
			wav := testWAV(duration)
			bytesPerMS := roomaudio.SampleRate * roomaudio.Channels * roomaudio.BytesPerSample / 1000
			for i := 44; i < len(wav); i++ {
				wav[i] = 0x11
				if i >= 44+cut*bytesPerMS {
					wav[i] = 0x22
				}
			}
			audio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(wav) }))
			defer audio.Close()
			client, _ := newTestClient(t, duration)
			engine := roomaudio.New()
			client.SetRoomAudioEngine(engine)
			frames, unsubscribe, _ := engine.Subscribe(99)
			defer unsubscribe()
			start, err := client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 99, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "voices", AudioURL: audio.URL, Text: "第一句。下一句。", DurationMS: duration, Timeline: []audioout.ProgramTimelineSegment{
				{SegmentID: "first", Index: 1, EndMS: cut, Text: "第一句。", SafeCut: true},
				{SegmentID: "next", Index: 2, StartMS: cut, EndMS: duration, Text: "下一句。", SafeCut: true},
			}}}})
			if err != nil {
				t.Fatal(err)
			}
			defer client.StopProgram(context.Background(), 99)
			// Simulate a prepared dispatch whose commit is delayed at the boundary.
			client.mu.Lock()
			program := client.programs[99]
			program.CutWaitingTaskID, program.CutWaitingMS = start.Task.ID, cut
			client.mu.Unlock()
			deadline := time.Now().Add(2 * time.Second)
			for engine.Snapshot(99).MainlineCursorMS < cut && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			held := engine.Snapshot(99).MainlineCursorMS
			time.Sleep(80 * time.Millisecond)
			if held < cut || engine.Snapshot(99).MainlineCursorMS != held {
				t.Fatal("next sentence advanced past the held boundary", held, engine.Snapshot(99))
			}
			var pcm []byte
			for len(frames) > 0 {
				pcm = append(pcm, (<-frames).PCM...)
			}
			if bytes.Count(pcm, []byte{0x11}) != cut*bytesPerMS || bytes.Count(pcm, []byte{0x22}) != 0 {
				t.Fatal("sentence cut lost its ending or leaked the next voice")
			}
			// Cancellation must release the hold and restore every unplayed sample.
			client.mu.Lock()
			program.CutWaitingTaskID, program.CutWaitingMS = "", 0
			client.mu.Unlock()
			for bytes.Count(pcm, []byte{0x22}) < (duration-cut)*bytesPerMS && time.Now().Before(deadline) {
				select {
				case frame := <-frames:
					pcm = append(pcm, frame.PCM...)
				case <-time.After(time.Until(deadline)):
				}
			}
			if bytes.Count(pcm, []byte{0x11}) != cut*bytesPerMS || bytes.Count(pcm, []byte{0x22}) != (duration-cut)*bytesPerMS {
				t.Fatal("cancelled cut failed to restore the remaining physical PCM")
			}
		})
	}
}

func TestLiveCoreCannotBypassSentenceEndWithMissingOrArbitraryCut(t *testing.T) {
	client, _ := newTestClient(t, 1200)
	engine := roomaudio.New()
	client.SetRoomAudioEngine(engine)
	audio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(testWAV(1200)) }))
	defer audio.Close()
	start, err := client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 97, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "whole", AudioURL: audio.URL, Text: "完整句子。", DurationMS: 1200, Timeline: []audioout.ProgramTimelineSegment{{SegmentID: "end", Index: 1, EndMS: 1200, Text: "完整句子。", SafeCut: true}}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.StopProgram(context.Background(), 97)
	middle, resume := 300, 1200
	for _, cut := range []*int{nil, &middle} {
		_, err := client.InsertTestProgramInteraction(context.Background(), audioout.InsertInteractionInput{RoomID: 97, ExpectedTaskID: start.Task.ID, AudioURL: audio.URL, SwitchAtMS: cut, ResumeOffsetMS: &resume})
		if !errors.Is(err, audioout.ErrInteractionSwitchStale) {
			t.Fatal("unverified cut accepted", cut, err)
		}
		current, _ := client.ProgramSnapshot(context.Background(), 97)
		if current.Suspended || current.Task.ID != start.Task.ID {
			t.Fatal("invalid request changed audio", current)
		}
	}
}

func TestPCMCutWaitTimeoutNeverAuthorizesEarlySwitch(t *testing.T) {
	client, _ := newTestClient(t, 1000)
	engine := roomaudio.New()
	client.SetRoomAudioEngine(engine)
	_, _ = engine.StartMainline(98, "sentence")
	_, _ = engine.PublishPCMAt(98, roomaudio.SourceMainline, make([]byte, roomaudio.PCMBytesPerFrame), "sentence", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := client.waitForRoomAudioSwitchPoint(ctx, 98, 1000, time.Millisecond)
	if !errors.Is(err, audioout.ErrInteractionSwitchStale) || engine.Snapshot(98).MainlineCursorMS != 20 {
		t.Fatal("timeout authorized unplayed sentence", err, engine.Snapshot(98))
	}
}
