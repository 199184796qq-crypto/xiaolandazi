package paidpipeline

import (
	"context"
	"encoding/json"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/interactionruntime"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/model"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInteractionV2HTTPAdapterBindsOneTaskResult(t *testing.T) {
	s := interactionruntime.Scope{Tenant: 14, Room: 29, Plan: 7, Epoch: "boot"}
	w := interactionruntime.Work{Scope: s, ID: "server-owned", Kind: "semantic", Text: "哪里发货", Revision: 2, Expires: time.Now().Add(time.Minute), AllowClarify: true}
	server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Core-Token") != "secret" {
			t.Error("auth missing")
		}
		var in map[string]any
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			t.Error("bad payload")
		}
		for _, k := range []string{"user_id", "nickname", "fact_ids"} {
			if _, ok := in[k]; ok {
				t.Error("identity leaked to content", k)
			}
		}
		in["body"] = "按当前方案发货。"
		in["intent"] = map[string]any{"relevant": true, "question": true}
		json.NewEncoder(out).Encode(map[string]any{"knowledge": in})
	}))
	defer server.Close()
	c, err := (HTTPPurposeResolver{URL: server.URL, Token: "secret"}).prepareV2(context.Background(), w)
	if err != nil || c.Body != "按当前方案发货。" || !c.Intent.Question {
		t.Fatal(c, err)
	}
}
func TestInteractionV2HTTPAdapterRejectsMismatchedIdentity(t *testing.T) {
	for _, field := range []string{"task", "epoch", "revision", "tenant_id", "room_id", "plan_id", "expires_at"} {
		t.Run(field, func(t *testing.T) {
			w := interactionruntime.Work{Scope: interactionruntime.Scope{Tenant: 14, Room: 29, Plan: 7, Epoch: "boot"}, ID: "task", Revision: 1, Expires: time.Now().Add(time.Minute)}
			server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
				var in map[string]any
				json.NewDecoder(r.Body).Decode(&in)
				in[field] = "changed"
				json.NewEncoder(out).Encode(map[string]any{"knowledge": in})
			}))
			defer server.Close()
			if _, err := (HTTPPurposeResolver{URL: server.URL}).prepareV2(context.Background(), w); err == nil {
				t.Fatal("accepted unrelated model/result", field)
			}
		})
	}
}
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
	if len(q.Snapshot(29).Queue) != 0 || len(p.interactionWindows[29]) != 0 {
		t.Fatal("old runtime still handles same event")
	}
	tasks := p.interactionV2.Load().engine.Snapshot(29)
	if len(tasks) != 1 || tasks[0].Work.Text != event.Content {
		t.Fatal(tasks)
	}
	raw, _ := json.Marshal(map[string]any{"decision_id": "unused", "status": "PLAYING", "at": time.Now()})
	if !p.interactionV2Receipt(messagecaptain.Message{Kind: "interaction.v2.playback", RoomID: 29, Payload: raw}) {
		t.Fatal("playback entry disconnected")
	}
}
