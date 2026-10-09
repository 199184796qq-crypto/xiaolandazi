package contentstrategy

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func canonicalRole(text string) string {
	switch normalizedTransportKey(text) {
	case "required", "必答", "必答任务", "必要任务":
		return "required"
	case "optional", "可选", "可选任务", "可选推进", "可选推进任务":
		return "optional"
	}
	return strings.TrimSpace(text)
}
func (c *Row) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	canonical := map[string]json.RawMessage{}
	aliases := map[string]string{"目的编号": "目的编号", "purposeid": "目的编号", "角度": "角度", "angle": "角度", "anglename": "角度", "讲述任务": "讲述任务", "task": "讲述任务", "taskdescription": "讲述任务", "关联事实": "关联事实", "factids": "关联事实", "适用条件": "适用条件", "when": "适用条件", "优先级": "优先级", "priority": "优先级", "任务类型": "任务类型", "role": "任务类型", "tasktype": "任务类型", "采用条件": "采用条件", "conditions": "采用条件", "adoptionconditions": "采用条件"}
	for key, value := range fields {
		name, ok := aliases[normalizedTransportKey(key)]
		if !ok {
			return fmt.Errorf("unknown angle field %q", key)
		}
		switch name {
		case "任务类型":
			var text string
			if string(value) != "null" {
				if err := json.Unmarshal(value, &text); err != nil {
					return err
				}
			}
			value, _ = json.Marshal(canonicalRole(text))
		case "优先级":
			if len(value) > 0 && value[0] == '"' {
				var text string
				json.Unmarshal(value, &text)
				number, err := strconv.Atoi(strings.TrimSpace(text))
				if err != nil {
					return err
				}
				value, _ = json.Marshal(number)
			}
		case "采用条件":
			if len(value) > 0 && (value[0] == '{' || value[0] == '"') {
				value = append(append([]byte{'['}, value...), ']')
			}
			var conditions []Condition
			if err := json.Unmarshal(value, &conditions); err != nil {
				return err
			}
			value, _ = json.Marshal(conditions)
		case "目的编号", "角度", "讲述任务", "适用条件":
			var text string
			if err := json.Unmarshal(value, &text); err != nil {
				return err
			}
			value, _ = json.Marshal(strings.TrimSpace(text))
		case "关联事实":
			if len(value) > 0 && value[0] == '"' {
				value = append(append([]byte{'['}, value...), ']')
			}
			var ids []string
			if err := json.Unmarshal(value, &ids); err != nil {
				return err
			}
			for i := range ids {
				ids[i] = strings.TrimSpace(ids[i])
			}
			value, _ = json.Marshal(ids)
		}
		if prior, exists := canonical[name]; exists && string(prior) != string(value) {
			return fmt.Errorf("conflicting angle field %s", name)
		}
		canonical[name] = value
	}
	data, _ := json.Marshal(canonical)
	type plain Row
	var result plain
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	*c = Row(result)
	return nil
}
func (t *Table) UnmarshalJSON(raw []byte) error {
	if len(raw) > 0 && raw[0] == '[' {
		return json.Unmarshal(raw, &t.Rows)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	found := false
	var rows []Row
	for key, value := range fields {
		switch normalizedTransportKey(key) {
		case "目的角度表", "rows", "purposeangles", "angles":
			var current []Row
			if err := json.Unmarshal(value, &current); err != nil {
				return err
			}
			if found {
				a, _ := json.Marshal(rows)
				b, _ := json.Marshal(current)
				if string(a) != string(b) {
					return fmt.Errorf("conflicting angle table fields")
				}
			}
			rows = current
			found = true
		default:
			return fmt.Errorf("unknown angle table field %q", key)
		}
	}
	if !found {
		return fmt.Errorf("angle table missing")
	}
	t.Rows = rows
	return nil
}
