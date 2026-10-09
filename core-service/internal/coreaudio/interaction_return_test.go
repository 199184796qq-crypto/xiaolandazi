package coreaudio

import (
	"context"
	"encoding/json"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/roomaudio"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompleteUnitCoveragePreservesNewPricesAndQualifiers(t *testing.T) {
	for _, test := range []struct {
		answer, unit string
		want         bool
	}{
		{"一只85.5元。", "一只85.5元。", true},
		{"一只85.5元。", "一只85.5元，运费另算。", false},
		{"一只85.5元。", "一只95.5元。", false},
		{"今天新鲜制作。", "当天卤制，现炸出锅。", false},
	} {
		if got := audioout.CompleteUnitCovered(test.answer, test.unit); got != test.want {
			t.Fatalf("coverage got=%v want=%v unit=%s", got, test.want, test.unit)
		}
	}
	p := &programState{ID: "run", Tracks: []programTrack{{ID: "a", Timeline: []audioout.ProgramTimelineSegment{{SegmentID: "1", StartMS: 0, EndMS: 100, Text: "价格是", SafeCut: false}, {SegmentID: "2", StartMS: 100, EndMS: 200, Text: "85元，运费另算。", SafeCut: true}, {SegmentID: "3", StartMS: 200, EndMS: 400, Text: "接着说保存方法。", SafeCut: true}}}}}
	state := buildInteractionReturn(p, "task", "价格是85元，运费另算。", 0, 2000)
	if len(state.snapshot.SkippedUnits) != 1 || state.resumeMS != 200 {
		t.Fatal("complete fragments not grouped before dedup", state)
	}
}

func TestDeferredBridgeSharesTaskAndReturnsWithoutWaiting(t *testing.T) {
	for _, bridge := range []bool{false, true} {
		t.Run(map[bool]string{false: "short_direct", true: "prepared_bridge"}[bridge], func(t *testing.T) {
			client, _ := newTestClient(t, 400)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				duration := 400
				if r.URL.Path == "/b" {
					duration = 2000
				}
				if r.URL.Path == "/answer" {
					duration = 1400
				}
				w.Write(testWAV(duration))
			}))
			defer server.Close()
			engine := roomaudio.New()
			client.SetRoomAudioEngine(engine)
			captain := messagecaptain.New()
			client.SetMessageCaptain(captain)
			frames, unsubscribe, err := engine.Subscribe(92)
			if err != nil {
				t.Fatal(err)
			}
			defer unsubscribe()
			start, err := client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 92, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "a", AudioURL: server.URL + "/a", Text: "切点前完整内容。", DurationMS: 400}, {ID: "b", AudioURL: server.URL + "/b", Text: "实际回归内容。", DurationMS: 2000}}})
			if err != nil {
				t.Fatal(err)
			}
			defer client.StopProgram(context.Background(), 92)
			cut := 400
			done := make(chan error, 1)
			go func() {
				_, err := client.InsertTestProgramInteraction(context.Background(), audioout.InsertInteractionInput{RoomID: 92, ExpectedTaskID: start.Task.ID, DecisionID: "d1", DeferredReturn: true, AudioURL: server.URL + "/answer", Text: "完整的核心互动回答。", SwitchAtMS: &cut, ResumeOffsetMS: &cut})
				done <- err
			}()
			var plan audioout.InteractionReturnSnapshot
			until := time.Now().Add(time.Second)
			for time.Now().Before(until) {
				plan, err = client.InteractionReturnSnapshot(context.Background(), 92, "")
				if err == nil {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err != nil || plan.DecisionID != "d1" || plan.Units[0].TrackID != "b" {
				t.Fatal("no confirmed pre-cut return reservation", plan, err)
			}
			if bridge {
				pcm := make([]byte, roomaudio.PCMBytesPerFrame*10)
				for i := range pcm {
					pcm[i] = 7
				}
				raw, _ := json.Marshal(BridgeReady{TaskID: plan.TaskID, Token: plan.Token, Text: "刚才说到用法，咱们接着讲。", PCM: pcm, DurationMS: 200})
				if errors := captain.Publish(context.Background(), messagecaptain.Message{Kind: messagecaptain.ReturnBridgeReady, RoomID: 92, RunID: plan.ProgramID, TaskID: plan.TaskID, Payload: raw}); len(errors) > 0 {
					t.Fatal(errors)
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if _, err := client.CompleteProgramInteraction(context.Background(), 92, plan.TaskID); err != nil {
				t.Fatal(err)
			}
			current, _ := client.ProgramSnapshot(context.Background(), 92)
			if !current.Suspended {
				t.Fatal("early receiver terminal released shared interaction")
			}
			interruptFrames, bridgeFrames := 0, 0
			returned := false
			deadline := time.After(3 * time.Second)
			for !returned {
				select {
				case frame := <-frames:
					if frame.Source == roomaudio.SourceInterrupt {
						interruptFrames++
						if len(frame.PCM) > 0 && frame.PCM[0] == 7 {
							bridgeFrames++
						}
					}
					current, _ = client.ProgramSnapshot(context.Background(), 92)
					if current.TrackID == "b" && !current.Suspended && frame.Source == roomaudio.SourceMainline {
						returned = true
					}
				case <-deadline:
					t.Fatal("did not finish answer/bridge/return")
				}
			}
			if interruptFrames < 70 || (bridge && bridgeFrames != 10) || (!bridge && bridgeFrames != 0) {
				t.Fatalf("core frames=%d bridge=%d", interruptFrames, bridgeFrames)
			}
			if _, err := client.InteractionReturnSnapshot(context.Background(), 92, plan.TaskID); err == nil {
				t.Fatal("late bridge still accepted after mainline return")
			}
		})
	}
}

func TestReturnBridgeRejectsChangedRunTokenAndDeadline(t *testing.T) {
	client, p, _, _ := sequentialFixture(t)
	p.Suspended = true
	p.InteractionReturn = buildInteractionReturn(p, p.CurrentTaskID, "核心回答。", 0, 3000)
	p.InteractionReturn.snapshot.Deadline = client.now().Add(-time.Second)
	raw, _ := json.Marshal(BridgeReady{TaskID: p.CurrentTaskID, Token: p.InteractionReturn.snapshot.Token, Text: "接着说刚才的用法。", PCM: make([]byte, roomaudio.PCMBytesPerFrame), DurationMS: 20})
	if err := client.handleReturnMessage(context.Background(), messagecaptain.Message{Kind: messagecaptain.ReturnBridgeReady, RoomID: p.RoomID, RunID: p.ID, Payload: raw}); err == nil {
		t.Fatal("late bridge accepted")
	}
	p.InteractionReturn.snapshot.Deadline = client.now().Add(time.Second)
	if err := client.handleReturnMessage(context.Background(), messagecaptain.Message{Kind: messagecaptain.ReturnBridgeReady, RoomID: p.RoomID, RunID: "old-run", Payload: raw}); err == nil {
		t.Fatal("old run bridge accepted")
	}
}
