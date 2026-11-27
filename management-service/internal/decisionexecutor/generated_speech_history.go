package decisionexecutor

import (
	"encoding/json"
	"strings"

	"livecompanion/management/internal/interactionplan"
)

// History records program-owned task identity separately from the model's
// semantic type. This copy never changes the prepared body or model input.
func generatedSpeechHistoryIntentJSON(item *decisionItem) string {
	if item == nil {
		return ""
	}
	var metadata map[string]json.RawMessage
	if json.Unmarshal(item.PurposeKnowledge, &metadata) != nil || metadata == nil {
		metadata = map[string]json.RawMessage{}
		if len(item.PurposeKnowledge) > 0 {
			metadata["legacy_intent"], _ = json.Marshal(string(item.PurposeKnowledge))
		}
	}
	for key, value := range map[string]string{
		"mission_kind": item.MissionKind,
		"event_kind":   item.InteractionDecision.PrimaryEvent,
	} {
		delete(metadata, key)
		if value = strings.TrimSpace(value); value != "" {
			metadata[key], _ = json.Marshal(value)
		}
	}
	if envelope, ok := interactionplan.Decode(item.PurposeKnowledge); ok && envelope.Template != "" {
		metadata["model_stages"], _ = json.Marshal(interactionplan.ModelStages{Answer: &interactionplan.ModelStage{Thinking: "no_model"}})
	} else if strings.EqualFold(strings.TrimSpace(item.ExecutionMode), "verbatim") {
		metadata["model_stages"], _ = json.Marshal(interactionplan.ModelStages{Answer: &interactionplan.ModelStage{Thinking: "no_model"}})
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return string(item.PurposeKnowledge)
	}
	return string(raw)
}
