package paidpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/interactionruntime"
	"livecompanion/core/internal/model"
)

func TestBehaviorProcessorUsesTemplateLaneEvenWhenAllModelSlotsAreBusy(t *testing.T) {
	start := time.Now()
	var tick atomic.Int64
	tick.Store(start.UnixNano())
	now := func() time.Time { return time.Unix(0, tick.Load()) }
	runtime := agentwork.New()
	runtime.SetPlan(29, 7, "plan")
	runtime.StartAgent(29, 0)
	queue := agentdecision.NewWithClock(now, time.Minute, time.Minute, 24)
	p := New(runtime, queue)
	p.now = now
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		var in struct {
			ID    string           `json:"batch_id"`
			Items []map[string]any `json:"items"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		if len(in.Items) != 1 || in.Items[0]["template"] == nil {
			t.Error("not a program template", in)
			out.WriteHeader(500)
			return
		}
		item := in.Items[0]
		if item["user_id"] != nil || item["nickname"] != nil {
			t.Error("unnecessary identity sent to preparation endpoint")
		}
		item["body"] = "谢谢支持。"
		item["intent"] = map[string]any{"relevant": true, "type": "闲聊"}
		requests.Add(1)
		json.NewEncoder(out).Encode(map[string]any{"protocol": interactionruntime.Protocol, "batch_id": in.ID, "results": []any{map[string]any{"task": item["task"], "revision": item["revision"], "knowledge": item}}})
	}))
	defer srv.Close()
	p.SetPurposeResolver(HTTPPurposeResolver{URL: srv.URL})
	v := p.interactionV2.Load()
	for i := 0; i < cap(v.slots); i++ {
		v.slots <- struct{}{}
	}
	for i := 0; i < 4; i++ {
		p.Handle(model.RoomEvent{ID: int64(i + 1), TenantID: 14, RoomID: 29, UserID: fmt.Sprint(i), Nickname: "小王", EventType: "chat", Content: "我拍了不知道多久到", OccurredAt: start}, basepipeline.Signal{})
	}
	p.Handle(model.RoomEvent{ID: 20, TenantID: 14, RoomID: 29, UserID: "l", Nickname: "小张", EventType: "like", OccurredAt: start}, basepipeline.Signal{})
	p.Handle(model.RoomEvent{ID: 21, TenantID: 14, RoomID: 29, UserID: "f", Nickname: "小李", EventType: "follow", OccurredAt: start}, basepipeline.Signal{})
	p.tickInteractionV2(context.Background(), now())
	until := time.Now().Add(3 * time.Second)
	for {
		ready := 0
		for _, task := range v.engine.Snapshot(29) {
			if task.State == "body_ready" {
				ready++
			}
		}
		if ready == 3 {
			break
		}
		if time.Now().After(until) {
			t.Fatal(v.engine.Snapshot(29), requests.Load())
		}
		time.Sleep(time.Millisecond)
	}
	if requests.Load() != 3 || len(v.slots) != 4 {
		t.Fatal("used model slots", requests.Load(), len(v.slots))
	}
	tick.Store(start.Add(time.Second).UnixNano())
	p.tickInteractionV2(context.Background(), now())
	q := queue.Snapshot(29).Queue
	if len(q) != 1 || q[0].MissionKind != "conversion_signal" || q[0].MissionEventCount != 4 {
		t.Fatal(q)
	}
	// The old event's 30s lease must be honored even if the queue/worker is busy.
	tick.Store(start.Add(31 * time.Second).UnixNano())
	p.tickInteractionV2(context.Background(), now())
	if len(queue.Snapshot(29).Queue) != 0 {
		t.Fatal("expired behavior remained queued")
	}
}
