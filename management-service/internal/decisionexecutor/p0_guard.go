package decisionexecutor

import (
	"regexp"
	"strings"
)

var p0Patterns = []*regexp.Regexp{
	regexp.MustCompile(`治疗|治愈|根治|降血压|降血糖|降血脂|增强免疫|抗癌|防癌|补肾|壮阳`),
	regexp.MustCompile(`最好|最低|最强|最大|第一名|顶级|绝对|百分百|100%|永不|彻底`),
	regexp.MustCompile(`保证(?:到|发|能|有效|治好)|一定(?:到|能到|发)|假一赔十|包赔|赔偿`),
}

// auditP0 removes only forbidden P0 claims. It deliberately does not compare
// numbers, units, prices, stock or wording against the fact package.
func auditP0(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	kept := make([]string, 0, 4)
	for _, sentence := range splitTTSSentences(text) {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		blocked := false
		for _, pattern := range p0Patterns {
			if pattern.MatchString(sentence) {
				blocked = true
				break
			}
		}
		if !blocked {
			kept = append(kept, sentence)
		}
	}
	return strings.TrimSpace(strings.Join(kept, ""))
}
