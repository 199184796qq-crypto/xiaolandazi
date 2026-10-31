package httpapi

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/coreaudio"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/roomaudio"
	"livecompanion/core/internal/speechruntime"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func lifecycleWAV(ms int) []byte {
	raw := make([]byte, 44+ms*48)
	copy(raw, "RIFF")
	binary.LittleEndian.PutUint32(raw[4:], uint32(len(raw)-8))
	copy(raw[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(raw[16:], 16)
	binary.LittleEndian.PutUint16(raw[20:], 1)
	binary.LittleEndian.PutUint16(raw[22:], 1)
	binary.LittleEndian.PutUint32(raw[24:], 24000)
	binary.LittleEndian.PutUint32(raw[28:], 48000)
	binary.LittleEndian.PutUint16(raw[32:], 2)
	binary.LittleEndian.PutUint16(raw[34:], 16)
	copy(raw[36:], "data")
	binary.LittleEndian.PutUint32(raw[40:], uint32(len(raw)-44))
	return raw
}
func TestPCMFinishedBroadcastCompletesQueueWithoutReceiverCallback(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ms := 1500
		if r.URL.Path == "/reply" {
			ms = 120
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(lifecycleWAV(ms))
	}))
	defer source.Close()
	server := New(nil, nil, nil, nil, nil, "development", "token")
	server.agentWork.SetPlan(42, 1, "plan")
	server.agentWork.StartAgent(42, 0)
	hub := audiohub.New()
	client, err := coreaudio.New(hub, "", "")
	if err != nil {
		t.Fatal(err)
	}
	engine := roomaudio.New()
	client.SetRoomAudioEngine(engine)
	server.SetRoomAudioEngine(engine)
	server.SetAudioHub(hub)
	server.SetAudioClient(client, "")
	events := make(chan string, 8)
	server.messages.Register("actual-pcm-receipts", func(_ context.Context, m messagecaptain.Message) error {
		if m.Kind == messagecaptain.AudioPlayback {
			var p audioout.PlaybackEvent
			json.Unmarshal(m.Payload, &p)
			events <- p.Status
		}
		return nil
	})
	start, err := client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 42, VersionID: 1, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "one", AudioURL: source.URL + "/main", DurationMS: 1500, Text: "第一句。接下来的主线。", Timeline: []audioout.ProgramTimelineSegment{{SegmentID: "first", Index: 1, EndMS: 300, Text: "第一句。", SafeCut: true}, {SegmentID: "next", Index: 2, StartMS: 300, EndMS: 1500, Text: "接下来的主线。", SafeCut: true}}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.StopTestProgram(context.Background(), 42)
	queued := server.agentDecisions.Enqueue(42, agentdecision.Candidate{Topic: "Q:links", Scene: "product_question", Question: "两链接区别", MaxRetries: 2}).Item
	item, ok := server.agentDecisions.ClaimNext(42)
	if !ok {
		t.Fatal("claim")
	}
	cut := 300
	inserted, err := client.InsertTestProgramInteraction(context.Background(), audioout.InsertInteractionInput{RoomID: 42, ExpectedTaskID: start.Task.ID, AudioURL: source.URL + "/reply", Text: "回答。", SwitchAtMS: &cut, ResumeOffsetMS: &cut})
	if err != nil {
		t.Fatal(err)
	}
	task := inserted.Task
	server.agentDecisions.BindPlayback(42, item.ID, task.ID)
	state := server.audioDevState()
	state.mu.Lock()
	state.interactions[task.ID] = &audioInteractionMeta{RoomID: 42, RuntimeEpoch: server.currentInteractionEpoch(42), DecisionID: item.ID, ReplyText: "回答。", WaitForMainline: true}
	state.mu.Unlock()
	server.speechRuntime.Update(42, speechruntime.UpdateInput{Track: speechruntime.TrackInterrupt, Status: speechruntime.StatusPlaying, DecisionID: item.ID, SpeechTaskID: task.ID, DurationMS: task.DurationMS})
	server.scheduleRoomAudioInteractionCompletion(42, *task, item.ID, true)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, exists := server.agentDecisions.Get(42, queued.ID); !exists {
			history := server.agentDecisions.Snapshot(42).PlayedHistory
			if len(history) != 1 {
				t.Fatal("completion was not accounted exactly once", history)
			}
			for _, want := range []string{"STARTING", "PLAYING", "COMPLETED"} {
				select {
				case got := <-events:
					if got != want {
						t.Fatal("actual PCM lifecycle out of order", got, want)
					}
				default:
					t.Fatal("actual PCM receipt missing", want)
				}
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("PCM EOF returned to mainline but left answer queued")
}
