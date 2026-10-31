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

func wireBatch(n int) interactionruntime.Batch {
	b := interactionruntime.Batch{ID: "opaque-batch"}
	for i := 0; i < n; i++ {
		w := interactionruntime.Work{Scope: interactionruntime.Scope{Tenant: 14, Room: 29, Plan: 7, Epoch: "boot"}, ID: fmt.Sprintf("task-%d", i), Revision: 1, Kind: "semantic", Text: "发什么快递", Expires: time.Now().Add(time.Minute), People: []interactionruntime.Person{{UID: "private-uid", Name: "私有昵称"}}}
		for j := 0; j < 5; j++ {
			w.Context = append(w.Context, interactionruntime.ContextMessage{Role: "user", Text: fmt.Sprintf("本人%c上下文%d", 'A'+i, j)})
		}
		b.Works = append(b.Works, w)
	}
	return b
}

func TestBatchHTTPAdapterOneThroughTenSingleRequestAndReorderedResults(t *testing.T) {
	for n := 1; n <= 10; n++ {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			batch := wireBatch(n)
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("X-Core-Token") != "secret" {
					t.Error("missing auth")
				}
				var in struct {
					Batch string           `json:"batch_id"`
					Items []map[string]any `json:"items"`
				}
				if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.Items) != n {
					t.Error("bad batch")
				}
				results := []any{}
				for i := n - 1; i >= 0; i-- {
					item := in.Items[i]
					for _, key := range []string{"user_id", "nickname", "People", "UID"} {
						if _, ok := item[key]; ok {
							t.Error("identity leak", key)
						}
					}
					item["body"] = fmt.Sprintf("问题%c的答复。", 'A'+i)
					item["intent"] = map[string]any{"type": "投诉", "relevant": true, "question": true, "priority": "较高", "priority_reason": "售后处理"}
					results = append(results, map[string]any{"task": item["task"], "revision": item["revision"], "knowledge": item})
				}
				json.NewEncoder(out).Encode(map[string]any{"protocol": interactionruntime.Protocol, "batch_id": in.Batch, "results": results})
			}))
			defer srv.Close()
			got, err := (HTTPPurposeResolver{URL: srv.URL, Token: "secret"}).prepareBatchV2(context.Background(), batch)
			if err != nil || len(got) != n || calls.Load() != 1 {
				t.Fatal(got, err, calls.Load())
			}
			for i, item := range got {
				if item.Err != nil || item.Content.Body != fmt.Sprintf("问题%c的答复。", 'A'+i) || item.Content.Intent.Type != "投诉" || item.Content.Intent.Priority != "较高" {
					t.Fatal("mapping/class lost", i, item)
				}
			}
		})
	}
}

func TestBatchHTTPAdapterRejectsPerItemTamperingAndMissingResults(t *testing.T) {
	for _, field := range []string{"task", "epoch", "revision", "tenant_id", "room_id", "plan_id", "expires_at", "missing", "duplicate"} {
		t.Run(field, func(t *testing.T) {
			b := wireBatch(2)
			srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
				var in struct {
					Batch string           `json:"batch_id"`
					Items []map[string]any `json:"items"`
				}
				json.NewDecoder(r.Body).Decode(&in)
				results := []any{}
				for i, item := range in.Items {
					if i == 0 && field == "missing" {
						continue
					}
					task, revision := item["task"], item["revision"]
					item["body"] = "答复。"
					item["intent"] = map[string]any{"relevant": true, "question": true}
					if i == 0 && field != "duplicate" {
						item[field] = "changed"
					}
					entry := map[string]any{"task": task, "revision": revision, "knowledge": item}
					results = append(results, entry)
					if i == 0 && field == "duplicate" {
						results = append(results, entry)
					}
				}
				json.NewEncoder(out).Encode(map[string]any{"protocol": interactionruntime.Protocol, "batch_id": in.Batch, "results": results})
			}))
			defer srv.Close()
			got, err := (HTTPPurposeResolver{URL: srv.URL}).prepareBatchV2(context.Background(), b)
			if err != nil || got[0].Err == nil || got[1].Err != nil {
				t.Fatal(field, got, err)
			}
		})
	}
}

func TestBatchHTTPAdapterRejectsCrossRoomBeforeSending(t *testing.T) {
	b := wireBatch(2)
	b.Works[1].Scope.Room = 30
	if _, err := (HTTPPurposeResolver{URL: "http://unused.invalid"}).prepareBatchV2(context.Background(), b); err == nil {
		t.Fatal("cross-room batch sent")
	}
}

