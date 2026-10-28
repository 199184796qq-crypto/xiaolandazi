// Package addressing owns viewer identity, nickname compliance and the single
// frozen address plan for one interaction. The program decides who is named and
// which forms are allowed; the model only phrases an address inside those bounds.
package addressing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Rules is global, operator-maintained configuration. One file governs every
// room so a rule change never needs a code release.
type Rules struct {
	Version    int       `json:"version"`
	Dice       DiceRules `json:"dice"`
	HardBlock  HardBlock `json:"hard_block"`
	Rewrite    Rewrite   `json:"rewrite"`
	GroupLabel []string  `json:"group_labels"`
	GreyHints  []string  `json:"grey_hints"`
}

type DiceRules struct {
	Max      int     `json:"max"`
	MinRatio float64 `json:"min_ratio"` // 抽中人数下限占总人数比例
	Pick     string  `json:"pick"`      // recent | random
}

type HardBlock struct {
	ContactTerms        []string `json:"contact_terms"`
	GenericPrefixes     []string `json:"generic_prefixes"`
	InsultTerms         []string `json:"insult_terms"`
	SemanticTerms       []string `json:"semantic_terms"`
	CommandTerms        []string `json:"command_terms"`
	MaxContinuousDigits int      `json:"max_continuous_digits"`
	MaxNameChars        int      `json:"max_name_chars"`
}

type Rewrite struct {
	LongNameMax      int      `json:"long_name_max"`
	EnglishKeepWords int      `json:"english_keep_words"`
	KeepSuffixTitles []string `json:"keep_suffix_titles"`
}

// DefaultRules returns a usable fallback so a missing file never blocks a live
// room; the shipped JSON stays the source of truth.
func DefaultRules() Rules {
	return Rules{
		Version: 1,
		Dice:    DiceRules{Max: 5, MinRatio: 0.3, Pick: "recent"},
		HardBlock: HardBlock{
			ContactTerms:        []string{"微信", "加v", "vx", "手机", "电话", "qq", "联系我", "专卖店", "出租"},
			GenericPrefixes:     []string{"用户", "抖音用户", "游客", "访客"},
			InsultTerms:         []string{"滚", "骗子", "假货", "垃圾"},
			SemanticTerms:       []string{"爸爸", "妈妈", "老公", "老婆", "主人"},
			CommandTerms:        []string{"叫我", "喊我", "命令", "必须"},
			MaxContinuousDigits: 4,
			MaxNameChars:        10,
		},
		Rewrite: Rewrite{
			LongNameMax:      4,
			EnglishKeepWords: 2,
			KeepSuffixTitles: []string{"老板", "老师", "师傅", "哥", "姐", "弟", "妹"},
		},
		GroupLabel: []string{"几位朋友", "几位老乡"},
	}
}

// Load reads the rules file at path.
func Load(path string) (*Rules, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rules Rules
	if err := json.Unmarshal(raw, &rules); err != nil {
		return nil, fmt.Errorf("昵称规则配置格式错误: %w", err)
	}
	if err := rules.Validate(); err != nil {
		return nil, err
	}
	return &rules, nil
}

// LoadDefault resolves the operator file, then falls back to built-in rules.
func LoadDefault() *Rules {
	if path := strings.TrimSpace(os.Getenv("NICKNAME_RULES_CONFIG")); path != "" {
		if rules, err := Load(path); err == nil {
			return rules
		}
	}
	if dir := strings.TrimSpace(os.Getenv("ANCHOR_STYLE_PROMPT_DIR")); dir != "" {
		if rules, err := Load(filepath.Join(dir, "nickname-rules.json")); err == nil {
			return rules
		}
	}
	defaults := DefaultRules()
	return &defaults
}

func (r Rules) Validate() error {
	if r.Dice.Max <= 0 || r.Dice.Max > 32 {
		return fmt.Errorf("昵称掷骰子上限无效")
	}
	if r.HardBlock.MaxContinuousDigits <= 0 {
		return fmt.Errorf("昵称连续数字上限无效")
	}
	if r.HardBlock.MaxNameChars <= 0 {
		return fmt.Errorf("昵称长度上限无效")
	}
	if r.Rewrite.LongNameMax <= 0 || r.Rewrite.EnglishKeepWords <= 0 {
		return fmt.Errorf("昵称改写规则无效")
	}
	return nil
}

// Status is the program's verdict for one viewer's name.
type Status string

const (
	// StatusSafe needs no rewrite: the raw name is speakable as-is.
	StatusSafe Status = "safe"
	// StatusRewritten carries a program-made safe form.
	StatusRewritten Status = "rewritten"
	// StatusGrey needs semantic judgement; it is sent to the model in one batch.
	StatusGrey Status = "grey"
	// StatusDropped must not be named; the viewer joins the group address.
	StatusDropped Status = "dropped"
)

// Decision is the compliance result for one viewer.
type Decision struct {
	Status Status
	Text   string // speakable form for safe/rewritten; candidate for grey
	Short  string // 1-2 character address for later sentence building
	Reason string
}

