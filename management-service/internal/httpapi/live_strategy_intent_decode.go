package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"livecompanion/management/internal/agentunderstanding"
)

var errLiveStrategyIntentModelResponse = errors.New("decode live strategy intent")

// Normalize content at the model boundary, keeping the existing string API.
// Do not coerce IDs, intent, confidence, role lists or arbitrary objects.
func decodeLiveStrategyIntentModelOutput(raw string) (liveStrategyIntentModelOutput, error) {
	var output liveStrategyIntentModelOutput
	var envelope map[string]json.RawMessage
	if err := decodeProductJSON(raw, &envelope); err != nil {
		return output, fmt.Errorf("%w: %v", errLiveStrategyIntentModelResponse, err)
	}
	if changesRaw := envelope["changes"]; len(changesRaw) > 0 && string(changesRaw) != "null" {
		var changes map[string]json.RawMessage
		if err := json.Unmarshal(changesRaw, &changes); err != nil {
			return output, fmt.Errorf("%w: changes: %v", errLiveStrategyIntentModelResponse, err)
		}
		// RawMessage keeps numbers exact: no float64 rounding or invented units.
		for _, key := range []string{"spec", "daily_price", "quantity", "attribute_value", "activity_price", "fact_value"} {
			value := strings.TrimSpace(string(changes[key]))
			if value != "" && (value[0] == '-' || value[0] >= '0' && value[0] <= '9') {
				changes[key], _ = json.Marshal(value)
			}
		}
		if value := strings.TrimSpace(string(changes["audience"])); strings.HasPrefix(value, "[") {
			var items []json.RawMessage
			if err := json.Unmarshal([]byte(value), &items); err != nil {
				return output, fmt.Errorf("%w: changes.audience: %v", errLiveStrategyIntentModelResponse, err)
			}
			texts := make([]string, 0, len(items))
			for _, item := range items {
				var text string
				if !strings.HasPrefix(strings.TrimSpace(string(item)), `"`) || json.Unmarshal(item, &text) != nil {
					return output, fmt.Errorf("%w: changes.audience must contain only text", errLiveStrategyIntentModelResponse)
				}
				texts = append(texts, text)
			}
			changes["audience"], _ = json.Marshal(strings.Join(texts, "、"))
		}
		envelope["changes"], _ = json.Marshal(changes)
	}
	canonical, err := json.Marshal(envelope)
	if err == nil {
		// Retain strict checks for unknown fields and all non-content types.
		err = decodeProductJSON(string(canonical), &output)
	}
	if err != nil {
		return liveStrategyIntentModelOutput{}, fmt.Errorf("%w: %v", errLiveStrategyIntentModelResponse, err)
	}
	return output, nil
}

func liveStrategyIntentModelFallback(program agentunderstanding.UnifiedIntent, policySource string, cause error) liveStrategyIntentOutput {
	fallback := liveStrategyProgramOutput(program)
	code := "model_call_failed"
	reply := "理解服务暂时未能完成请求。我没有执行任何修改，请稍后重试本次操作。"
	if errors.Is(cause, errLiveStrategyIntentModelResponse) {
		code = "model_response_invalid"
		reply = "模型已返回结果，但系统未能正确解析。我没有执行任何修改，请重试本次操作。"
	}
	if fallback.Reply == "" {
		fallback.Reply = reply
	}
	return liveStrategyIntentOutput{
		ProtocolVersion: agentProtocolVersion, State: agentStateResponded, Code: code,
		Kind: fallback.Kind, Intent: fallback.Intent, Reply: fallback.Reply,
		Target: fallback.Target, Changes: fallback.Changes, Missing: fallback.Missing,
		Confidence: fallback.Confidence, Engine: "program_fallback", PolicySource: policySource,
	}
}
