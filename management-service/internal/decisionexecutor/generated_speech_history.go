package decisionexecutor

import (
	"encoding/json"
	"strings"
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
	raw, err := json.Marshal(metadata)
	if err != nil {
		return string(item.PurposeKnowledge)
	}
	return string(raw)
}
