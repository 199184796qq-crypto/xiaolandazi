package anchorcycle

import (
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
