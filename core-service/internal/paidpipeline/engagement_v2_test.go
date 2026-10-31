package paidpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/interactionruntime"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/roomaudio"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestActualPCMToParticipationRoutingEndToEnd(t *testing.T) {
	r := agentwork.New()
	r.SetPlan(29, 4, "plan")
	run, _ := r.StartAgent(29, 0)
	now := run.WorkingSince.Add(time.Second)
	p := New(r, agentdecision.New())
	p.now = func() time.Time { return now }
	p.SetPurposeResolver(HTTPPurposeResolver{URL: "http://unused.invalid"})
	p.Handle(model.RoomEvent{ID: 1, TenantID: 14, RoomID: 29, EventType: "chat", UserID: "buyer", Content: "哪里发货", OccurredAt: now}, basepipeline.Signal{})
	audio := roomaudio.New()
	audio.SetSpokenObserver(p.ObserveSpoken)
	audio.SetMainlineTimeline(29, []roomaudio.SpeechSegment{{SegmentID: "live", Text: "想要就扣1。", StartMS: 0, EndMS: 1000}})
	audio.StartMainline(29, "live")
	// Preparing text is not enough. It must enter actual server PCM output.
	audio.PublishPCMAt(29, roomaudio.SourceMainline, make([]byte, roomaudio.PCMBytesPerFrame), "live", 0)
	for i := 0; i < 3; i++ {
		p.Handle(model.RoomEvent{ID: int64(10 + i), TenantID: 14, RoomID: 29, UserID: fmt.Sprint(i), EventType: "chat", Content: "1", OccurredAt: now}, basepipeline.Signal{})
	}
	bs := p.interactionV2.Load().engine.Batches(now.Add(12*time.Second), 4)
	semantic, aggregate := 0, 0
	for _, b := range bs {
		for _, w := range b.Works {
			switch w.Kind {
			case "semantic":
				semantic++
				if w.Text != "哪里发货" {
					t.Fatal(w)
				}
			case "engagement":
				aggregate++
				if w.Count != 3 {
					t.Fatal(w)
				}
			}
		}
	}
	if semantic != 1 || aggregate != 1 {
		t.Fatal(semantic, aggregate, bs)
	}
	// An old segment after restarting the agent cannot create a new command.
	r.StopAgent(29, agentwork.StopReasonManual)
	next, _ := r.StartAgent(29, 0)
	p.ObserveSpoken(roomaudio.SpokenSegment{RoomID: 29, Sequence: 88, Text: "扣2。", At: next.WorkingSince.Add(-time.Second)})
	if p.runtime.Get(29).RunGeneration != next.RunGeneration {
		t.Fatal("observer changed runtime")
	}
}

func TestPreparedBatchCarriesActiveTokenAndPreservesParticipationResult(t *testing.T) {
	expires := time.Now().Add(time.Minute)
	w := interactionruntime.Work{Scope: interactionruntime.Scope{Tenant: 14, Room: 29, Plan: 4, Epoch: "epoch"}, ID: "task", Revision: 1, Kind: "semantic", Text: "收到", EngagementToken: "收到", Expires: expires}
	srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		var in struct {
			BatchID string           `json:"batch_id"`
			Items   []prepareInputV2 `json:"items"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.Items) != 1 || in.Items[0].EngagementToken != "收到" {
			t.Error("token context missing")
		}
		json.NewEncoder(out).Encode(map[string]any{"protocol": interactionruntime.Protocol, "batch_id": in.BatchID, "results": []any{map[string]any{"task": w.ID, "revision": 1, "knowledge": map[string]any{"protocol": interactionruntime.Protocol, "task": w.ID, "epoch": w.Scope.Epoch, "revision": 1, "tenant_id": 14, "room_id": 29, "plan_id": 4, "expires_at": expires, "body": "", "intent": map[string]any{"type": "主动互动", "relevant": true, "question": false}}}}})
	}))
	defer srv.Close()
	got, err := (HTTPPurposeResolver{URL: srv.URL}).prepareBatchV2(context.Background(), interactionruntime.Batch{ID: "batch", Works: []interactionruntime.Work{w}})
	if err != nil || len(got) != 1 || got[0].Err != nil || got[0].Content.Intent.Type != "主动互动" || got[0].Content.Body != "" {
		t.Fatal(got, err)
	}
}
