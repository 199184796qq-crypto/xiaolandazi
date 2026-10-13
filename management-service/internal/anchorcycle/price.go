package anchorcycle

import (
	"math/rand"
	"regexp"
	"strings"
)

type PriceHook struct {
	Task         Task   `json:"task"`
	Promise      string `json:"promise"`
	SourceText   string `json:"source_text"`
	CreatedIndex int    `json:"created_index"`
	DueIndex     int    `json:"due_index"`
}
type PriceCheck struct {
	Hook      string `json:"hook,omitempty"`
	Completed bool   `json:"completed"`
}

var pricePromise = regexp.MustCompile(`(?:下一(?:段|轮)|接下来|待会儿?|等会儿?|一会儿?|后面|稍后)[^。！？!?\n]{0,60}(?:算账|算一笔|算一下|算清|算给|多少钱|省多少|价格|报价|塑价)|(?:算账|算一笔|省多少|价格)[^。！？!?\n]{0,30}(?:下一(?:段|轮)|待会儿?|等会儿?|稍后)`)
var monetaryNumber = regexp.MustCompile(`[0-9一二三四五六七八九十百千万两]+(?:\.[0-9]+)?(?:块(?:[0-9一二三四五六七八九])?|元)`)
var arithmeticAction = regexp.MustCompile(`省|差价|合计|一共|总共|算下来|划下来|每斤|每桶|平均|两桶|四桶|2桶|4桶`)

// This checks explicit promises and visible arithmetic structure only. It does
// not certify factual truth, value, or subjective persuasion quality.
func InspectPrice(text string, task Task) PriceCheck {
	check := PriceCheck{Hook: strings.TrimSpace(pricePromise.FindString(text))}
	check.Completed = len(monetaryNumber.FindAllString(text, -1)) >= 2 && arithmeticAction.MatchString(text)
	return check
}

func (s *State) PriceHookAllowed() bool {
	if s.ProductFocus != nil && s.ProductFocus.AuxiliaryActive {
		return false
	}
	if s.PriceHook != nil || (s.Phase == "closing" && (!s.Config.Rolling || (s.Config.MaxCycles > 0 && s.CycleNumber >= s.Config.MaxCycles))) {
		return false
	}
	// At most one price promise in any six adopted paragraphs. Failed drafts
	// do not spend this budget; this is a product scheduling rule, not a fact.
	for _, segment := range s.Accepted[max(0, len(s.Accepted)-5):] {
		if segment.Selected.PriceCheck.Hook != "" {
			return false
		}
	}
	return true
}

func (s *State) priceObligationProblem(check PriceCheck) string {
	if s.PriceHook != nil && (!check.Completed || check.Hook != "") {
		return "price_hook_unresolved"
	}
	if check.Hook != "" && !s.PriceHookAllowed() {
		return "price_hook_overuse"
	}
	return ""
}

// PriceDrift 正文价格与事实包快照串换的一处错误。
type PriceDrift struct {
	Spoken  string `json:"spoken"`  // 正文说错的原文片段（含单位，如 "92.6元"）
	Correct string `json:"correct"` // 正确价格（纯数字，如 "87.6"）
	Start   int    `json:"start"`   // 错误片段在正文的 rune 起始偏移
	End     int    `json:"end"`     // rune 结束偏移
}

// 链接号 + 同句内价格声明（不超过 40 字），用于检测「X号...价格」串换。
var linkPriceRE = regexp.MustCompile(`([0-9]+)\s*号[^。！？!?\n]{0,40}?([0-9]+(?:\.[0-9]+)?)\s*(?:元|块钱?)`)
var priceNumRE = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

// normPrice 规范化价格：去掉 ¥/￥ 符号与「元/块」单位，返回纯数字。
func normPrice(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "¥")
	s = strings.TrimPrefix(s, "￥")
	s = strings.TrimSuffix(s, "元")
	s = strings.TrimSuffix(s, "块")
	return strings.TrimSpace(s)
}

// normLinkKey 规范化链接号：提取首位数字。
func normLinkKey(s string) string {
	return priceNumRE.FindString(s)
}

// priceSnapshot 返回 链接号→规范化日常价。
func priceSnapshot(products []FocusProduct) map[string]string {
	m := map[string]string{}
	for _, p := range products {
		k, v := normLinkKey(p.LinkKey), normPrice(p.DailyPrice)
		if k != "" && v != "" {
			m[k] = v
		}
	}
	return m
}

// CheckPriceDrift 检测正文价格串换：正文里「X号…价格Y」，若 Y 恰为另一链接 Z
// 的日常价，则判定串错，应纠正为 X 号的日常价。要求至少两个链接才有可比对。
func CheckPriceDrift(text string, products []FocusProduct) []PriceDrift {
	snap := priceSnapshot(products)
	if len(snap) < 2 {
		return nil
	}
	priceToLink := map[string]string{}
	for k, v := range snap {
		if _, exists := priceToLink[v]; !exists {
			priceToLink[v] = k
		}
	}
	var out []PriceDrift
	for _, m := range linkPriceRE.FindAllStringSubmatchIndex(text, -1) {
		linkNo := text[m[2]:m[3]]
		spokenNum := text[m[4]:m[5]]
		correct, ok := snap[linkNo]
		if !ok {
			continue
		}
		owner, known := priceToLink[spokenNum]
		if !known || owner == linkNo {
			continue
		}
		out = append(out, PriceDrift{
			Spoken:  text[m[4]:m[1]],
			Correct: correct,
			Start:   runeLen(text[:m[4]]),
			End:     runeLen(text[:m[1]]),
		})
	}
	return out
}

func runeLen(s string) int { return len([]rune(s)) }

// ApplyPriceCorrection 随机二选一纠正价格串错：
// 一半概率硬改数字；一半概率保留口误、在后面补一句真人式纠正。
func ApplyPriceCorrection(text string, d PriceDrift, rng *rand.Rand) string {
	if rng.Intn(2) == 0 {
		return hardReplacePrice(text, d)
	}
	return appendPriceCorrection(text, d)
}

// hardReplacePrice 只替换错误价格里的数字，单位原样保留。
func hardReplacePrice(text string, d PriceDrift) string {
	runes := []rune(text)
	if d.Start < 0 || d.End > len(runes) || d.Start >= d.End {
		return text
	}
	spoken := string(runes[d.Start:d.End])
	fixed := priceNumRE.ReplaceAllString(spoken, d.Correct)
	return string(runes[:d.Start]) + fixed + string(runes[d.End:])
}

// appendPriceCorrection 在错误价格后补一句真人式口误纠正。
func appendPriceCorrection(text string, d PriceDrift) string {
	runes := []rune(text)
	if d.End < 0 || d.End > len(runes) {
		return text
	}
	correction := "（哎说岔了，是" + d.Correct + "）"
	return string(runes[:d.End]) + correction + string(runes[d.End:])
}
