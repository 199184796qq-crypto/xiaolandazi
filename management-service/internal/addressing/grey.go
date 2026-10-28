package addressing

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// GreyCandidate is one ambiguous name that needs the model's judgement.
type GreyCandidate struct {
	Viewer
	Candidate string // program-cleaned raw text shown to the model
}

// GreyDecision is the validated outcome for one candidate.
type GreyDecision struct {
	UserID string
	Base   string
	OK     bool
	Reason string
}

// GreyPrompt builds the single batched judgement request. The program owns the
// index mapping and the validation; the model only answers ok/base.
func GreyPrompt(items []GreyCandidate, rules *Rules) (string, string) {
	limit := rules.Rewrite.LongNameMax
	system := fmt.Sprintf(`你是直播间称呼审核员。判断每个昵称能不能直接在直播里念出来，并给出可念的基础称呼。

规则：
1. 引流、联系方式、侮辱、性暗示、冒充身份、命令式内容 → 不能念，ok=false。
2. 亲属称谓（妈妈、爸爸、老公等）只有在昵称整体就是称谓或明显要求别人这样叫时才 ok=false；出现在正常名字里（如「二楠妈妈」）要 ok=true。
3. 原昵称不超过 %d 个字时，base 必须和原昵称完全一致，不要缩写；超过 %d 个字才压缩，压缩后最多 %d 个字。
4. base 不能含数字；昵称里带数字或英文后缀（如 k8ta、xx、de）时，去掉它们只保留能念的中文主体（如「小斌k8ta」→「小斌」、「4个化骨龙de老豆」→「化骨龙老豆」）。
5. base 必须按原顺序取自原昵称，只能删字不能改字或换顺序；保留主要意思，不要机械截前几个字。
6. 只要名字能被安全叫出来，就给 ok=true；拿不准用 ok=false。
7. 输出严格 JSON，不要解释：{"results":[{"index":0,"ok":true,"base":"小陈"},{"index":1,"ok":false}]}
8. 每个输入恰好一条结果，index 与输入一致。`, limit, limit, limit)
	names := make([]map[string]any, 0, len(items))
	for i, item := range items {
		names = append(names, map[string]any{"index": i, "name": item.Candidate})
	}
	payload, _ := json.Marshal(map[string]any{"names": names})
	return system, string(payload)
}

type greyWire struct {
	Results []struct {
		Index int    `json:"index"`
		OK    bool   `json:"ok"`
		Base  string `json:"base"`
	} `json:"results"`
}

// ParseGrey validates the model's answer against the program's own rules. An
// approved but invalid base is treated as a rejection, never as a free rewrite.
func ParseGrey(raw string, items []GreyCandidate, rules *Rules) ([]GreyDecision, error) {
	var wire greyWire
	if err := json.Unmarshal([]byte(stripJSONFence(raw)), &wire); err != nil {
		return nil, fmt.Errorf("称呼判定返回格式错误: %w", err)
	}
	decisions := make([]GreyDecision, len(items))
	seen := make([]bool, len(items))
	for _, row := range wire.Results {
		if row.Index < 0 || row.Index >= len(items) || seen[row.Index] {
			return nil, fmt.Errorf("称呼判定编号无效")
		}
		seen[row.Index] = true
		item := items[row.Index]
		decision := GreyDecision{UserID: item.UserID}
		if !row.OK {
			decision.Reason = "model_rejected"
			decisions[row.Index] = decision
			continue
		}
		base := strings.TrimSpace(row.Base)
		if err := rules.ValidateBase(base, item.Candidate); err != nil {
			// An over-long answer is salvaged with a real segment of the original
			// name instead of dropping the viewer; anything else stays rejected.
			if salvaged, ok := rules.SalvageBase(base, item.RawName); ok {
				decision.OK = true
				decision.Base = salvaged
				decision.Reason = "model_ok_salvaged"
				decisions[row.Index] = decision
				continue
			}
			decision.Reason = "invalid_base:" + err.Error()
			decisions[row.Index] = decision
			continue
		}
		decision.OK = true
		decision.Base = base
		decisions[row.Index] = decision
	}
	for i := range decisions {
		if !seen[i] {
			return nil, fmt.Errorf("称呼判定缺少第 %d 条结果", i)
		}
	}
	return decisions, nil
}