func TestProductionTickBatchesThenSelectsOneClassifiedPerson(t *testing.T) {
	for _, n := range []int{1, 10} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			start := time.Now()
			var tick atomic.Int64
			tick.Store(start.UnixNano())
			now := func() time.Time { return time.Unix(0, tick.Load()) }
			runtime, queue := agentwork.New(), agentdecision.NewWithClock(now, agentdecision.DefaultTTL, agentdecision.DefaultCooldown, agentdecision.DefaultCapacity)
			runtime.SetPlan(29, 7, "plan")
			runtime.StartAgent(29, 0)
			p := New(runtime, queue)
			p.now = now
			var calls atomic.Int32
			var expectedTask atomic.Value
			srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var input struct {
					Batch string           `json:"batch_id"`
					Items []map[string]any `json:"items"`
				}
				json.NewDecoder(r.Body).Decode(&input)
				if len(input.Items) != n {
					t.Errorf("got %d items want %d", len(input.Items), n)
				}
				entries := []any{}
				for i, item := range input.Items {
					typeName := "提问"
					if i == 0 {
						typeName = "投诉"
						expectedTask.Store(item["task"].(string))
					}
					item["body"] = "可以联系客服核实处理。"
					item["intent"] = map[string]any{"type": typeName, "relevant": true, "question": true}
					entries = append(entries, map[string]any{"task": item["task"], "revision": item["revision"], "knowledge": item})
				}
				json.NewEncoder(out).Encode(map[string]any{"protocol": interactionruntime.Protocol, "batch_id": input.Batch, "results": entries})
			}))
			defer srv.Close()
			p.SetPurposeResolver(HTTPPurposeResolver{URL: srv.URL})
			for i := 0; i < n; i++ {
				p.Handle(model.RoomEvent{ID: int64(i + 1), TenantID: 14, RoomID: 29, UserID: fmt.Sprintf("uid%d", i), Nickname: fmt.Sprintf("昵称%d", i), EventType: "chat", Content: "咨询当前问题", OccurredAt: start}, basepipeline.Signal{Content: "咨询当前问题", IsQuestion: true})
			}
			tick.Store(start.Add(2 * time.Second).UnixNano())
			p.tickInteractionV2(context.Background(), p.clock())
			until := time.Now().Add(4 * time.Second)
			for {
				ready := 0
				for _, task := range p.interactionV2.Load().engine.Snapshot(29) {
					if task.State == "body_ready" {
						ready++
					}
				}
				if ready == n {
					break
				}
				if time.Now().After(until) {
					t.Fatal("batch not ready", p.interactionV2.Load().engine.Snapshot(29))
				}
				time.Sleep(time.Millisecond)
			}
			if len(queue.Snapshot(29).Queue) != 0 {
				t.Fatal("all generated replies were enqueued before selection")
			}
			tick.Store(start.Add(3 * time.Second).UnixNano())
			p.tickInteractionV2(context.Background(), p.clock())
			q := queue.Snapshot(29).Queue
			if len(q) != 1 || calls.Load() != 1 || q[0].MissionKind != "complaint" {
				t.Fatal("batch/priority not connected", q, calls.Load())
			}
			var envelope struct {
				Task   string                    `json:"task"`
				Intent interactionruntime.Intent `json:"intent"`
			}
			json.Unmarshal(q[0].PurposeKnowledge, &envelope)
			if envelope.Task != expectedTask.Load().(string) || envelope.Intent.Type != "投诉" || !q[0].ExpiresAt.Equal(start.Add(53*time.Second)) {
				t.Fatal("wrong selected result/deadline", q[0], envelope)
			}
		})
	}
}

func TestTextHintsStillUseJointModelClassification(t *testing.T) {
	runtime := agentwork.New()
	runtime.SetPlan(29, 7, "plan")
	runtime.StartAgent(29, 0)
	p := New(runtime, agentdecision.New())
	p.SetPurposeResolver(HTTPPurposeResolver{URL: "http://unused.invalid"})
	for i, signal := range []basepipeline.Signal{{IsOrderHint: true}, {IsNegative: true}} {
		p.Handle(model.RoomEvent{ID: int64(i + 1), TenantID: 14, RoomID: 29, UserID: fmt.Sprint(i), EventType: "chat", Content: "先由模型判断此人的意图", OccurredAt: time.Now()}, signal)
	}
	for _, task := range p.interactionV2.Load().engine.Snapshot(29) {
		if task.Work.Kind != "semantic" {
			t.Fatal("coarse event hint bypassed batch classification", task)
		}
	}
}