// Check applies the global hard rules first, then program rewrites, and only
// leaves genuinely ambiguous names for the model.
func (r Rules) Check(raw string) Decision {
	decision := r.check(raw)
	if decision.Status == StatusSafe || decision.Status == StatusRewritten {
		decision.Short = r.ShortForm(decision.Text)
	}
	return decision
}

func (r Rules) check(raw string) Decision {
	name := strings.TrimSpace(raw)
	if name == "" {
		return Decision{Status: StatusDropped, Reason: "empty"}
	}
	if at := strings.IndexAny(name, "*＊"); at >= 0 {
		name = strings.TrimSpace(name[:at])
		if name == "" {
			return Decision{Status: StatusDropped, Reason: "masked_without_prefix"}
		}
	}
	if containsAny(name, r.HardBlock.ContactTerms) {
		return Decision{Status: StatusDropped, Reason: "contact"}
	}
	if containsAny(name, r.HardBlock.InsultTerms) {
		return Decision{Status: StatusDropped, Reason: "insult"}
	}
	// A name that contains a kinship term ("二楠妈妈") is still a person; a name
	// that IS the term ("妈妈") or asks to be called one ("我是你爸爸") goes to
	// semantic judgement instead of being dropped, because it may be legitimate.
	if r.HardBlock.hasSemanticRisk(name) {
		return Decision{Status: StatusGrey, Text: name, Reason: "semantic_relation"}
	}
	if containsAny(name, r.HardBlock.CommandTerms) {
		return Decision{Status: StatusDropped, Reason: "command"}
	}
	// A pure-digit nickname keeps its first four digits as spoken numerals, so it
	// is still a usable, distinguishable address instead of being dropped.
	if allDigits(name) {
		return Decision{Status: StatusRewritten, Text: chineseDigits(firstDigits(name, 4)), Reason: "digits_only"}
	}
	// Auto-generated accounts: "用户4001612171583" becomes 用户+前三位; a trailing
	// real name ("用户6008101088461梅梅") keeps only 梅梅.
	if prefix, digits, rest, ok := r.HardBlock.parseGenericAccount(name); ok {
		switch {
		case digits == "" && rest == "":
			return Decision{Status: StatusDropped, Reason: "generic_account"}
		case digits != "" && rest != "":
			name = rest
		case digits != "":
			return Decision{Status: StatusRewritten, Text: prefix + chineseDigits(firstDigits(digits, 3)), Reason: "generic_account_digits"}
		}
	}
	if longestDigits(name) >= r.HardBlock.MaxContinuousDigits {
		name = stripDigits(name)
		if strings.TrimSpace(name) == "" {
			return Decision{Status: StatusRewritten, Text: chineseDigits(firstDigits(stripDigits(raw), 4)), Reason: "digits_only"}
		}
	}
	name = stripSymbols(name)
	if strings.TrimSpace(name) == "" {
		return Decision{Status: StatusDropped, Reason: "symbols_only"}
	}
	// "用户4001612171583" is an auto-generated account, not a person's name.
	if r.HardBlock.isGenericAccount(name) {
		return Decision{Status: StatusDropped, Reason: "generic_account"}
	}
	if hasHan(name) && hasLatin(name) {
		return Decision{Status: StatusGrey, Text: name, Reason: "mixed_script"}
	}
	if allLatin(name) {
		words := strings.Fields(name)
		trimmed := words
		if len(trimmed) > r.Rewrite.EnglishKeepWords {
			trimmed = trimmed[:r.Rewrite.EnglishKeepWords]
		}
		form := strings.Join(trimmed, " ")
		if form != name {
			return Decision{Status: StatusRewritten, Text: form, Reason: "english_name"}
		}
		return Decision{Status: StatusSafe, Text: form, Reason: "english_name"}
	}
	if utf8.RuneCountInString(name) > r.Rewrite.LongNameMax {
		return Decision{Status: StatusGrey, Text: name, Reason: "long_name"}
	}
	if name == strings.TrimSpace(raw) {
		return Decision{Status: StatusSafe, Text: name, Reason: "safe"}
	}
	return Decision{Status: StatusRewritten, Text: name, Reason: "rewritten"}
}

// ShortForm returns a deterministic 1-2 character address for later sentence
// building. Long or ambiguous names get their short form from the grey model.
func (r Rules) ShortForm(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	runes := []rune(base)
	if len(runes) <= 2 {
		return base
	}
	if allLatin(base) {
		word := strings.Fields(base)[0]
		if len([]rune(word)) <= 3 {
			return word
		}
		return string([]rune(word)[:3])
	}
	// Account names keep their spoken digits: 用户七二一 -> 七二一.
	for _, prefix := range r.HardBlock.GenericPrefixes {
		if prefix != "" && strings.HasPrefix(base, prefix) {
			rest := strings.TrimSpace(strings.TrimPrefix(base, prefix))
			if rest != "" && allChineseDigits(rest) {
				return rest
			}
		}
	}
	suffix := ""
	for _, title := range r.Rewrite.KeepSuffixTitles {
		if strings.HasSuffix(base, title) && utf8.RuneCountInString(title) > utf8.RuneCountInString(suffix) {
			suffix = title
		}
	}
	if suffix != "" {
		core := strings.TrimSuffix(base, suffix)
		coreRunes := []rune(core)
		if len(coreRunes) == 0 {
			return base
		}
		if len(coreRunes) > 2 {
			coreRunes = coreRunes[len(coreRunes)-2:]
		}
		return string(coreRunes)
	}
	return string(runes[:2])
}

