package paidpipeline

import (
	"context"
	"encoding/json"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/interactionruntime"
	"livecompanion/core/internal/messagecaptain"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestProductionWarmupWaitsForEveryActualPlaybackEnd(t *testing.T) {
	start := time.Now()
	var clock atomic.Int64
	clock.Store(start.UnixNano())
	now := func() time.Time { return time.Unix(0, clock.Load()) }
	r := agentwork.New()
	r.SetPlan(29, 7, "plan")
	r.SetMode(29, agentwork.ModeAnchor)
	r.StartAgent(29, 0)
	q := agentdecision.NewWithClock(now, time.Minute, time.Minute, 24)
	p := New(r, q)
	p.now = now
	p.SetTenantResolver(func(int64) (int64, bool) { return 14, true })
	p.SetAudioStartedReader(func(int64) time.Time { return start })
	p.SetWarmupEligibility(func(int64) bool { return true })
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		var in struct {
			Batch string           `json:"batch_id"`
			Items []map[string]any `json:"items"`
		}
		json.NewDecoder(req.Body).Decode(&in)
		if len(in.Items) != 1 || in.Items[0]["kind"] != "guide" {
			t.Error("production guide did not use batch path", in)
			w.WriteHeader(400)
			return
		}
		item := in.Items[0]
		item["body"] = "继续看看咱们的商品。"
		item["intent"] = map[string]any{"relevant": true}
		json.NewEncoder(w).Encode(map[string]any{"protocol": interactionruntime.Protocol, "batch_id": in.Batch, "results": []any{map[string]any{"task": item["task"], "revision": item["revision"], "knowledge": item}}})
	}))
	defer srv.Close()
	p.SetPurposeResolver(HTTPPurposeResolver{URL: srv.URL})
	tick := func(seconds int) {
		clock.Store(start.Add(time.Duration(seconds) * time.Second).UnixNano())
		p.tickInteractionV2(context.Background(), now())
	}
	tick(179)
	if calls.Load() != 0 || len(p.interactionV2.Load().engine.Snapshot(29)) != 0 {
		t.Fatal("warmup before 180 seconds")
	}
	tick(180)
	tick(181)
	until := time.Now().Add(3 * time.Second)
	for {
		tasks := p.interactionV2.Load().engine.Snapshot(29)
		if len(tasks) == 1 && tasks[0].State == "body_ready" {
			break
		}
		if time.Now().After(until) {
			t.Fatal(tasks)
		}
		time.Sleep(time.Millisecond)
	}
	tick(182)
	if len(q.Snapshot(29).Queue) != 1 || calls.Load() != 1 {
		t.Fatal("guide not dispatched through current path")
	}
	manual := q.Enqueue(29, agentdecision.Candidate{Source: agentdecision.SourceManual, Topic: "manual", Question: "人工插播", Priority: 200, Scene: "product_question", TTLSeconds: 60}).Item
	if manual == nil {
		t.Fatal("manual enqueue")
	}
	claimed, ok := q.ClaimNext(29)
	if !ok || claimed.ID != manual.ID {
		t.Fatal(claimed)
	}
	q.BindPlayback(29, manual.ID, "manual-audio")
	receipt := func(status, epoch string, seconds int) {
		clock.Store(start.Add(time.Duration(seconds) * time.Second).UnixNano())
		raw, _ := json.Marshal(map[string]any{"decision_id": manual.ID, "status": status, "at": now()})
		p.handleRuntimeMessage(context.Background(), messagecaptain.Message{Kind: "interaction.v2.playback", RoomID: 29, RunID: epoch, TaskID: "manual-audio", Payload: raw})
	}
	receipt("PLAYING", "obsolete-run", 183)
	if len(p.interactionV2.Load().engine.Cancelled(29)) != 0 {
		t.Fatal("stale receipt changed current scheduler")
	}
	receipt("PLAYING", p.cutEpoch(29), 184)
	for _, task := range p.interactionV2.Load().engine.Snapshot(29) {
		if task.Work.Kind == "guide" && task.State != "cancelled" {
			t.Fatal("manual playback did not cancel queued guide", task)
		}
	}
	receipt("COMPLETED", p.cutEpoch(29), 190)
	q.Complete(29, manual.ID)
	tick(369)
	if calls.Load() != 1 {
		t.Fatal("guide before 180 seconds from manual end", calls.Load())
	}
	tick(370)
	tick(371)
	until = time.Now().Add(3 * time.Second)
	for calls.Load() != 2 {
		if time.Now().After(until) {
			t.Fatal("guide not eligible after true 180-second gap", p.interactionV2.Load().engine.Snapshot(29))
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMissingResolverCannotActivateRemovedHeuristicPath(t *testing.T) {
	r := agentwork.New()
	r.SetPlan(29, 7, "plan")
	r.StartAgent(29, 0)
	q := agentdecision.New()
	p := New(r, q)
	if p.interactionV2.Load() == nil {
		t.Fatal("missing current runtime")
	}
	if len(q.Snapshot(29).Queue) != 0 {
		t.Fatal("unexpected fallback queue")
	}
}
