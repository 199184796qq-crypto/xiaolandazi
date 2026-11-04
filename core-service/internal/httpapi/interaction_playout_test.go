package httpapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/coreaudio"
	"livecompanion/core/internal/speechruntime"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRealPCMSourceStartupDelayDoesNotSpendPlaybackTime(t *testing.T) {
	s, item := v2ServerItem(t)
	s.agentWork.SetMode(94, agentwork.ModeAnchor)
	var replyRequests atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ms := 3000
		if r.URL.Path == "/reply" {
			ms = 120
			if replyRequests.Add(1) > 1 {
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
		}
		raw := lifecycleWAV(ms)
		if r.URL.Path != "/reply" {
			for i := 44; i+1 < len(raw); i += 2 {
				at := (i - 44) / 48
				if at < 500 || at >= 900 {
					binary.LittleEndian.PutUint16(raw[i:], 5000)
				}
			}
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(raw)
	}))
	hub := audiohub.New()
	client, err := coreaudio.New(hub, "", "")
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	client.SetRoomAudioEngine(s.roomAudio)
	s.SetAudioHub(hub)
	s.SetAudioClient(client, "")
	defer func() { unblock(); client.StopProgram(context.Background(), 94); source.Close() }()
	_, err = client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 94, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "main", AudioURL: source.URL + "/main", DurationMS: 3000, Text: "主线。"}}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		p, _ := client.ProgramSnapshot(context.Background(), 94)
		if p.AcousticReady && len(p.AcousticCuts) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no PCM cut")
		}
		time.Sleep(time.Millisecond)
	}
	s.agentDecisions.ClaimNext(94)
	body, _ := json.Marshal(map[string]any{"decision_id": item.ID, "audio_url": source.URL + "/reply", "reply_text": "回答。"})
	r := httptest.NewRequest("POST", "/dispatch?tenant_id=7", bytes.NewReader(body))
	r.SetPathValue("roomID", "94")
	out := httptest.NewRecorder()
	s.dispatchRoomAudioInteraction(out, r)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	got, _ := s.speechRuntime.Snapshot(94)
	state := s.audioDevState()
	state.mu.Lock()
	ev := state.events[got.Interrupt.SpeechTaskID]
	state.mu.Unlock()
	if got.Interrupt.Status != speechruntime.StatusReady || got.Interrupt.StartedAt != nil || ev.Status != "" {
		t.Fatal("real premature Playing not reproduced", got, ev)
	}

	// The old watchdog incorrectly failed this 120ms clip after 4.12s.
	time.Sleep(4400 * time.Millisecond)
	still, _ := s.speechRuntime.Snapshot(94)
	if still.Interrupt.Status != speechruntime.StatusReady || still.Interrupt.StartedAt != nil {
		t.Fatal("startup wait counted as playback", still)
	}
	unblock()
	deadline = time.Now().Add(4 * time.Second)
	for {
		if _, exists := s.agentDecisions.Get(94, item.ID); !exists {
			history := s.agentDecisions.Snapshot(94).PlayedHistory
			receipt, err := client.InteractionPlaybackSnapshot(context.Background(), 94, got.Interrupt.SpeechTaskID)
			if err != nil || receipt.StartedAt.IsZero() || receipt.Event.Status != "COMPLETED" || len(history) != 1 {
				t.Fatal("real EOF not accounted once", receipt, history, err)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("released source did not start/complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type ledgerPlaybackClient struct {
	fakeAudioTaskClient
	receipt audioout.InteractionPlaybackReceipt
}

func (c *ledgerPlaybackClient) InteractionPlaybackSnapshot(context.Context, int64, string) (audioout.InteractionPlaybackReceipt, error) {
	return c.receipt, nil
}

func TestPCMEOFReconcilesLostListenerWithoutInventingOrDuplicatingPlayback(t *testing.T) {
	for _, scenario := range []string{"completed", "wrong_room", "wrong_task", "wrong_decision", "old_run", "replacement", "paused", "no_receipt"} {
		t.Run(scenario, func(t *testing.T) {
			s, item := v2ServerItem(t)
			s.agentDecisions.ClaimNext(94)
			if !s.agentDecisions.BindPlayback(94, item.ID, "voice") {
				t.Fatal("bind")
			}
			at := time.Now().Add(-10 * time.Second)
			until := at.Add(time.Second)
			_, _ = s.speechRuntime.Update(94, speechruntime.UpdateInput{Track: speechruntime.TrackInterrupt, Status: speechruntime.StatusReady, DecisionID: item.ID, SpeechTaskID: "voice", DurationMS: 120, PreparedUntil: &until})
			event := audioout.PlaybackEvent{RoomID: 94, SpeechTaskID: "voice", SessionID: "live", ReceiverID: "core-pcm-output", Status: "COMPLETED", OccurredAt: at.Add(time.Second), DurationMS: 120}
			c := &ledgerPlaybackClient{receipt: audioout.InteractionPlaybackReceipt{RoomID: 94, TaskID: "voice", DecisionID: item.ID, StartedAt: at, Event: event}}
			s.SetAudioClient(c, "")
			state := s.audioDevState()
			state.interactions["voice"] = &audioInteractionMeta{RoomID: 94, DecisionID: item.ID, RuntimeEpoch: s.currentInteractionEpoch(94)}
			switch scenario {
			case "wrong_room":
				c.receipt.RoomID = 95
			case "wrong_task":
				c.receipt.TaskID = "other"
			case "wrong_decision":
				c.receipt.DecisionID = "other"
			case "old_run":
				state.interactions["voice"].RuntimeEpoch = "old"
			case "replacement":
				_, _ = s.speechRuntime.Update(94, speechruntime.UpdateInput{Track: speechruntime.TrackInterrupt, Status: speechruntime.StatusReady, DecisionID: "new", SpeechTaskID: "new"})
			case "paused":
				_, _ = s.speechRuntime.Update(94, speechruntime.UpdateInput{Track: speechruntime.TrackInterrupt, Status: speechruntime.StatusPaused, DecisionID: item.ID, SpeechTaskID: "voice"})
			case "no_receipt":
				c.receipt.Event = audioout.PlaybackEvent{}
				c.receipt.StartedAt = time.Time{}
			}
			s.reconcileInteractionPCM(94, "voice", item.ID)
			s.reconcileInteractionPCM(94, "voice", item.ID)
			history := s.agentDecisions.Snapshot(94).PlayedHistory
			if scenario == "completed" {
				got, _ := s.speechRuntime.Snapshot(94)
				if len(history) != 1 || got.Interrupt.Status != speechruntime.StatusCompleted {
					t.Fatal("lost EOF not recovered once", history, got)
				}
			} else if len(history) != 0 {
				t.Fatal("unproven or foreign EOF counted", history)
			}
		})
	}
}

func TestFirstPCMRejectsExpiredPreparationWithoutRenewingIt(t *testing.T) {
	s, item := v2ServerItem(t)
	s.agentDecisions.ClaimNext(94)
	s.agentDecisions.BindPlayback(94, item.ID, "voice")
	s.SetAudioClient(&fakeAudioTaskClient{}, "")
	state := s.audioDevState()
	state.interactions["voice"] = &audioInteractionMeta{RoomID: 94, DecisionID: item.ID, RuntimeEpoch: s.currentInteractionEpoch(94)}
	until := time.Now().Add(-time.Millisecond)
	_, _ = s.speechRuntime.Update(94, speechruntime.UpdateInput{Track: speechruntime.TrackInterrupt, Status: speechruntime.StatusReady, DecisionID: item.ID, SpeechTaskID: "voice", PreparedUntil: &until})
	if err := s.validatePCMInteractionStart(context.Background(), audioout.PlaybackEvent{RoomID: 94, SpeechTaskID: "voice", OccurredAt: time.Now()}); err == nil {
		t.Fatal("started expired preparation")
	}
	got, _ := s.speechRuntime.Snapshot(94)
	if got.Interrupt.StartedAt != nil || !got.Interrupt.PreparedUntil.Equal(until) {
		t.Fatal("preflight renewed playback", got)
	}
}
