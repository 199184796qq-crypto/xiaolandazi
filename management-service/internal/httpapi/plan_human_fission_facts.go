package httpapi

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"livecompanion/management/internal/humanfission"
)

type fissionReviewPart struct {
	Number int    `json:"number"`
	SRT    string `json:"srt"`
}

type fissionReviewInput struct {
	Order     []int                 `json:"order"`
	Parts     []fissionReviewPart   `json:"parts"`
	Facts     json.RawMessage       `json:"facts"`
	Conflicts []fissionFactConflict `json:"scanned_conflicts,omitempty"`
}

type fissionFactConflict struct {
	Part      int    `json:"part"`
	Before    string `json:"before"`
	Object    string `json:"object"`
	Attribute string `json:"attribute"`
	Current   string `json:"current_fact"`
	Spoken    string `json:"spoken_value"`
	kind      fissionNumericKind
	expected  int
}

type fissionNumericKind string

const (
	fissionPrice    fissionNumericKind = "price"
	fissionDeadline fissionNumericKind = "deadline"
	fissionTerm     fissionNumericKind = "term"
)

const fissionNumerals = `[0-9零〇一二两三四五六七八九十百千万亿]+`
const fissionDigit = `[0-9零〇一二两三四五六七八九]`

var (
	fissionMoneyRE    = regexp.MustCompile(`(` + fissionNumerals + `)(?:[.点](` + fissionDigit + `{1,2}))?(元|块钱?)`)
	fissionCoinRE     = regexp.MustCompile(`^([0-9一二三四五六七八九])([角毛]?)`)
	fissionPackPrefix = regexp.MustCompile(`^(?:公斤|千克|斤|两|克|个|份|袋|包|盒|瓶|杯|g|kg)`)
	fissionDayOfMonth = regexp.MustCompile(`(` + fissionNumerals + `)[日号]`)
	fissionDateRE     = regexp.MustCompile(`(` + fissionNumerals + `)月(` + fissionNumerals + `)[日号]`)
	fissionDaysRE     = regexp.MustCompile(`(` + fissionNumerals + `)天`)
	// A bare “29号” may be a date. Require the explicit link/product suffix.
	fissionLinkRE     = regexp.MustCompile(`(?:[0-9]{1,2}|[一二三四五六七八九十]{1,3})号(?:链接|商品)`)
	fissionPriceBound = regexp.MustCompile(`[0-9一二三四五六七八九十][~～—–到至-]$`)
	fissionVaguePrice = regexp.MustCompile(`^(?:多|几|左右|上下|差不多|起价|起售|以上|以下|出头|不到)`)
)

func compactFissionFact(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return -1
		case r >= '０' && r <= '９':
			return r - '０' + '0'
		case r == '．':
			return '.'
		}
		return r
	}, text)
}

func fissionNumber(text string) (int, bool) {
	if value, err := strconv.Atoi(text); err == nil && value >= 0 && value < 1_000_000 {
		return value, true
	}
	switch text {
	case "零", "〇":
		return 0, true
	case "两":
		return 2, true
	}
	if hundreds, remainder, found := strings.Cut(text, "百"); found {
		head, ok := fissionNumber(hundreds)
		if !ok || head < 1 || head > 9 {
			return 0, false
		}
		if remainder == "" {
			return head * 100, true
		}
		if rest, startsWithZero := strings.CutPrefix(remainder, "零"); startsWithZero {
			tail, ok := fissionNumber(rest)
			return head*100 + tail, ok && tail > 0 && tail < 10
		}
		tail, ok := chinesePlanNumber(remainder)
		return head*100 + tail, ok && tail >= 10
	}
	return chinesePlanNumber(text)
}

