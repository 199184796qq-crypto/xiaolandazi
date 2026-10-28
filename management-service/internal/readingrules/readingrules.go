package readingrules

import (
	"regexp"
	"strings"
	"unicode"
)

// Rule is one line of the room's 读写规则.
//
//	南充特产绸都杨鸭子 => 南充特产|绸都杨鸭子   // | 处插入 200ms 停顿
//	绸都 => chou2 du1                        // 原文按给定拼音朗读
type Rule struct {
	Source string
	Kind   string // break | pronounce
	Breaks string // break 规则：已把 | 保留为分隔
	Pinyin string
}

var pinyinPattern = regexp.MustCompile(`^[a-zA-Z]+[1-5](\s+[a-zA-Z]+[1-5])*$`)

// Parse turns the textarea content into rules, skipping malformed lines.
func Parse(text string) []Rule {
	rules := make([]Rule, 0, 8)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		source, target, ok := strings.Cut(line, "=>")
		if !ok {
			continue
		}
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if source == "" || target == "" {
			continue
		}
		if strings.Contains(target, "|") {
			rules = append(rules, Rule{Source: source, Kind: "break", Breaks: target})
			continue
		}
		if pinyinPattern.MatchString(target) && len(strings.Fields(target)) == len([]rune(source)) {
			rules = append(rules, Rule{Source: source, Kind: "pronounce", Pinyin: target})
		}
	}
	return rules
}

// Apply returns the plain text (breaks become commas) and the SSML body with
// native pauses and phoneme tags.
func Apply(text string, rules []Rule) (plain string, ssml string) {
	plain = text
	ssml = escape(text)
	for _, rule := range rules {
		switch rule.Kind {
		case "break":
			plain = strings.ReplaceAll(plain, rule.Source, strings.ReplaceAll(rule.Breaks, "|", "，"))
			ssml = strings.ReplaceAll(ssml, escape(rule.Source), breakBody(rule.Breaks))
		case "pronounce":
			ssml = strings.ReplaceAll(ssml, escape(rule.Source),
				`<phoneme alphabet="py" ph="`+rule.Pinyin+`">`+escape(rule.Source)+`</phoneme>`)
		}
	}
	return plain, ssml
}

func breakBody(pattern string) string {
	parts := strings.Split(pattern, "|")
	for i := range parts {
		parts[i] = escape(parts[i])
	}
	return strings.Join(parts, `<break time="200ms"/>`)
}

func escape(text string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(text)
}

// LooksLikeRule reports whether a line has the expected "原文 => 规则" shape.
func LooksLikeRule(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	source, target, ok := strings.Cut(line, "=>")
	if !ok {
		return false
	}
	return strings.TrimSpace(source) != "" && strings.TrimSpace(target) != "" &&
		(hasHan(source) || hasHan(target) || strings.Contains(target, "|"))
}

func hasHan(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
