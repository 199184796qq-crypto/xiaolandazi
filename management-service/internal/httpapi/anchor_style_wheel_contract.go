package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
)

var sellingPointHighRiskExpansionPhrases = []string{
	"现发", "现场", "当天", "全店", "全部地区", "独家", "特有", "官方",
	"适合多人", "适合送礼", "确保", "保证", "更快", "更稳", "更可靠", "更划算",
	"不使用其他", "不使用其它", "不包邮",
}

func optionalWheelText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// Check the template actually sent; a new mother does not invalidate old pools.
func validateSellingPointPromptContract(raw, prompt string) error {
	if _, _, _, err := normalizeAnchorStyleSellingPointWheelJSON(raw); err != nil {
		return err
	}
	var rawTop map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(raw)), &rawTop); err != nil {
		return err
	}
	if strings.Contains(prompt, `"事实补充建议"`) && rawTop["事实补充建议"] == nil {
		return fmt.Errorf("缺少母版要求的“事实补充建议”，原始返回已保留，请重新生成；不会由程序编造补齐")
	}
	var top struct {
		Items []map[string]json.RawMessage `json:"卖点列表"`
	}
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(raw)), &top); err != nil {
		return err
	}
	for _, key := range []string{"目标人群", "痛点", "归属"} {
		if !strings.Contains(prompt, `"`+key+`"`) {
			continue
		}
		for i, item := range top.Items {
			if _, ok := item[key]; !ok {
				return fmt.Errorf("第%d个卖点缺少母版要求的“%s”，原始返回已保留，请重新生成；不会由程序编造补齐", i+1, key)
			}
		}
	}
	return nil
}

func sellingPointFactBoundaryViolations(raw, factPackage string) []string {
	var wheel anchorStyleSellingPointWheel
	if json.Unmarshal([]byte(stripPolicyJSONFence(raw)), &wheel) != nil {
		return []string{"轮盘JSON无法解析"}
	}
	evidence := sellingPointFactEvidenceBody(factPackage)
	violations := make([]string, 0)
	seen := map[string]bool{}
	for _, item := range wheel.Items {
		texts := []string{optionalWheelText(item.TargetAudience), optionalWheelText(item.PainPoint)}
		texts = append(texts, item.Angles...)
		for _, text := range texts {
			for _, phrase := range sellingPointHighRiskExpansionPhrases {
				if !strings.Contains(text, phrase) || strings.Contains(evidence, phrase) {
					continue
				}
				message := fmt.Sprintf("卖点“%s”出现事实正文未提供的“%s”", item.Name, phrase)
				if !seen[message] {
					seen[message] = true
					violations = append(violations, message)
				}
			}
		}
	}
	return violations
}

func removeSellingPointFactBoundaryViolations(raw, factPackage string) (string, []string, int, int, error) {
	var wheel anchorStyleSellingPointWheel
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(raw)), &wheel); err != nil {
		return "", nil, 0, 0, err
	}
	evidence := sellingPointFactEvidenceBody(factPackage)
	removed := make([]string, 0)
	items := make([]anchorStyleSellingPointWheelItem, 0, len(wheel.Items))
	for _, item := range wheel.Items {
		if phrases := unsupportedSellingPointPhrases(optionalWheelText(item.TargetAudience), evidence); len(phrases) > 0 {
			removed = append(removed, fmt.Sprintf("删除卖点“%s”：目标人群含%s", item.Name, strings.Join(phrases, "、")))
			continue
		}
		if phrases := unsupportedSellingPointPhrases(optionalWheelText(item.PainPoint), evidence); len(phrases) > 0 {
			removed = append(removed, fmt.Sprintf("删除卖点“%s”：痛点含%s", item.Name, strings.Join(phrases, "、")))
			continue
		}
		angles := make([]string, 0, len(item.Angles))
		for _, angle := range item.Angles {
			phrases := unsupportedSellingPointPhrases(angle, evidence)
			if len(phrases) > 0 {
				removed = append(removed, fmt.Sprintf("删除卖点“%s”的角度“%s”：含%s", item.Name, angle, strings.Join(phrases, "、")))
				continue
			}
			angles = append(angles, angle)
		}
		if len(angles) == 0 {
			removed = append(removed, fmt.Sprintf("删除卖点“%s”：角度均未通过事实正文硬校验", item.Name))
			continue
		}
		item.Angles = angles
		items = append(items, item)
	}
	if len(items) == 0 {
		return "", removed, 0, 0, fmt.Errorf("事实正文硬校验删除了全部卖点")
	}
	wheel.Items = items
	encoded, err := json.Marshal(wheel)
	if err != nil {
		return "", removed, 0, 0, err
	}
	canonical, points, angles, err := normalizeAnchorStyleSellingPointWheelJSON(string(encoded))
	return canonical, removed, points, angles, err
}

func unsupportedSellingPointPhrases(text, evidence string) []string {
	phrases := make([]string, 0)
	for _, phrase := range sellingPointHighRiskExpansionPhrases {
		if strings.Contains(text, phrase) && !strings.Contains(evidence, phrase) {
			phrases = append(phrases, phrase)
		}
	}
	return phrases
}

func sellingPointFactEvidenceBody(factPackage string) string {
	lines := make([]string, 0)
	for _, rawLine := range strings.Split(factPackage, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "- [") {
			if close := strings.Index(line, "] "); close >= 0 {
				line = strings.TrimSpace(line[close+2:])
			}
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "方案：") || strings.HasPrefix(line, "边界：") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
