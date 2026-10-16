package decisionexecutor

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentstrategy"
)

type interactionFactScope struct {
	FullFacts string
	Links     []string
}

var interactionSpokenLink = regexp.MustCompile(`([0-9０-９]+|[一二三四五六七八九十百零〇两]+)\s*号(?:\s*链接)?`)
var interactionCombinedLinks = regexp.MustCompile(`((?:[0-9０-９一二三四五六七八九十百零〇两]+\s*[、,，/和与及]\s*)+[0-9０-９一二三四五六七八九十百零〇两]+)\s*号`)
var interactionLinkListSeparator = regexp.MustCompile(`\s*[、,，/和与及]\s*`)
var interactionNameParts = regexp.MustCompile(`[【】\[\]（）(),，;；:：]`)

// Link numbers are merchant-facing business labels, never database IDs.
func interactionLinkNumber(text string) string {
	if utf8.RuneCountInString(text) > 9 {
		return ""
	}
	text = strings.Map(func(r rune) rune {
		if r >= '０' && r <= '９' {
			return r - '０' + '0'
		}
		return r
	}, strings.TrimSpace(text))
	if n, err := strconv.Atoi(text); err == nil && n > 0 {
		return strconv.Itoa(n)
	}
	digits := map[rune]int{'一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9, '零': 0, '〇': 0}
	if !strings.ContainsAny(text, "十百") {
		n := 0
		for _, r := range text {
			v, ok := digits[r]
			if !ok {
				return ""
			}
			n = n*10 + v
		}
		if n > 0 {
			return strconv.Itoa(n)
		}
		return ""
	}
	n, digit := 0, 0
	for _, r := range text {
		if r == '十' || r == '百' {
			unit := 10
			if r == '百' {
				unit = 100
			}
			if digit == 0 {
				digit = 1
			}
			n += digit * unit
			digit = 0
		} else if v, ok := digits[r]; ok {
			digit = v
		} else {
			return ""
		}
	}
	if n+digit > 0 {
		return strconv.Itoa(n + digit)
	}
	return ""
}

func interactionProductNumber(key string) string {
	if matches := interactionSpokenLink.FindStringSubmatch(key); len(matches) > 0 {
		return interactionLinkNumber(matches[1])
	}
	return interactionLinkNumber(key)
}

// Resolve only explicit business labels or a unique product title. Pronouns,
// unknown links and ambiguous names retain the full package, rather than
// guessing from the most recent mainline or from a model's generated plan.
func interactionQuestionLinks(question string, products []anchorcycle.FocusProduct) []string {
	for _, unresolved := range []string{"另一个", "另一款", "其他商品", "其它商品", "其他链接", "其它链接"} {
		if strings.Contains(question, unresolved) {
			return nil
		}
	}
	byNumber := map[string]string{}
	for _, p := range products {
		if n := interactionProductNumber(p.LinkKey); n != "" {
			if _, exists := byNumber[n]; exists {
				return nil
			}
			byNumber[n] = p.LinkKey
		}
	}
	selected := map[string]bool{}
	explicit := interactionSpokenLink.FindAllStringSubmatch(question, -1)
	for _, match := range explicit {
		key, ok := byNumber[interactionLinkNumber(match[1])]
		if !ok {
			return nil
		}
		selected[key] = true
	}
	for _, group := range interactionCombinedLinks.FindAllStringSubmatch(question, -1) {
		for _, number := range interactionLinkListSeparator.Split(group[1], -1) {
			key, ok := byNumber[interactionLinkNumber(number)]
			if !ok {
				return nil
			}
			selected[key] = true
		}
	}
	aliases := map[string]map[string]bool{}
	for _, p := range products {
		// Only title fragments preceding specifications are aliases. Ingredients,
		// prices and examples in a description cannot become product identifiers.
		title := strings.TrimSpace(p.Name)
		if at := strings.IndexAny(title, "（("); at >= 0 {
			title = title[:at]
		}
		for _, name := range append([]string{title}, interactionNameParts.Split(title, -1)...) {
			name = strings.TrimSpace(name)
			if utf8.RuneCountInString(name) < 3 {
				continue
			}
			if aliases[name] == nil {
				aliases[name] = map[string]bool{}
			}
			aliases[name][p.LinkKey] = true
		}
	}
	for alias, keys := range aliases {
		if !interactionTitleMention(question, alias) {
			continue
		}
		if len(keys) != 1 {
			if len(explicit) == 0 {
				return nil
			}
			continue
		}
		for key := range keys {
			selected[key] = true
		}
	}
	var result []string
	for _, p := range products {
		if selected[p.LinkKey] {
			result = append(result, p.LinkKey)
		}
	}
	return result
}

func interactionTitleMention(question, alias string) bool {
	// A title beginning with 6 must not match the tail of 16 or 106.
	first, _ := utf8.DecodeRuneInString(alias)
	for start := 0; start < len(question); {
		at := strings.Index(question[start:], alias)
		if at < 0 {
			return false
		}
		at += start
		if first < '0' || first > '9' || at == 0 {
			return true
		}
		previous, _ := utf8.DecodeLastRuneInString(question[:at])
		if !(previous >= '0' && previous <= '9' || previous >= '０' && previous <= '９') {
			return true
		}
		start = at + len(alias)
	}
	return false
}

// Narrow only from complete, source-matching saved scope metadata. Missing or
// legacy scope metadata is a full-package fallback, never an empty replacement.
func narrowInteractionFacts(generation map[string]any, full, question string) (string, interactionFactScope) {
	fallback := interactionFactScope{FullFacts: full}
	var scheme struct {
		Focus *anchorcycle.ProductFocus `json:"product_focus"`
	}
	raw, err := json.Marshal(generation["anchor_generation"])
	if err != nil || json.Unmarshal(raw, &scheme) != nil || scheme.Focus == nil {
		return full, fallback
	}
	focus := scheme.Focus
	if focus.MaterialVersion != "product-common-v1" || focus.FactsSHA != contentstrategy.SHA(full) {
		return full, fallback
	}
	known := map[string]bool{}
	for _, p := range focus.Products {
		if p.LinkKey == "" || known[p.LinkKey] {
			return full, fallback
		}
		known[p.LinkKey] = true
	}
	links := interactionQuestionLinks(question, focus.Products)
	if len(links) == 0 {
		return full, fallback
	}
	selected := map[string]bool{}
	for _, link := range links {
		selected[link] = true
	}
	remaining := map[string]int{}
	for _, f := range contentstrategy.Facts(full) {
		remaining[f.Text]++
	}
	var blocks []string
	ownFacts := map[string]bool{}
	for _, fact := range focus.Facts {
		if fact.Text == "" || remaining[fact.Text] == 0 || (!fact.Shared && len(fact.Links) == 0) {
			return full, fallback
		}
		remaining[fact.Text]--
		allowed := fact.Shared
		allSelected := true
		for _, link := range fact.Links {
			if !known[link] {
				return full, fallback
			}
			if !selected[link] {
				allSelected = false
			}
			if selected[link] && (fact.Applicable || len(fact.Links) == 1) {
				allowed = true
				ownFacts[link] = true
			}
		}
		// A relational/comparison fact needs every referenced product; an
		// applicable fact is separately valid for any of its assigned products.
		if !fact.Shared && !fact.Applicable && len(fact.Links) > 1 {
			allowed = allSelected
		}
		if allowed {
			blocks = append(blocks, fact.Text)
		}
	}
	for _, count := range remaining {
		if count != 0 {
			return full, fallback
		}
	}
	for _, link := range links {
		if !ownFacts[link] {
			return full, fallback
		}
	}
	if len(blocks) == 0 {
		return full, fallback
	}
	if len(links) == len(focus.Products) {
		return full, interactionFactScope{FullFacts: full, Links: links}
	}
	return "# 本次商品完整事实与公共事实\n适用商品：" + strings.Join(links, "、") + "\n" + strings.Join(blocks, "\n"), interactionFactScope{FullFacts: full, Links: links}
}
