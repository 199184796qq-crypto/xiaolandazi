package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	roomstore "livecompanion/core/internal/room"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func v2ServerItem(t *testing.T) (*Server, *agentdecision.Item) {
	t.Helper()
	db, err := sql.Open("phase1-return-room", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := New(roomstore.NewStore(db), nil, nil, nil, nil, "development", "token")
	s.agentWork.SetPlan(94, 3, "plan")
	s.agentWork.StartAgent(94, 0)
	run := s.agentWork.Get(94)
	e := interactionV2Envelope{Protocol: "interaction_v2", Task: "body-task", Epoch: fmt.Sprintf("%s:%d:%d", run.BootID, run.PlanID, run.RunGeneration), Tenant: 7, Room: 94, Plan: 3, Expires: time.Now().Add(time.Minute)}
	raw, _ := json.Marshal(e)
	v := s.agentDecisions.Enqueue(94, agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "body-task", Question: "哪里发货", PurposeKnowledge: raw, TTLSeconds: 60})
	if v.Item == nil {
		t.Fatal(v)
	}
	return s, v.Item
}
func TestInteractionV2DispatchRejectsExpiredClaimedItemBeforeAudio(t *testing.T) {
	s, item := v2ServerItem(t)
	s.agentDecisions.ClaimNext(94)
	e, _ := decodeInteractionV2Item(*item)
	e.Expires = time.Now().Add(-time.Second)
	raw, _ := json.Marshal(e)
	s.agentDecisions.ClearRoom(94)
	item = s.agentDecisions.Enqueue(94, agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "body-task", Question: "问题", PurposeKnowledge: raw, TTLSeconds: 60}).Item
	s.agentDecisions.ClaimNext(94)
	body, _ := json.Marshal(map[string]any{"decision_id": item.ID, "audio_url": "http://unused.invalid/audio.wav", "reply_text": "回答。"})
	r := httptest.NewRequest(http.MethodPost, "/internal/v1/rooms/94/audio/interaction?tenant_id=7", bytes.NewReader(body))
	r.SetPathValue("roomID", "94")
	out := httptest.NewRecorder()
	s.dispatchRoomAudioInteraction(out, r)
	if out.Code != 409 {
		t.Fatal(out.Code, out.Body.String())
	}
	if _, ok := s.agentDecisions.Get(94, item.ID); ok {
		t.Fatal("expired task stayed claimed")
	}
}
func TestInteractionV2RuntimeIdentityGuards(t *testing.T) {
	s, item := v2ServerItem(t)
	if !s.validInteractionV2Item(*item, 94, 7, time.Now()) {
		t.Fatal("current valid item rejected")
	}
	e, _ := decodeInteractionV2Item(*item)
	for _, field := range []string{"epoch", "room", "plan", "deadline"} {
		t.Run(field, func(t *testing.T) {
			bad := e
			switch field {
			case "epoch":
				bad.Epoch = "old"
			case "room":
				bad.Room = 29
			case "plan":
				bad.Plan = 4
			case "deadline":
				bad.Expires = time.Now()
			}
			raw, _ := json.Marshal(bad)
			copy := *item
			copy.PurposeKnowledge = raw
			if s.validInteractionV2Item(copy, 94, 7, time.Now()) {
				t.Fatal("accepted", field)
			}
		})
	}
	s.agentWork.StopAgent(94, agentwork.StopReasonLiveFinished)
	if s.validInteractionV2Item(*item, 94, 7, time.Now()) {
		t.Fatal("stopped runtime accepted")
	}
}
func TestInteractionV2ActualPlaybackEventIsPublishedAndIdempotent(t *testing.T) {
	s, item := v2ServerItem(t)
	s.agentDecisions.ClaimNext(94)
	s.messages = messagecaptain.New()
	var events []string
	s.messages.Register("test", func(_ context.Context, m messagecaptain.Message) error {
		if m.Kind == "interaction.v2.playback" {
			var p struct {
				Decision string `json:"decision_id"`
				Status   string `json:"status"`
			}
			if json.Unmarshal(m.Payload, &p) != nil || p.Decision != item.ID {
				t.Error("receipt identity lost")
			}
			events = append(events, p.Status)
		}
		return nil
	})
	state := &audioDevState{events: map[string]audioout.PlaybackEvent{}, interactions: map[string]*audioInteractionMeta{"audio": {RoomID: 94, DecisionID: item.ID}}}
	for _, status := range []string{"PLAYING", "FAILED", "FAILED"} {
		s.applyAudioInteractionPlaybackEvent(context.Background(), state, audioout.PlaybackEvent{RoomID: 94, SpeechTaskID: "audio", Status: status, OccurredAt: time.Now(), ReceiverID: "core-pcm-output"})
	}
	if len(events) != 2 || events[0] != "PLAYING" || events[1] != "FAILED" {
		t.Fatal(events)
	}
}
