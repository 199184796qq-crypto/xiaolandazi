package decisionexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
)

func TestBehaviorThanksTenChoicesWithSafeSingleAndGroupAddresses(t *testing.T) {
	_, _, _, _, _, session, _ := v2Fixture(t, 61200)
	snapshot, ok := interactionplan.Frozen.ForSession(session.TenantID, session.RoomID, session.ID)
	if !ok {
		t.Fatal("missing frozen content")
	}
	for _, group := range []struct {
		template string
		choices  []string
	}{{"like_v1", likeThanksTemplates[:]}, {"follow_v1", followThanksTemplates[:]}} {
		t.Run(group.template, func(t *testing.T) {
			if len(group.choices) != 10 {
				t.Fatalf("want ten choices, got %d", len(group.choices))
			}
			unique := map[string]bool{}
			for _, format := range group.choices {
				if unique[format] || strings.Count(format, "%s") != 1 || strings.Count(format, "%") != 1 {
					t.Fatalf("duplicate or malformed addressing: %q", format)
				}
				unique[format] = true
				for _, address := range []string{"小王", "老乡们", "小王、小张、小李"} {
					text := fmt.Sprintf(format, address)
					if strings.Count(text, address) != 1 || strings.Contains(text, "%!") || utf8.RuneCountInString(text) > 40 {
						t.Fatalf("invalid/long spoken text: %q", text)
					}
					if err := interactionplan.Validate(snapshot, text); err != nil {
						t.Fatalf("existing P0 rejected %q: %v", text, err)
					}
				}
			}
		})
	}
}

func TestBehaviorThanksDiceCoversTenAndStaysStableForTheSameTask(t *testing.T) {
	session := model.LiveRuntimeSession{TenantID: 14, RoomID: 29, ID: 641, ExternalID: "fixed-show"}
	for _, template := range []string{"like_v1", "follow_v1"} {
		t.Run(template, func(t *testing.T) {
			counts := map[string]int{}
			for i := 0; i < 4096; i++ {
				decision := fmt.Sprintf("new-task-%d", i)
				text, err := behaviorThanksText(session, decision, template, "老乡们")
				if err != nil {
					t.Fatal(err)
				}
				counts[text]++
				// Reconstruct the session and call independently: no worker map,
				// global RNG sequence or elapsed time may alter this task's roll.
				copy := model.LiveRuntimeSession{TenantID: 14, RoomID: 29, ID: 641, ExternalID: "fixed-show", Status: "paused"}
				retried, err := behaviorThanksText(copy, decision, template, "老乡们")
				if err != nil || retried != text {
					t.Fatal("retry rerolled", decision, text, retried, err)
				}
			}
			if len(counts) != 10 {
				t.Fatal("some choices unreachable", counts)
			}
			t.Logf("4096 fixed new-task identities: %v", counts)
		})
	}
}

func TestBehaviorThanksRealWorkerDoesNotRerollOrRepurchaseWhileWaiting(t *testing.T) {
	for i, kind := range []string{"like", "follow"} {
		t.Run(kind, func(t *testing.T) {
			w, _, core, agent, tts, session, envelope := v2Fixture(t, int64(61210+i))
			now := time.Now()
			w.now = func() time.Time { return now }
			envelope.ExpiresAt = now.Add(30 * time.Second)
			envelope.Template = kind + "_v1"
			envelope.EventCount = 1
			envelope.Intent.Question = false
			envelope.Intent.Type = "闲聊"
			envelope.Body, _ = interactionplan.BehaviorBody(kind, envelope.Template, 1)
			mission := "reply_" + kind
			claim := func(id string) {
				raw, _ := json.Marshal(map[string]any{"claimed": true, "item": map[string]any{"id": id, "mission_kind": mission, "nicknames": []string{"小王", "###"}, "purpose_knowledge": envelope, "expires_at": envelope.ExpiresAt}})
				core.claimRaw = string(raw)
			}
			core.dispatchStatus = 409
			core.dispatchRaw = `{"error":"当前35秒内没有安全句末切点，继续排队"}`
			claim("dice-original")
			if err := w.processRoom(context.Background(), session); err != nil {
				t.Fatal(err)
			}
			first := tts.last.Text
			for attempt := 0; attempt < 3; attempt++ {
				now = now.Add(2 * time.Second)
				claim("dice-original")
				if err := w.processRoom(context.Background(), session); err != nil {
					t.Fatal(err)
				}
				if tts.calls != 1 || tts.last.Text != first || agent.calls != 0 {
					t.Fatal("waiting rerolled/rebought or used a model", tts.calls, tts.last.Text, agent.calls)
				}
			}
			if !strings.Contains(first, "小王") || strings.Contains(first, "###") {
				t.Fatal("nickname guard changed", first)
			}
			// A different task is allowed to roll the same choice. Find one
			// with a different roll, then verify the real worker follows it.
			next := ""
			for j := 0; j < 100; j++ {
				id := fmt.Sprintf("dice-new-%d", j)
				text, err := behaviorThanksText(session, id, envelope.Template, "小王")
				if err != nil {
					t.Fatal(err)
				}
				if text != first {
					next = id
					break
				}
			}
			if next == "" {
				t.Fatal("new task cannot select a different choice")
			}
			core.dispatchStatus, core.dispatchRaw = 200, `{"ok":true}`
			claim(next)
			if err := w.processRoom(context.Background(), session); err != nil {
				t.Fatal(err)
			}
			if tts.calls != 2 || tts.last.Text == first || agent.calls != 0 {
				t.Fatal("new task did not get its own dice/text", tts.calls, tts.last.Text, first, agent.calls)
			}
		})
	}
}

func TestBehaviorThanksRejectsMissingTaskIdentityAndUnknownTemplates(t *testing.T) {
	valid := model.LiveRuntimeSession{TenantID: 14, RoomID: 29, ID: 641}
	for _, v := range []struct {
		session               model.LiveRuntimeSession
		id, template, address string
	}{{valid, "", "like_v1", "老乡们"}, {valid, " \t", "follow_v1", "老乡们"}, {valid, "d", "like_v1", ""}, {valid, "d", "order_single_v1", "老乡们"}, {model.LiveRuntimeSession{}, "d", "like_v1", "老乡们"}} {
		if text, err := behaviorThanksText(v.session, v.id, v.template, v.address); err == nil || text != "" {
			t.Fatal("missing/unknown identity accepted", text, err)
		}
	}
}