// Values use cents, month*100+day or whole days. Conflicting values within
// one cue or fact are left to the model rather than assigning one at random.
func fissionNumericValue(kind fissionNumericKind, text string) (int, string, bool) {
	text = compactFissionFact(text)
	pattern := fissionMoneyRE
	switch kind {
	case fissionDeadline:
		pattern = fissionDateRE
	case fissionTerm:
		pattern = fissionDaysRE
	}
	var value int
	var spoken string
	for _, match := range pattern.FindAllStringSubmatchIndex(text, -1) {
		token := func(group int) string {
			at := match[group*2]
			if at < 0 {
				return ""
			}
			return text[at:match[group*2+1]]
		}
		n, ok := fissionNumber(token(1))
		if !ok {
			return 0, "", false
		}
		end := match[1]
		switch kind {
		case fissionPrice:
			if fissionPriceBound.MatchString(text[:match[0]]) || strings.HasSuffix(text[:match[0]], ".") {
				return 0, "", false
			}
			cents := n * 100
			if fraction := token(2); fraction != "" {
				digits := []rune(fraction)
				for i, digit := range digits {
					part, ok := fissionNumber(string(digit))
					if !ok {
						return 0, "", false
					}
					if i == 0 {
						part *= 10
					}
					cents += part
				}
			} else if coin := fissionCoinRE.FindStringSubmatch(text[end:]); coin != nil {
				rest := text[end+len(coin[0]):]
				implicit := strings.HasPrefix(token(3), "块") && !fissionPackPrefix.MatchString(rest)
				if coin[2] != "" || implicit {
					tenths, _ := fissionNumber(coin[1])
					cents += tenths * 10
					end += len(coin[0])
				}
			}
			if fissionVaguePrice.MatchString(text[end:]) {
				return 0, "", false
			}
			n = cents
		case fissionDeadline:
			day, ok := fissionNumber(token(2))
			monthDays := [...]int{0, 31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
			if !ok || n < 1 || n > 12 || day < 1 || day > monthDays[n] {
				return 0, "", false
			}
			n = n*100 + day
		case fissionTerm:
			if fissionPriceBound.MatchString(text[:match[0]]) {
				return 0, "", false
			}
		}
		if spoken != "" && value != n {
			return 0, "", false
		}
		value, spoken = n, text[match[0]:end]
	}
	return value, spoken, spoken != ""
}

func fissionNumericFactKind(attribute string) fissionNumericKind {
	if strings.Contains(attribute, "券") &&
		(strings.Contains(attribute, "有效期") || strings.Contains(attribute, "截止") || strings.Contains(attribute, "到期")) {
		return fissionDeadline // A coupon fact may instead contain a duration.
	}
	if !strings.Contains(attribute, "价格") && !strings.Contains(attribute, "售价") &&
		!strings.Contains(attribute, "团购价") && !strings.Contains(attribute, "到手价") {
		return ""
	}
	for _, marker := range []string{"门店", "线下", "网上", "网购", "竞品", "市场", "原价", "划线", "折算", "单价", "对比", "定位", "差价", "优惠券", "补贴", "起步"} {
		if strings.Contains(attribute, marker) {
			return ""
		}
	}
	return fissionPrice
}

type fissionFactKey struct {
	object string
	kind   fissionNumericKind
}

type fissionFactTarget struct {
	attribute string
	text      string
	value     int
	ambiguous bool
}

// Aliases resolve only through product facts. An ambiguous alias or a subtitle
// naming multiple products forms a boundary for borrowing nearby context.
func fissionCueSubject(text string, aliases map[string]string, shortAliases map[string]string) (subject string, boundary bool) {
	add := func(key string) {
		if boundary {
			return
		}
		if key == "" || subject != "" && subject != key {
			subject, boundary = "", true
			return
		}
		subject = key
	}
	text = compactFissionFact(text)
	for _, match := range fissionLinkRE.FindAllString(text, -1) {
		add(canonicalPlanProductLinkKey(match))
	}
	for name, key := range aliases {
		if strings.Contains(text, name) {
			add(key)
		}
	}
	for name, key := range shortAliases {
		if strings.Contains(text, "这个"+name) || strings.Contains(text, "这种"+name) {
			add(key)
		}
	}
	return
}

type fissionSubject struct {
	key      string
	boundary bool
	local    bool
}

func fissionNearbySubject(order []int, subjects map[int][]fissionSubject, at, cue, direction int) fissionSubject {
	part := subjects[order[at]]
	next := cue + direction
	for next >= 0 && next < len(part) {
		if found := part[next]; found.boundary || found.key != "" {
			found.local = true
			return found
		}
		next += direction
	}
	neighbor := at + direction
	if neighbor < 0 || neighbor >= len(order) {
		return fissionSubject{}
	}
	part = subjects[order[neighbor]]
	next = 0
	if direction < 0 {
		next = len(part) - 1
	}
	for next >= 0 && next < len(part) {
		if found := part[next]; found.boundary || found.key != "" {
			return found
		}
		next += direction
	}
	return fissionSubject{}
}

func fissionNumericConflicts(order []int, cues map[int][]humanfission.Cue, facts json.RawMessage) ([]fissionFactConflict, error) {
	var entries []rewriteFact
	if err := json.Unmarshal(facts, &entries); err != nil {
		return nil, fmt.Errorf("读取真人话术事实清单: %w", err)
	}
	targets := map[fissionFactKey]fissionFactTarget{}
	aliases, shortAliases := map[string]string{}, map[string]string{}
	addAlias := func(index map[string]string, name, object string) {
		if previous, exists := index[name]; exists && previous != object {
			index[name] = ""
		} else {
			index[name] = object
		}
	}
	for _, fact := range entries {
		object := compactFissionFact(fact.Object)
		if !fissionLinkRE.MatchString(object) || fissionLinkRE.FindString(object) != object {
			continue
		}
		object = canonicalPlanProductLinkKey(object)
		var value string
		stringValue := json.Unmarshal(fact.Value, &value) == nil
		if stringValue && (strings.Contains(fact.Attribute, "品名") || strings.Contains(fact.Attribute, "商品名称") || strings.Contains(fact.Attribute, "产品名称")) {
			name := []rune(compactFissionFact(value))
			if len(name) >= 2 && len(name) <= 24 {
				addAlias(aliases, string(name), object)
				if len(name) >= 4 {
					addAlias(shortAliases, string(name[len(name)-3:]), object)
				}
			}
		}
		kind := fissionNumericFactKind(fact.Attribute)
		if kind == "" {
			continue
		}
		number, _, ok := fissionNumericValue(kind, value)
		if kind == fissionDeadline && !ok {
			kind = fissionTerm
			number, _, ok = fissionNumericValue(kind, value)
		}
		key := fissionFactKey{object: object, kind: kind}
		existing, exists := targets[key]
		ambiguous := !ok || !stringValue || fact.Condition != "" || fact.Exception != "" ||
			fact.Polarity == "negative" || exists && (existing.ambiguous || existing.value != number)
		targets[key] = fissionFactTarget{attribute: fact.Attribute, text: value, value: number, ambiguous: ambiguous}
	}

	subjects := make(map[int][]fissionSubject, len(cues))
	for number, lines := range cues {
		for _, line := range lines {
			key, boundary := fissionCueSubject(line.Text, aliases, shortAliases)
			subjects[number] = append(subjects[number], fissionSubject{key: key, boundary: boundary})
		}
	}
	occurrences := map[int]int{}
	hits := map[fissionFactConflict]int{}
	var conflicts []fissionFactConflict
	for at, part := range order {
		occurrences[part]++
		seen := map[fissionFactConflict]bool{}
		for index, cue := range cues[part] {
			subject := subjects[part][index]
			if subject.boundary {
				continue
			}
			if subject.key == "" {
				before := fissionNearbySubject(order, subjects, at, index, -1)
				after := fissionNearbySubject(order, subjects, at, index, 1)
				if before.local != after.local {
					if before.local {
						after = fissionSubject{}
					} else {
						before = fissionSubject{}
					}
				}
				if before.boundary || after.boundary || before.key != "" && after.key != "" && before.key != after.key {
					continue
				}
				subject.key = before.key
				if subject.key == "" {
					subject.key = after.key
				}
			}
			if subject.key == "" {
				continue
			}
			text := compactFissionFact(cue.Text)
			for _, kind := range []fissionNumericKind{fissionPrice, fissionDeadline, fissionTerm} {
				target, exists := targets[fissionFactKey{object: subject.key, kind: kind}]
				if !exists || target.ambiguous || !fissionCueStatesFact(kind, text) {
					continue
				}
				number, spoken, ok := fissionNumericValue(kind, text)
				if !ok || number == target.value {
					continue
				}
				conflict := fissionFactConflict{Part: part, Before: cue.Text, Object: subject.key, Attribute: target.attribute,
					Current: target.text, Spoken: spoken, kind: kind, expected: target.value}
				if !seen[conflict] {
					seen[conflict] = true
					if hits[conflict] == 0 {
						conflicts = append(conflicts, conflict)
					}
					hits[conflict]++
				}
			}
		}
	}
	// The same source audio may be replayed with different neighboring clips.
	// Only require a rewrite if the mismatch is confirmed in each occurrence.
	confirmed := conflicts[:0]
	for _, conflict := range conflicts {
		if hits[conflict] == occurrences[conflict.Part] {
			confirmed = append(confirmed, conflict)
		}
	}
	return confirmed, nil
}

func fissionCueStatesFact(kind fissionNumericKind, text string) bool {
	contains := func(markers ...string) bool {
		for _, marker := range markers {
			if strings.Contains(text, marker) {
				return true
			}
		}
		return false
	}
	if kind == fissionPrice {
		return contains("价格", "售价", "团购价", "到手", "元", "块") &&
			!contains("门店", "线下", "外面买", "网上", "网购", "市面", "市价", "原价", "划线", "平时", "折合", "折算", "一斤", "每斤", "一公斤", "每公斤", "运费", "邮费", "满减", "加购", "如果", "立减", "可省", "押金", "退款", "抵扣", "返现", "券面额")
	}
	return contains("券") && contains("有效", "截止", "到期", "用到", "能用", "使用", "核销", "过期") &&
		!contains("提前", "至少", "最多", "保质期", "领取时间", "开始销售")
}

func (c fissionFactConflict) resolved(after string) bool {
	number, _, ok := fissionNumericValue(c.kind, after)
	if !ok || number != c.expected {
		return false
	}
	if c.kind == fissionDeadline {
		after = compactFissionFact(after)
		day := c.expected % 100
		days := [...]int{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
		if day < days[c.expected/100] && (strings.Contains(after, "月底") || strings.Contains(after, "月末")) {
			return false
		}
		for _, match := range fissionDayOfMonth.FindAllStringSubmatchIndex(after, -1) {
			rest := after[match[1]:]
			if strings.HasPrefix(rest, "链接") || strings.HasPrefix(rest, "商品") {
				continue
			}
			if n, ok := fissionNumber(after[match[2]:match[3]]); !ok || n != day {
				return false
			}
		}
	}
	return true
}
