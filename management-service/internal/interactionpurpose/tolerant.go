package interactionpurpose

import (
	"encoding/json"
	"strings"

	"livecompanion/management/internal/contentstrategy"
)

// Model output for task angles is advisory. One unknown condition field must not
// throw the whole intent away: decode leniently, keep the usable rows and drop
// the rest, so question/relevant/purpose still reach the answer path.
var allowedConditionTypes = map[string]string{
	"always": "always", "一般适用": "always", "无条件": "always",
	"question": "question", "仅互动": "question", "互动": "question",
	"mainline": "mainline", "仅主线": "mainline", "主线": "mainline",
	"active_benefit": "active_benefit", "有效福利": "active_benefit", "有效优惠": "active_benefit",
	"purpose_match": "purpose_match", "目的匹配": "purpose_match", "匹配目的": "purpose_match",
	"task_contains": "task_contains", "任务包含": "task_contains", "任务匹配": "task_contains",
}

type tolerantEnvelope struct {
	Question   bool              `json:"question"`
	Relevant   bool              `json:"relevant"`
	Confidence float64           `json:"confidence"`
	PurposeID  string            `json:"purpose_id"`
	Purpose    string            `json:"purpose"`
	Scope      string            `json:"scope"`
	TaskAngles []json.RawMessage `json:"task_angles"`
}

type tolerantRow struct {
	PurposeID  string           `json:"目的编号"`
	Angle      string           `json:"角度"`
	Task       string           `json:"讲述任务"`
	FactIDs    []string         `json:"关联事实"`
	When       string           `json:"适用条件"`
	Conditions []map[string]any `json:"采用条件"`
}

// purposeRetryInstruction is sent once when the first answer is uncertain. It
// asks for a decision instead of a guess, so the question is not dropped.
const purposeRetryInstruction = "上一次输出目的不明确（purpose 为空，或 confidence 低于 0.75）。请重新判断：" +
	"能从已有目的中匹配时必须给出对应 purpose_id；如果是新目的，给出明确、通用、简短的目的；" +
	"与业务无关就把 relevant 设为 false。仍只输出约定 JSON，不要重复上一次的不确定结果。"

// purposeKnown reports whether the model used an id from the plan's own list.
func purposeKnown(purposes []contentstrategy.Purpose, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, purpose := range purposes {
		if purpose.ID == id {
			return true
		}
	}
	return false
}

// decodeIntentResult tries the strict contract first, then the lenient one so a
// malformed angle row cannot discard the whole intent.
func decodeIntentResult(raw, facts string) (Result, error) {
	var result Result
	if err := contentstrategy.Decode(raw, &result); err == nil {
		return result, nil
	}
	known := map[string]bool{}
	for _, f := range contentstrategy.Facts(facts) {
		known[f.ID] = true
	}
	return decodeTolerantResult(raw, known)
}

// decodeTolerantResult keeps the intent when only the angle rows are malformed.
func decodeTolerantResult(raw string, knownFacts map[string]bool) (Result, error) {
	var envelope tolerantEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return Result{}, err
	}
	result := Result{
		Question:   envelope.Question,
		Relevant:   envelope.Relevant,
		Confidence: envelope.Confidence,
		PurposeID:  strings.TrimSpace(envelope.PurposeID),
		Purpose:    strings.TrimSpace(envelope.Purpose),
		Scope:      strings.TrimSpace(envelope.Scope),
	}
	for _, rawRow := range envelope.TaskAngles {
		var row tolerantRow
		if json.Unmarshal(rawRow, &row) != nil {
			continue
		}
		row.Angle = strings.TrimSpace(row.Angle)
		row.Task = strings.TrimSpace(row.Task)
		if row.Angle == "" || row.Task == "" {
			continue
		}
		var conditions []contentstrategy.Condition
		for _, condition := range row.Conditions {
			typeText, _ := condition["类型"].(string)
			value, _ := condition["值"].(string)
			canonical := allowedConditionTypes[strings.TrimSpace(typeText)]
			if canonical == "" {
				continue
			}
			conditions = append(conditions, contentstrategy.Condition{Type: canonical, Value: strings.TrimSpace(value)})
		}
		var facts []string
		for _, id := range row.FactIDs {
			if knownFacts[strings.TrimSpace(id)] {
				facts = append(facts, strings.TrimSpace(id))
			}
		}
		if len(facts) == 0 {
			continue
		}
		result.Angles = append(result.Angles, contentstrategy.Row{
			PurposeID:  result.PurposeID,
			Angle:      row.Angle,
			Task:       row.Task,
			FactIDs:    facts,
			When:       row.When,
			Conditions: conditions,
		})
	}
	return result, nil
}

// pruneAngles drops angle rows the program cannot safely use, keeping the rest.
func pruneAngles(rows []contentstrategy.Row, knownFacts map[string]bool) []contentstrategy.Row {
	kept := make([]contentstrategy.Row, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Angle) == "" || strings.TrimSpace(row.Task) == "" {
			continue
		}
		if err := contentstrategy.ValidateRowPolicy(row, false); err != nil {
			continue
		}
		facts := make([]string, 0, len(row.FactIDs))
		for _, id := range row.FactIDs {
			if knownFacts[strings.TrimSpace(id)] {
				facts = append(facts, strings.TrimSpace(id))
			}
		}
		if len(facts) == 0 {
			continue
		}
		row.FactIDs = facts
		kept = append(kept, row)
	}
	return kept
}
