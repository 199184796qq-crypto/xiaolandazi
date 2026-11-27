package interactionplan

import (
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
)

func TestModelStagesRecordCompletedCalls(t *testing.T) {
	var stages ModelStages
	stages.Record("interaction_v2_address_join", agentgateway.Response{ThinkingMode: "off"}, time.Second)
	if stages.Recorded() != nil {
		t.Fatal("address joining should not be measured as answering")
	}

	stages.Record("interaction_answerflow_batch_intent", agentgateway.Response{ThinkingMode: "off", LatencyMS: 84}, 1016*time.Millisecond)
	stages.Record("interaction_answerflow_answer", agentgateway.Response{ThinkingMode: "low", LatencyMS: 87}, 2765*time.Millisecond)
	if got := stages.Recorded(); got == nil || *got.Intent != (ModelStage{Thinking: "off", ElapsedMS: 1016}) ||
		*got.Answer != (ModelStage{Thinking: "low", ElapsedMS: 2765}) {
		t.Fatalf("unexpected archived question diagnostics: %+v", got)
	}

	var welcome ModelStages
	welcome.Record("interaction_welcome_writer", agentgateway.Response{ThinkingMode: "off", LatencyMS: 90}, 1309*time.Millisecond)
	if got := welcome.Recorded(); got == nil || got.Intent != nil ||
		*got.Answer != (ModelStage{Thinking: "off", ElapsedMS: 1309}) {
		t.Fatalf("unexpected archived welcome diagnostics: %+v", got)
	}
}
