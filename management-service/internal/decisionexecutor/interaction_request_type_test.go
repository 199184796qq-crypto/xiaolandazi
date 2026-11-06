package decisionexecutor

import (
	"encoding/json"
	"testing"

	"livecompanion/management/internal/interactionplan"
)

func TestRequestTypeUsesExistingFactAnswerTier(t *testing.T) {
	for _, label := range []string{"请求", "要求"} {
		if tier, ok := typeTier(label); !ok || tier != tierFactAnswer {
			t.Fatalf("%s was not handled as a normal actionable reply: tier=%d ok=%v", label, tier, ok)
		}
		raw, err := json.Marshal(interactionplan.Envelope{Protocol: interactionplan.Protocol, Task: "request-type-test", Intent: interactionplan.Intent{Relevant: true, Question: true, Type: label, Priority: "普通"}, Body: "具体以客服确认为准。"})
		if err != nil {
			t.Fatal(err)
		}
		// The model's content type must take precedence over a generic chat event.
		// Otherwise a new label falls back to the chat tier instead of the established reply path.
		item := decisionItem{MissionKind: "reply_chat", PurposeKnowledge: raw}
		if got := classifyInteraction(item); got != tierFactAnswer {
			t.Fatalf("%s fell back to mission kind: got tier=%d", label, got)
		}
	}
}
