package contentstrategy

import (
	"encoding/json"
	"fmt"
	"strings"
)

func normalizedTransportKey(text string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.TrimSpace(text)))
}
func canonicalConditionType(text string) string {
	key := normalizedTransportKey(text)
	aliases := map[string]string{"always": "always", "一般适用": "always", "无条件": "always", "question": "question", "仅互动": "question", "互动": "question", "mainline": "mainline", "仅主线": "mainline", "主线": "mainline", "activebenefit": "active_benefit", "有效福利": "active_benefit", "有效优惠": "active_benefit", "purposematch": "purpose_match", "目的匹配": "purpose_match", "匹配目的": "purpose_match", "taskcontains": "task_contains", "任务包含": "task_contains", "任务匹配": "task_contains"}
	if value, ok := aliases[key]; ok {
		return value
	}
	return strings.TrimSpace(text)
}

// Compatible names and equivalent values are normalized explicitly. Raw model
// text remains separately retained; ambiguous execution meanings are rejected.
func (c *Condition) UnmarshalJSON(raw []byte) error {
	if len(raw) > 0 && raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return err
		}
		*c = Condition{Type: canonicalConditionType(text)}
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	var result Condition
	hasType, hasValue := false, false
	for key, value := range fields {
		var target string
		switch normalizedTransportKey(key) {
		case "类型", "type", "条件类型":
			target = "type"
		case "值", "value", "条件值", "目的编号", "purposeid", "关键词":
			target = "value"
		default:
			return fmt.Errorf("unknown condition field %q", key)
		}
		var text string
		if string(value) != "null" {
			if err := json.Unmarshal(value, &text); err != nil {
				return err
			}
		}
		text = strings.TrimSpace(text)
		if target == "type" {
			text = canonicalConditionType(text)
			if hasType && result.Type != text {
				return fmt.Errorf("conflicting condition type fields")
			}
			result.Type = text
			hasType = true
		} else {
			if hasValue && result.Value != text {
				return fmt.Errorf("conflicting condition value fields")
			}
			result.Value = text
			hasValue = true
		}
	}
	if !hasType || result.Type == "" {
		return fmt.Errorf("condition type missing")
	}
	if result.Type != "purpose_match" && result.Type != "task_contains" {
		for key := range fields {
			if normalizedTransportKey(key) == "目的编号" || normalizedTransportKey(key) == "purposeid" || normalizedTransportKey(key) == "关键词" {
				return fmt.Errorf("condition value alias incompatible with type")
			}
		}
	}
	*c = result
	return nil
}
