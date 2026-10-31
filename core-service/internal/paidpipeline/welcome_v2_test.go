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
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/model"
)

func TestWelcomeRealProcessorOneBodyForOneOrTenPeopleAndPreparedTTL(t *testing.T) {
	for _, n := range []int{1, 10, 16} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			start := time.Now()
			var tick atomic.Int64
			tick.Store(start.UnixNano())
			now := func() time.Time { return time.Unix(0, tick.Load()) }
			runtime := agentwork.New()
			runtime.SetPlan(29, 7, "plan")
			runtime.StartAgent(29, 0)
			q := agentdecision.NewWithClock(now, time.Minute, time.Minute, 24)
			p := New(runtime, q)
			p.now = now
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var input struct {
					Batch string           `json:"batch_id"`
					Items []map[string]any `json:"items"`
				}
				json.NewDecoder(r.Body).Decode(&input)
				if len(input.Items) != 1 || input.Items[0]["event_count"] != float64(min(n, 10)) {
					t.Error("welcome split into personal model requests", input.Items)
				}
				item := input.Items[0]
				if item["kind"] != "welcome" {
					t.Error(item)
				}
				item["body"] = "欢迎新来的朋友，一起看看。"
				item["intent"] = map[string]any{"relevant": true}
				json.NewEncoder(out).Encode(map[string]any{"protocol": interactionruntime.Protocol, "batch_id": input.Batch, "results": []any{map[string]any{"task": item["task"], "revision": item["revision"], "knowledge": item}}})
			}))
			defer srv.Close()
			p.SetPurposeResolver(HTTPPurposeResolver{URL: srv.URL})
			for i := 0; i < n; i++ {
				p.Handle(model.RoomEvent{ID: int64(i + 1), TenantID: 14, RoomID: 29, UserID: fmt.Sprint(i), Nickname: fmt.Sprintf("新人%d", i), EventType: "member", OccurredAt: start}, basepipeline.Signal{})
			}
			p.tickInteractionV2(context.Background(), now())
			until := time.Now().Add(3 * time.Second)
			for {
				a := p.interactionV2.Load().engine.Snapshot(29)
				if len(a) == 1 && a[0].State == "body_ready" {
					break
				}
				if time.Now().After(until) {
					t.Fatal(a)
				}
				time.Sleep(time.Millisecond)
			}
			tick.Store(start.Add(time.Second).UnixNano())
			p.tickInteractionV2(context.Background(), now())
			queued := q.Snapshot(29).Queue
			if len(queued) != 1 || calls.Load() != 1 || len(queued[0].Nicknames) != min(n, 10) || queued[0].MissionEventCount != min(n, 10) {
				t.Fatal("incorrect welcome aggregation", queued, calls.Load())
			}
			id := queued[0].ID
			claimed, ok := q.ClaimNext(29)
			if !ok || claimed.ID != id {
				t.Fatal("claim")
			}
			tick.Store(start.Add(10 * time.Second).UnixNano())
			raw, _ := json.Marshal(map[string]string{"decision_id": id})
			msg := messagecaptain.Message{Kind: "interaction.v2.welcome-ready", RoomID: 29, Payload: raw}
			if err := p.handleRuntimeMessage(context.Background(), msg); err != nil {
				t.Fatal(err)
			}
			prepared, _ := q.Get(29, id)
			want := now().Add(30 * time.Second)
			if !prepared.ExpiresAt.Equal(want) || !prepared.AudioPrepared {
				t.Fatal("TTL started before complete audio", prepared)
			}
			tick.Store(start.Add(20 * time.Second).UnixNano())
			if err := p.handleRuntimeMessage(context.Background(), msg); err != nil {
				t.Fatal(err)
			}
			again, _ := q.Get(29, id)
			if !again.ExpiresAt.Equal(want) {
				t.Fatal("renewed on retry")
			}
			tick.Store(want.UnixNano())
			p.tickInteractionV2(context.Background(), now())
			if _, ok := q.Get(29, id); ok {
				t.Fatal("expired welcome remained executable")
			}
		})
	}
}
