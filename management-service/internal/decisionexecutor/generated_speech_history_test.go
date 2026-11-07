package decisionexecutor

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGeneratedSpeechHistoryKeepsTaskKindSeparateFromModelIntent(t *testing.T) {
	for _, kind := range []string{"welcome_named", "welcome_batch", "warmup", "reply_like", "reply_follow", "conversion_signal", "time_report", "reply_question"} {
		t.Run(kind, func(t *testing.T) {
			original := `{"protocol":"interaction_v2","task":"task-1","body":"原始正文","intent":{"meaning":"原始意图","type":"闲聊"},"mission_kind":"untrusted","event_kind":"untrusted"}`
			item := decisionItem{MissionKind: kind, PurposeKnowledge: json.RawMessage(original)}
			item.InteractionDecision.PrimaryEvent = "welcome"
			var result map[string]json.RawMessage
			if err := json.Unmarshal([]byte(generatedSpeechHistoryIntentJSON(&item)), &result); err != nil {
				t.Fatal(err)
			}
			if string(result["mission_kind"]) != `"`+kind+`"` || string(result["event_kind"]) != `"welcome"` {
				t.Fatal("history did not retain the program's task identity", result)
			}
			var before map[string]json.RawMessage
			_ = json.Unmarshal([]byte(original), &before)
			for _, key := range []string{"protocol", "task", "body", "intent"} {
				if string(result[key]) != string(before[key]) {
					t.Fatalf("history changed original field %s", key)
				}
			}
			if string(item.PurposeKnowledge) != original {
				t.Fatal("history metadata mutated prepared playback/model input")
			}
		})
	}
}

func TestGeneratedSpeechHistoryPreservesMalformedLegacyPayload(t *testing.T) {
	for _, raw := range []string{"", "null", "[]", "旧版原始意图", "{损坏JSON"} {
		item := decisionItem{MissionKind: "warmup", PurposeKnowledge: json.RawMessage(raw)}
		var result struct{ MissionKind, LegacyIntent string }
		var values map[string]string
		if err := json.Unmarshal([]byte(generatedSpeechHistoryIntentJSON(&item)), &values); err != nil {
			t.Fatal(err)
		}
		result.MissionKind, result.LegacyIntent = values["mission_kind"], values["legacy_intent"]
		if result.MissionKind != "warmup" || result.LegacyIntent != raw {
			t.Fatalf("lost legacy metadata: %#v", result)
		}
	}
	item := decisionItem{PurposeKnowledge: json.RawMessage(`{"mission_kind":"welcome_named","event_kind":"welcome"}`)}
	var result map[string]any
	_ = json.Unmarshal([]byte(generatedSpeechHistoryIntentJSON(&item)), &result)
	if _, exists := result["mission_kind"]; exists {
		t.Fatal("missing program identity must not trust a payload's task label")
	}
}

func TestDeliveredSpeechHistoryRecordsActualTaskKindWithoutExtraGeneration(t *testing.T) {
	worker, store, core, agent, tts, session := preparedTestWorker()
	core.dispatchStatus, core.dispatchRaw = 0, ""
	core.claimRaw = `{"claimed":true,"item":{"id":"d-1","topic":"FAMILY:发货物流","sample_questions":["什么时候发货"],"mission_kind":"reply_question","manual_action":"answer"}}`
	if err := worker.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if len(store.history) != 1 || agent.calls != 0 || tts.calls != 1 {
		t.Fatalf("unexpected delivery/generation: history=%d model=%d tts=%d", len(store.history), agent.calls, tts.calls)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(store.history[0].IntentJSON), &record); err != nil || record["mission_kind"] != "reply_question" {
		t.Fatal("delivered history lost actual task kind", record, err)
	}
}
