package paidpipeline

import (
	"context"
	"encoding/json"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/interactionruntime"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPendingReportDoesNotOwnV2AdmissionLane(t *testing.T) {
	now := time.Now()
	p, q, _ := reportProcessor(t, &now, 24)
	s := p.v2Scope(14, 29)
	e := p.interactionV2.Load().engine
	report := q.Enqueue(29, agentdecision.Candidate{Topic: "report", MissionKind: "time_report", Question: "报时", Priority: 100, TTLSeconds: 600}).Item
	e.SetPolicy(s, p.v2Policy(s), now)
	e.Add(s, interactionruntime.Event{ID: "q", UID: "viewer", Kind: "chat", Text: "哪里发货", At: now}, now)
	now = now.Add(8 * time.Second)
	jobs := e.Batches(now, 1)
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	w := jobs[0].Works[0]
	raw, _ := json.Marshal(map[string]any{"protocol": interactionruntime.Protocol, "task": w.ID, "epoch": s.Epoch, "tenant_id": 14, "room_id": 29, "plan_id": s.Plan, "expires_at": w.Expires, "body": "按商品页。"})
	if !e.Accept(w, interactionruntime.Content{Body: "按商品页。", Intent: interactionruntime.Intent{Relevant: true, Question: true}, Knowledge: raw}, nil, now) {
		t.Fatal("accept")
	}
	p.tickInteractionV2(context.Background(), now)
	view := q.Snapshot(29)
	if len(view.Queue) != 2 {
		t.Fatal("unclaimed report blocked question", view)
	}
	q.YieldPreparation(29, report.ID, "no_pcm_cut")
	head := q.ClaimableHead(29)
	if head == nil || head.ID == report.ID {
		t.Fatal("report still blocks claim", head)
	}
}
func TestPhysicalGateBlocksActualResolverRequestsAndStillExpiresTasks(t *testing.T) {
	now := time.Now()
	p, _, _ := reportProcessor(t, &now, 24)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer srv.Close()
	p.SetPurposeResolver(HTTPPurposeResolver{URL: srv.URL})
	p.SetPreparationReader(func(context.Context, audioout.InteractionPreflightRequest) audioout.InteractionPreflightResult {
		return audioout.InteractionPreflightResult{Reason: "no_pcm_cut"}
	})
	s := p.v2Scope(14, 29)
	e := p.interactionV2.Load().engine
	e.SetPolicy(s, p.v2Policy(s), now)
	e.Add(s, interactionruntime.Event{ID: "q", UID: "viewer", Kind: "chat", Text: "哪里发货", At: now}, now)
	base := now
	for _, sec := range []int{1, 8, 20, 70} {
		now = base.Add(time.Duration(sec) * time.Second)
		p.tickInteractionV2(context.Background(), now)
	}
	if calls.Load() != 0 {
		t.Fatal("model resolver called without cut", calls.Load())
	}
	for _, task := range e.Snapshot(29) {
		if task.State != "expired" && task.State != "cancelled" {
			t.Fatal("blocked task did not expire", task)
		}
	}
}
