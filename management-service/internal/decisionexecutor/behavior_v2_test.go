package decisionexecutor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/interactionplan"
)

func TestBehaviorWorkerWholeTTSWithNoIntentBodyNameOrJoinModel(t *testing.T) {
	for i, x := range []struct {
		kind, id string
		count    int
		names    []string
	}{{"like", "like_v1", 1, []string{"小王"}}, {"follow", "follow_v1", 3, []string{"小张", "小王", "###"}}, {"order", "order_single_v1", 1, []string{"小王"}}, {"order", "order_count_v1", 4, []string{"旧名字不能播"}}, {"like", "like_v1", 1, []string{"超级爱吃苹果的人"}}} {
		t.Run(x.id, func(t *testing.T) {
			w, _, core, agent, tts, session, e := v2Fixture(t, int64(61000+i))
			e.Template = x.id
			e.EventCount = x.count
			e.Intent.Question = false
			e.Intent.Type = "闲聊"
			e.Body, _ = interactionplan.BehaviorBody(x.kind, x.id, x.count)
			mission := map[string]string{"like": "reply_like", "follow": "reply_follow", "order": "conversion_signal"}[x.kind]
			raw, _ := json.Marshal(map[string]any{"claimed": true, "item": map[string]any{"id": "template-task", "mission_kind": mission, "nicknames": x.names, "purpose_knowledge": e, "expires_at": e.ExpiresAt}})
			core.claimRaw = string(raw)
			if err := w.processRoom(context.Background(), session); err != nil {
				t.Fatal(err)
			}
			if agent.calls != 0 || tts.calls != 1 || core.dispatches != 1 {
				t.Fatal("model/tts count", agent.calls, tts.calls, core.dispatches)
			}
			if x.id == "order_count_v1" && (strings.Contains(tts.last.Text, "旧名字") || !strings.Contains(tts.last.Text, "4位")) {
				t.Fatal(tts.last.Text)
			}
		})
	}
}

func TestBehaviorWorkerRejectsChangedBodyAndReusesAudioUntilFixedExpiry(t *testing.T) {
	w, _, core, agent, tts, session, e := v2Fixture(t, 61110)
	now := time.Now()
	w.now = func() time.Time { return now }
	e.ExpiresAt = now.Add(30 * time.Second)
	e.Template = "order_count_v1"
	e.EventCount = 4
	e.Body, _ = interactionplan.BehaviorBody("order", e.Template, 4)
	setClaim := func() {
		raw, _ := json.Marshal(map[string]any{"claimed": true, "item": map[string]any{"id": "behavior-retry", "mission_kind": "conversion_signal", "nicknames": []string{"旧名字"}, "purpose_knowledge": e, "expires_at": e.ExpiresAt}})
		core.claimRaw = string(raw)
	}
	setClaim()
	core.dispatchStatus = 409
	core.dispatchRaw = `{"error":"当前35秒内没有安全句末切点，继续排队"}`
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)
	setClaim()
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if tts.calls != 1 || agent.calls != 0 {
		t.Fatal("repeated synthesis/model", tts.calls, agent.calls)
	}
	now = e.ExpiresAt
	setClaim()
	if err := w.processRoom(context.Background(), session); err == nil {
		t.Fatal("renewed template TTL")
	}
	if tts.calls != 1 {
		t.Fatal("expired audio synthesized again")
	}
	w2, _, c2, a2, t2, s2, e2 := v2Fixture(t, 61111)
	e2.Template = "like_v1"
	e2.EventCount = 1
	e2.Body = "随便改写的内容。"
	raw, _ := json.Marshal(map[string]any{"claimed": true, "item": map[string]any{"id": "forged-template", "mission_kind": "reply_like", "purpose_knowledge": e2, "expires_at": e2.ExpiresAt}})
	c2.claimRaw = string(raw)
	if err := w2.processRoom(context.Background(), s2); err == nil || a2.calls != 0 || t2.calls != 0 || c2.dispatches != 0 {
		t.Fatal("forged template passed", err, a2.calls, t2.calls, c2.dispatches)
	}
}
