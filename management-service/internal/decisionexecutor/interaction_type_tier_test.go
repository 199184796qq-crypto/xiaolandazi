package decisionexecutor

import (
	"encoding/json"
	"testing"

	"livecompanion/management/internal/interactionplan"
)

func TestClassifyInteractionUsesParsedType(t *testing.T) {
	cases := map[string]interactionTier{
		"提问":   tierFactAnswer,
		"下单意向": tierFactAnswer,
		"催单":   tierFactAnswer,
		"投诉":   tierFactAnswer,
		"闲聊":   tierAcknowledge,
		"刷屏":   tierAcknowledge,
	}
	for typ, want := range cases {
		raw, err := json.Marshal(interactionplan.Envelope{Protocol: interactionplan.Protocol, Task: "t-" + typ, Intent: interactionplan.Intent{Relevant: true, Type: typ}})
		if err != nil {
			t.Fatal(err)
		}
		got := classifyInteraction(decisionItem{ID: "d-" + typ, MissionKind: "reply_chat", PurposeKnowledge: raw})
		if got != want {
			t.Fatalf("type=%s got=%d want=%d", typ, got, want)
		}
	}
}

func TestClassifyInteractionFallsBackToMissionKind(t *testing.T) {
	if got := classifyInteraction(decisionItem{ID: "d1", MissionKind: "reply_question"}); got != tierFactAnswer {
		t.Fatalf("mission kind fallback got=%d", got)
	}
	if got := classifyInteraction(decisionItem{ID: "d2", MissionKind: "reply_like"}); got != tierAcknowledge {
		t.Fatalf("mission kind fallback got=%d", got)
	}
}