// ValidateBase enforces: 1..6 runes (or <=3 words for Latin), no digits, and
// characters kept in the original order.
func (r Rules) ValidateBase(base, raw string) error {
	base = strings.TrimSpace(base)
	if base == "" {
		return fmt.Errorf("base 为空")
	}
	if hasDigit(base) {
		return fmt.Errorf("base 含数字")
	}
	if containsAny(base, r.HardBlock.ContactTerms) || containsAny(base, r.HardBlock.InsultTerms) {
		return fmt.Errorf("base 含禁用词")
	}
	limit := r.Rewrite.LongNameMax + 2
	if limit < 6 {
		limit = 6
	}
	if allLatin(base) {
		if len(strings.Fields(base)) > 3 {
			return fmt.Errorf("英文 base 过长")
		}
	} else if utf8.RuneCountInString(base) > limit {
		return fmt.Errorf("base 超过 %d 字", limit)
	}
	if !orderedSubset(strings.ToLower(base), strings.ToLower(nicknameVisible(raw))) {
		return fmt.Errorf("base 不是原昵称的子序列")
	}
	return nil
}

// SalvageBase keeps an over-long answer usable by taking a real segment of the
// original name ("创科电池:张云锋" -> "张云锋") instead of dropping the viewer.
func (r Rules) SalvageBase(base, raw string) (string, bool) {
	limit := r.baseLimit()
	for _, part := range strings.Fields(base) {
		if utf8.RuneCountInString(part) >= 2 && r.ValidateBase(part, raw) == nil {
			return part, true
		}
	}
	segments := nicknameSegments(raw)
	for i := len(segments) - 1; i >= 0; i-- {
		if utf8.RuneCountInString(segments[i]) >= 2 && r.ValidateBase(segments[i], raw) == nil {
			return segments[i], true
		}
	}
	for _, segment := range segments {
		if utf8.RuneCountInString(segment) >= 2 && r.ValidateBase(segment, raw) == nil {
			return segment, true
		}
	}
	runes := []rune(base)
	for n := min(limit, len(runes)); n >= 2; n-- {
		candidate := string(runes[:n])
		if r.ValidateBase(candidate, raw) == nil {
			return candidate, true
		}
	}
	return "", false
}

// nicknameSegments splits a name on separator runes, keeping Han, Latin words
// and digits so a shop:person name can become just the person.
func nicknameSegments(raw string) []string {
	var segments []string
	var current strings.Builder
	flush := func() {
		text := strings.TrimSpace(current.String())
		current.Reset()
		if text != "" {
			segments = append(segments, text)
		}
	}
	for _, ch := range raw {
		switch {
		case unicode.Is(unicode.Han, ch), unicode.Is(unicode.Latin, ch), unicode.IsDigit(ch):
			current.WriteRune(ch)
		default:
			flush()
		}
	}
	flush()
	return segments
}

func (r Rules) baseLimit() int {
	limit := r.Rewrite.LongNameMax + 2
	if limit < 6 {
		limit = 6
	}
	return limit
}

func nicknameVisible(name string) string {
	return stripSymbols(strings.TrimSpace(name))
}

func orderedSubset(base, raw string) bool {
	if base == "" || raw == "" {
		return false
	}
	rawRunes := []rune(raw)
	index := 0
	for _, want := range []rune(base) {
		if unicode.IsSpace(want) {
			continue
		}
		found := false
		for index < len(rawRunes) {
			if rawRunes[index] == want {
				found = true
				index++
				break
			}
			index++
		}
		if !found {
			return false
		}
	}
	return true
}

func hasDigit(text string) bool {
	for _, ch := range text {
		if unicode.IsDigit(ch) {
			return true
		}
	}
	return false
}

func stripJSONFence(raw string) string {
	text := strings.TrimSpace(raw)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```")
		if idx := strings.IndexByte(text, '\n'); idx >= 0 {
			text = text[idx+1:]
		}
		text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	}
	return strings.TrimSpace(text)
}
