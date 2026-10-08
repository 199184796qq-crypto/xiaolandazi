package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"testing"
	"time"
)

type cutPlannerClient struct {
	*fakeAudioTaskClient
	program audioout.RoomProgramSnapshot
}

func (c *cutPlannerClient) ProgramSnapshot(context.Context, int64) (audioout.RoomProgramSnapshot, error) {
	return c.program, nil
}

func TestCutPlannerBroadcastsOnlySafeActiveMainlineOffers(t *testing.T) {
	client := &cutPlannerClient{fakeAudioTaskClient: &fakeAudioTaskClient{}}
	s := New(nil, nil, nil, nil, nil, "test", "")
	s.SetAudioClient(client, "")
	client.program = audioout.RoomProgramSnapshot{RoomID: 7, ProgramID: "program", Running: true, CurrentMS: 1000, Task: &audioout.SpeechTask{ID: "mainline", DurationMS: 50000}, SafePoints: []audioout.ProgramSafePoint{{CutMS: 16000, Grade: "A", Score: 90}, {CutMS: 49000, Grade: "A", Score: 95}}}
	var offers []messagecaptain.Message
	s.MessageCaptain().Register("observer", func(_ context.Context, m messagecaptain.Message) error {
		if m.Kind == messagecaptain.CutOffered {
			offers = append(offers, m)
		}
		return nil
	})
	raw, _ := json.Marshal(messagecaptain.CutRequest{RequestID: "intent", Epoch: "scene", LeadMS: 15000})
	message := messagecaptain.Message{Kind: messagecaptain.CutRequested, RoomID: 7, OccurredAt: time.Now(), Payload: raw}
	if failures := s.MessageCaptain().Publish(context.Background(), message); len(failures) > 0 {
		t.Fatal(failures)
	}
	if len(offers) != 1 || offers[0].TaskID != "mainline" {
		t.Fatalf("offers=%#v", offers)
	}
	var offer messagecaptain.CutOffer
	json.Unmarshal(offers[0].Payload, &offer)
	if offer.CutMS != 16000 || offer.LeadMS != 15000 {
		t.Fatalf("offer=%#v", offer)
	}
	client.program.Suspended = true
	s.MessageCaptain().Publish(context.Background(), message)
	if len(offers) != 1 {
		t.Fatal("do not offer cuts during another answer")
	}
}
