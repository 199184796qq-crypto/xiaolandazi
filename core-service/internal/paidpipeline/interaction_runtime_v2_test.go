package paidpipeline

import (
	"encoding/json"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/model"
	"testing"
	"time"
)

func TestProductionHTTPResolverEnablesV2AndBypassesOldWindows(t *testing.T) {
	r, q := agentwork.New(), agentdecision.New()
	r.SetPlan(29, 7, "plan")
	r.StartAgent(29, 0)
	p := New(r, q)
	p.SetPurposeResolver(HTTPPurposeResolver{URL: "http://unused.invalid"})
	if p.interactionV2.Load() == nil {
		t.Fatal("production path did not enable replacement")
	}
	event := model.RoomEvent{ID: 1, TenantID: 14, RoomID: 29, UserID: "u", Nickname: "小王", EventType: "chat", Content: "哪里发货", OccurredAt: time.Now()}
	p.Handle(event, basepipeline.Signal{Content: event.Content, IsQuestion: true})
	if len(q.Snapshot(29).Queue) != 0 {
		t.Fatal("old runtime still handles same event")
	}
	tasks := p.interactionV2.Load().engine.Snapshot(29)
	if len(tasks) != 1 || tasks[0].Work.Text != event.Content {
		t.Fatal(tasks)
	}
	event.ID = 2
	event.EventType = "comment"
	event.Content = "发什么快递"
	p.Handle(event, basepipeline.Signal{Content: event.Content, IsQuestion: true, IsOrderHint: true})
	tasks = p.interactionV2.Load().engine.Snapshot(29)
	if len(tasks) != 2 || tasks[1].Work.Kind != "semantic" {
		t.Fatal("comment question dropped or misclassified as complaint", tasks)
	}
	raw, _ := json.Marshal(map[string]any{"decision_id": "unused", "status": "PLAYING", "at": time.Now()})
	if !p.interactionV2Receipt(messagecaptain.Message{Kind: "interaction.v2.playback", RoomID: 29, Payload: raw}) {
		t.Fatal("playback entry disconnected")
	}
}
