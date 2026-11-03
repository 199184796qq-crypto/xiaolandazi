package interactionplan

import (
	"errors"
	"strconv"
	"strings"
)

// IntentAlternative is non-spoken metadata; only Body enters the voice chain.
type IntentAlternative struct {
	Meaning    string `json:"meaning"`
	Confidence *int   `json:"confidence,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

func parseIntentLines(value string) ([]IntentAlternative, error) {
	lines := strings.Split(value, "\n")
	if len(lines) < 1 || len(lines) > 3 {
		return nil, errors.New("互动意图须为一至三条")
	}
	var result []IntentAlternative
	last := 101
	for _, line := range lines {
		parts := strings.Split(strings.ReplaceAll(line, "｜", "|"), "|")
		entry := IntentAlternative{Meaning: strings.TrimSpace(parts[0])}
		if entry.Meaning == "" {
			return nil, errors.New("互动意图不能为空")
		}
		for _, part := range parts[1:] {
			key, val, ok := splitKVLine(part)
			if !ok {
				return nil, errors.New("互动意图附加字段格式错误")
			}
			switch key {
			case "置信度":
				n, err := strconv.Atoi(val)
				if err != nil || n < 0 || n > 100 || entry.Confidence != nil {
					return nil, errors.New("互动置信度须为0到100的整数")
				}
				entry.Confidence = &n
			case "建议":
				entry.Suggestion = val
			default:
				return nil, errors.New("互动意图含未知字段")
			}
		}
		if len(lines) > 1 && entry.Confidence == nil {
			return nil, errors.New("多条互动意图缺少置信度")
		}
		if entry.Confidence != nil {
			if *entry.Confidence > last {
				return nil, errors.New("互动意图未按置信度降序")
			}
			last = *entry.Confidence
		}
		result = append(result, entry)
	}
	return result, nil
}

func splitKVLine(line string) (string, string, bool) {
	pos := strings.IndexAny(line, "：:")
	if pos <= 0 {
		return "", "", false
	}
	width := 1
	if strings.HasPrefix(line[pos:], "：") {
		width = len("：")
	}
	return strings.TrimSpace(line[:pos]), strings.TrimSpace(line[pos+width:]), true
}

func isInteractionField(key string) bool {
	switch key {
	case "意图", "类型", "优先程度", "优先原因", "歧义", "候选", "澄清句", "话术要求", "话术", "回答", "对话关系", "继续对话":
		return true
	}
	return false
}

func bodyField(key string) bool { return key == "话术" || key == "回答" }

func validateContentFields(kv map[string]string) error {
	_, unified := kv["话术"]
	_, old := kv["回答"]
	if unified && old {
		return errors.New("互动结果含两个正文字段")
	}
	if v, ok := kv["歧义"]; ok && v != "是" && v != "否" {
		return errors.New("互动歧义字段须为是或否")
	}
	if unified {
		if _, ok := kv["歧义"]; !ok {
			return errors.New("互动结果缺少歧义判断")
		}
		if kv["歧义"] == "是" {
			candidates := strings.Split(kv["候选"], "、")
			if len(candidates) < 2 || len(candidates) > 3 || strings.TrimSpace(kv["话术"]) != "" {
				return errors.New("歧义结果需要两三个候选且话术留空")
			}
			seen := map[string]bool{}
			for _, candidate := range candidates {
				candidate = strings.TrimSpace(candidate)
				if candidate == "" || seen[candidate] {
					return errors.New("歧义候选为空或重复")
				}
				seen[candidate] = true
			}
		} else if strings.TrimSpace(kv["候选"]+kv["澄清句"]) != "" {
			return errors.New("非歧义结果候选及澄清句须留空")
		}
	}
	return nil
}