func containsAny(text string, terms []string) bool {
	for _, term := range terms {
		if term != "" && strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func longestDigits(name string) int {
	longest, run := 0, 0
	for _, ch := range name {
		if unicode.IsDigit(ch) {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	return longest
}

func allDigits(name string) bool {
	for _, ch := range name {
		if !unicode.IsDigit(ch) {
			return false
		}
	}
	return name != ""
}

func stripDigits(name string) string {
	var b strings.Builder
	for _, ch := range name {
		if !unicode.IsDigit(ch) {
			b.WriteRune(ch)
		}
	}
	return strings.TrimSpace(b.String())
}

// stripSymbols keeps Han characters, Latin letters, digits and spaces; kana,
// emoji and decoration never reach speech.
func stripSymbols(name string) string {
	var b strings.Builder
	for _, ch := range name {
		switch {
		case unicode.Is(unicode.Han, ch), unicode.Is(unicode.Latin, ch), unicode.IsDigit(ch), ch == ' ':
			b.WriteRune(ch)
		}
	}
	return strings.TrimSpace(b.String())
}

// hasSemanticRisk matches a name that is exactly a kinship term, or one that
// combines a kinship term with an address request ("我是你爸爸").
func (h HardBlock) hasSemanticRisk(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, term := range h.SemanticTerms {
		if term == "" {
			continue
		}
		if name == term {
			return true
		}
		if strings.Contains(name, term) && containsAny(name, h.CommandTerms) {
			return true
		}
	}
	return false
}

// isGenericAccount matches auto-generated account names such as "用户".
func (h HardBlock) isGenericAccount(name string) bool {
	name = strings.TrimSpace(name)
	for _, prefix := range h.GenericPrefixes {
		if prefix != "" && name == prefix {
			return true
		}
	}
	return false
}

// parseGenericAccount splits an auto-generated account name into its prefix,
// leading digit run and any trailing text ("用户4001612171583" →
// 用户/4001612171583/"" ; "用户6008101088461梅梅" → 用户/6008101088461/梅梅).
func (h HardBlock) parseGenericAccount(name string) (string, string, string, bool) {
	name = strings.TrimSpace(name)
	for _, prefix := range h.GenericPrefixes {
		if prefix == "" || !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(name, prefix))
		if rest == "" {
			return prefix, "", "", true
		}
		runes := []rune(rest)
		cut := 0
		for cut < len(runes) && unicode.IsDigit(runes[cut]) {
			cut++
		}
		if cut == 0 {
			// 用户小王 is an ordinary name, not an account.
			return "", "", "", false
		}
		return prefix, string(runes[:cut]), string(runes[cut:]), true
	}
	return "", "", "", false
}

// AccountAddress returns the account prefix plus its first n digits as spoken
// numerals, used to break collisions inside one batch.
func (h HardBlock) AccountAddress(raw string, n int) (string, bool) {
	prefix, digits, rest, ok := h.parseGenericAccount(raw)
	if !ok || digits == "" || rest != "" || n <= 0 {
		return "", false
	}
	return prefix + chineseDigits(firstDigits(digits, n)), true
}

// DigitsAddress spells the first n digits of a pure-digit nickname.
func DigitsAddress(raw string, n int) (string, bool) {
	if !allDigits(raw) || n <= 0 {
		return "", false
	}
	return chineseDigits(firstDigits(raw, n)), true
}

func firstDigits(digits string, n int) string {
	runes := []rune(digits)
	if n > len(runes) {
		n = len(runes)
	}
	return string(runes[:n])
}

// chineseDigits spells an account suffix digit by digit so TTS never reads it
// as a quantity, phone number or order number.
func chineseDigits(digits string) string {
	var b strings.Builder
	for _, ch := range digits {
		if mapped, ok := chineseDigitRunes[ch]; ok {
			b.WriteRune(mapped)
			continue
		}
		b.WriteRune(ch)
	}
	return b.String()
}

var chineseDigitRunes = map[rune]rune{
	'0': '零', '1': '一', '2': '二', '3': '三', '4': '四',
	'5': '五', '6': '六', '7': '七', '8': '八', '9': '九',
}

func hasHan(name string) bool {
	for _, ch := range name {
		if unicode.Is(unicode.Han, ch) {
			return true
		}
	}
	return false
}

func hasLatin(name string) bool {
	for _, ch := range name {
		if unicode.Is(unicode.Latin, ch) {
			return true
		}
	}
	return false
}

func allLatin(name string) bool {
	hasLetter := false
	for _, ch := range name {
		switch {
		case unicode.Is(unicode.Latin, ch):
			hasLetter = true
		case ch == ' ':
		default:
			return false
		}
	}
	return hasLetter
}
