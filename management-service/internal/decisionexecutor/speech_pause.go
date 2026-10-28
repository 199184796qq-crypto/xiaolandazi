package decisionexecutor

import (
	"encoding/json"
	"livecompanion/management/internal/readingrules"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// ttsPauseRules inserts breath/segment punctuation so the TTS engine does not
// glue a product name to the phrase before it ("南充特产绸都杨鸭子").
type ttsPauseRules struct {
	Version          int      `json:"version"`
	Pause            string   `json:"pause"`
	AfterPhrases     []string `json:"after_phrases"`
	BeforeTerms      []string `json:"before_terms"`
	MaxPausesPerLine int      `json:"max_pauses_per_line"`
}

var (
	ttsPauseOnce  sync.Once
	ttsPauseValue ttsPauseRules
)

func loadTTSPauseRules() ttsPauseRules {
	ttsPauseOnce.Do(func() {
		ttsPauseValue = ttsPauseRules{Pause: "，", MaxPausesPerLine: 2}
		paths := []string{}
		if path := strings.TrimSpace(os.Getenv("TTS_PAUSE_RULES")); path != "" {
			paths = append(paths, path)
		}
		if dir := strings.TrimSpace(os.Getenv("ANCHOR_STYLE_PROMPT_DIR")); dir != "" {
			paths = append(paths, filepath.Join(dir, "tts-pause-rules.json"))
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var rules ttsPauseRules
			if json.Unmarshal(raw, &rules) != nil {
				continue
			}
			if rules.Pause == "" {
				rules.Pause = "，"
			}
			ttsPauseValue = rules
			return
		}
	})
	return ttsPauseValue
}

// insertSpeechPauses adds at most MaxPausesPerLine pauses per line: one after a
// lead-in phrase and one before a product/brand term.
func insertSpeechPauses(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	rules := loadTTSPauseRules()
	if len(rules.AfterPhrases) == 0 && len(rules.BeforeTerms) == 0 {
		return text
	}
	text = repairSplitTerms(text, rules.BeforeTerms)
	// 长词优先：先匹「绸都杨鸭子」再匹「杨鸭子」，避免在品牌中间插停顿。
	beforeTerms := append([]string(nil), rules.BeforeTerms...)
	sort.SliceStable(beforeTerms, func(i, j int) bool { return len([]rune(beforeTerms[i])) > len([]rune(beforeTerms[j])) })
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		limit := rules.MaxPausesPerLine
		if limit <= 0 {
			limit = 2
		}
		added := 0
		for _, phrase := range rules.AfterPhrases {
			if added >= limit || phrase == "" || !strings.Contains(trimmed, phrase) {
				continue
			}
			beforeText := trimmed
			trimmed = pauseAfter(trimmed, phrase, rules.Pause)
			if trimmed != beforeText {
				added++
			}
		}
		for _, term := range beforeTerms {
			if added >= limit || term == "" || !strings.Contains(trimmed, term) {
				continue
			}
			before := trimmed
			trimmed = pauseBefore(trimmed, term, rules.Pause, beforeTerms)
			if trimmed != before {
				added++
			}
		}
		lines[i] = trimmed
	}
	return strings.Join(lines, "\n")
}

func pauseAfter(text, phrase, pause string) string {
	runes := []rune(text)
	target := []rune(phrase)
	for i := 0; i+len(target) <= len(runes); i++ {
		if string(runes[i:i+len(target)]) != phrase {
			continue
		}
		next := i + len(target)
		if next >= len(runes) || isPauseRune(runes[next]) || unicode.IsSpace(runes[next]) {
			continue
		}
		if runes[next] == '的' || runes[next] == '了' {
			continue
		}
		out := make([]rune, 0, len(runes)+1)
		out = append(out, runes[:next]...)
		out = append(out, []rune(pause)...)
		out = append(out, runes[next:]...)
		return string(out)
	}
	return text
}

func pauseBefore(text, term, pause string, terms []string) string {
	runes := []rune(text)
	target := []rune(term)
	for i := 0; i+len(target) <= len(runes); i++ {
		if string(runes[i:i+len(target)]) != term {
			continue
		}
		if insideLongerTerm(runes, i, term, terms) {
			continue
		}
		if i == 0 || isPauseRune(runes[i-1]) || unicode.IsSpace(runes[i-1]) {
			continue
		}
		out := make([]rune, 0, len(runes)+1)
		out = append(out, runes[:i]...)
		out = append(out, []rune(pause)...)
		out = append(out, runes[i:]...)
		return string(out)
	}
	return text
}

// insideLongerTerm prevents a shorter configured term ("杨鸭子") from matching
// inside a longer configured brand ("绸都杨鸭子") after the longer term already
// received its pause. The old long-first sort only controlled iteration order;
// it did not stop the shorter term from splitting the brand on a second pass.
func insideLongerTerm(runes []rune, at int, term string, terms []string) bool {
	target := []rune(term)
	for _, longer := range terms {
		long := []rune(longer)
		if len(long) <= len(target) || !strings.Contains(longer, term) {
			continue
		}
		for j := 0; j+len(long) <= len(runes); j++ {
			if string(runes[j:j+len(long)]) != longer {
				continue
			}
			if at >= j && at+len(target) <= j+len(long) {
				return true
			}
		}
	}
	return false
}

// repairSplitTerms repairs a brand that a previous pass or the model separated
// with punctuation, e.g. "绸都，杨鸭子" -> "绸都杨鸭子". It only touches
// configured protected terms, never arbitrary text.
func repairSplitTerms(text string, terms []string) string {
	for _, term := range terms {
		runes := []rune(term)
		if len(runes) < 2 {
			continue
		}
		for cut := 1; cut < len(runes); cut++ {
			left, right := string(runes[:cut]), string(runes[cut:])
			for _, sep := range []string{"，", ",", "、", "；", ";", " "} {
				text = strings.ReplaceAll(text, left+sep+right, term)
			}
		}
	}
	return text
}

func isPauseRune(r rune) bool {
	switch r {
	case '，', '。', '！', '？', '、', '；', '：', '…', '\n':
		return true
	}
	return false
}

// addressSSML wraps the text in SSML and inserts a short break right after the
// address, using the TTS model's native <break> support.
func addressSSML(text, address, pause string) (string, bool) {
	text = strings.TrimSpace(text)
	address = strings.TrimSpace(address)
	if text == "" || address == "" || pause == "" {
		return "", false
	}
	runes := []rune(text)
	target := []rune(address)
	for i := 0; i+len(target) <= len(runes); i++ {
		if string(runes[i:i+len(target)]) != address {
			continue
		}
		end := i + len(target)
		var b strings.Builder
		b.WriteString("<speak>")
		b.WriteString(escapeSSML(string(runes[:end])))
		b.WriteString("<break time=\"" + pause + "\"/>")
		b.WriteString(escapeSSML(string(runes[end:])))
		b.WriteString("</speak>")
		return b.String(), true
	}
	return "", false
}

func escapeSSML(text string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(text)
}

// buildTTSText applies the room's reading rules and, when the voice supports
// SSML, wraps everything with a native break after the address.
func buildTTSText(text, address string, rules []readingrules.Rule, ssml bool) (string, bool) {
	plain, body := readingrules.Apply(text, rules)
	if !ssml {
		return ensureAddressPunctuation(plain, address), false
	}
	doc := "<speak>" + body + "</speak>"
	if address = strings.TrimSpace(address); address != "" {
		escaped := escapeSSML(address)
		if strings.Contains(doc, escaped) {
			doc = strings.Replace(doc, escaped, escaped+`<break time="200ms"/>`, 1)
		}
	}
	return doc, true
}
