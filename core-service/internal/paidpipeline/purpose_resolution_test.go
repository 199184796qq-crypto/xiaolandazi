package paidpipeline

import (
	"context"
	"encoding/json"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/roombrain"
	"livecompanion/core/internal/strategycenter"
	"testing"
	"time"
)

type blockingPurpose struct{ started, release chan struct{} }

func (r blockingPurpose) Resolve(ctx context.Context, _ model.RoomEvent, _ int64) (messagecaptain.PurposeResult, error) {
	close(r.started)
	select {
	case <-r.release:
		return messagecaptain.PurposeResult{Question: true, Relevant: true, Topic: "PURPOSE:courier", PurposeID: "P01", Source: "model"}, nil
	case <-ctx.Done():
		return messagecaptain.PurposeResult{}, ctx.Err()
	}
}
func TestPurposeWorkerDoesNotBlockCollectorAndDiscardsCancelledRun(t *testing.T) {
	r, q := agentwork.New(), agentdecision.New()
	r.Set(7, agentwork.StateWorking)
	p := New(r, q, strategycenter.New())
	c := messagecaptain.New()
	p.SetMessageCaptain(c)
	started, release := make(chan struct{}), make(chan struct{})
	p.SetPurposeResolver(blockingPurpose{started, release})
	event := model.RoomEvent{ID: 100, RoomID: 7, TenantID: 2, EventType: "chat", Content: "发哪个快递", OccurredAt: time.Now()}
	begin := time.Now()
	p.Handle(event, basepipeline.Signal{Content: event.Content, IsQuestion: true})
	if time.Since(begin) > 100*time.Millisecond {
		t.Fatal("collector waited for purpose model")
	}
	<-started
	r.StopAgent(7, agentwork.StopReasonLiveFinished)
	close(release)
	time.Sleep(20 * time.Millisecond)
	if len(q.Snapshot(7).Queue) > 0 {
		t.Fatal("cancelled run accepted late analysis")
	}
}
func TestActualAudioObservationBlocksAllAdmissionButKeepsAggregation(t *testing.T) {
	r, q := agentwork.New(), agentdecision.New()
	r.Set(7, agentwork.StateWorking)
	r.SetMode(7, agentwork.ModeAnchor)
	p := New(r, q, strategycenter.New())
	now := time.Now().UTC()
	p.now = func() time.Time { return now }
	actual := time.Time{}
	p.SetAudioStartedReader(func(int64) time.Time { return actual })
	for _, kind := range []string{"reply_question", "complaint", "conversion_signal", "warmup", "welcome_named", "reply_like", "reply_follow", "reply_chat"} {
		candidate := agentdecision.Candidate{MissionKind: kind}
		if p.decorateCandidate(7, 2, &candidate) {
			t.Fatal("admitted before sound", kind)
		}
	}
	actual = now
	now = now.Add(119 * time.Second)
	if p.InteractionsAllowed(7, 2) {
		t.Fatal("observation counted from room creation")
	}
	now = now.Add(time.Second)
	if !p.InteractionsAllowed(7, 2) {
		t.Fatal("120 second sound observation never ended")
	}
	brain := roombrain.NewManager()
	event := model.RoomEvent{ID: 4, RoomID: 7, TenantID: 2, EventType: "chat", Content: "生油还是熟油", OccurredAt: now}
	brain.Ingest(event)
	c := messagecaptain.New()
	brain.RegisterPurposeMessages(c, p.cutEpoch)
	p.SetMessageCaptain(c)
	result := messagecaptain.PurposeResult{Event: event, Epoch: p.cutEpoch(7), Question: true, Relevant: true, Topic: "PURPOSE:raw-cooked"}
	raw, _ := json.Marshal(result)
	c.Publish(context.Background(), messagecaptain.Message{Kind: messagecaptain.PurposeResolved, RoomID: 7, Payload: raw})
	view, _ := brain.Snapshot(7)
	if view.Intelligence.SessionChats != 1 || len(view.Intelligence.TopTopics) != 1 || view.Intelligence.TopTopics[0].Questions[0].Content != event.Content {
		t.Fatal("analysis duplicated aggregate or lost original", view.Intelligence)
	}
}
