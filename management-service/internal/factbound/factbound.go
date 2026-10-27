// Package factbound decides how much of a viewer question the selected facts
// actually cover. It is deliberately value-based: no question lists, no category
// rules. Values come from a maintainable JSON lexicon (places, units, time and
// claim words); everything else is left to the model.
package factbound

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"sort"
	"strings"

	"livecompanion/management/internal/interactionpolicy"
)

// State is the question-level hit result.
type State string

const (
	Hit     State = "hit"     // the facts answer every explicit condition
	Partial State = "partial" // some conditions are answered, some are not
	Miss    State = "miss"    // nothing in the facts answers this question
)

type Place struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	Remote  bool     `json:"remote"`
}

type Config struct {
	Version    int      `json:"version"`
	Places     []Place  `json:"places"`
	Nationwide []string `json:"nationwide"`
	Exclusions []string `json:"exclusions"`
	Units      []string `json:"units"`
	TimeWords  []string `json:"time_words"`
	ClaimWords []string `json:"claim_words"`
	Hedges     []string `json:"hedges"`
	// BoundaryTemplates holds per-class spoken fallbacks so wording lives in
	// configuration; several variants let repeated questions sound different.
	BoundaryTemplates map[string][]string `json:"boundary_templates"`
	BoundaryStyles    []string            `json:"boundary_styles"`
	BoundaryPatterns  []string            `json:"boundary_patterns"`
	BoundaryRhetorics []string            `json:"boundary_rhetorics"`
	MetaBans          []string            `json:"meta_bans"`
	IntroRules        IntroRules          `json:"intro_rules"`
}

// IntroRules is the configurable check for "what this room sells" intros. It is
// data, not code: a new category only edits these lists.
type IntroRules struct {
	CategoryWords []string `json:"category_words"`
	ForbidWords   []string `json:"forbid_words"`
	MaterialWords []string `json:"material_words"`
	LeadRunes     int      `json:"lead_runes"`
}

// Value is one explicit condition the viewer named.
type Value struct {
	Kind string `json:"kind"` // place | quantity | time
	Text string `json:"text"`
}

// Result is the program's decision for one question.
type Result struct {
	State     State   `json:"state"`
	Values    []Value `json:"values"`
	Uncovered []Value `json:"uncovered"`
	ConfigSHA string  `json:"config_sha256"`
}

// NeedsBoundary reports whether the answer must hedge part of the question.
func (r Result) NeedsBoundary() bool { return r.State == Partial }

var loadCache struct {
	config Config
	sha    string
	err    error
}

// Load reads the lexicon once. A packaged prompt directory is the default; the
// FACT_BOUND_CONFIG override exists for probes and tests.
func Load() (Config, string, error) {
	if loadCache.sha != "" || loadCache.err != nil {
		return loadCache.config, loadCache.sha, loadCache.err
	}
	raw, err := readConfig()
	if err != nil {
		loadCache.err = err
		return Config{}, "", err
	}
	var config Config
	if err = json.Unmarshal(raw, &config); err != nil || config.Version != 1 || len(config.Places) == 0 {
		loadCache.err = fmt.Errorf("事实边界配置无效")
		return Config{}, "", loadCache.err
	}
	loadCache.config, loadCache.sha = config, fmt.Sprintf("%x", sha256.Sum256(raw))
	return loadCache.config, loadCache.sha, nil
}

func readConfig() ([]byte, error) {
	if path := strings.TrimSpace(os.Getenv("FACT_BOUND_CONFIG")); path != "" {
		return os.ReadFile(path)
	}
	return interactionpolicy.Read("fact-bound.json")
}

// Evaluate compares the question's explicit values with the facts actually sent
// to the model. hasFacts=false means fact selection returned nothing at all.
func Evaluate(question, facts string, hasFacts bool) Result {
	config, sha, err := Load()
	result := Result{ConfigSHA: sha}
	if err != nil {
		// A missing lexicon must not silently confirm anything.
		result.State = Partial
		return result
	}
	if !hasFacts {
		result.State = Miss
		return result
	}
	result.Values = Extract(config, question)
	if len(result.Values) == 0 {
		result.State = Hit
		return result
	}
	for _, value := range result.Values {
		if covered(config, facts, value) {
			continue
		}
		result.Uncovered = append(result.Uncovered, value)
	}
	if len(result.Uncovered) == 0 {
		result.State = Hit
	} else {
		result.State = Partial
	}
	return result
}

func covered(config Config, facts string, value Value) bool {
	switch value.Kind {
	case "place":
		for _, place := range config.Places {
			if place.Name != value.Text {
				continue
			}
			if containsAny(facts, place.Aliases) {
				return true
			}
			if excludedRegion(config, facts, place) {
				return true
			}
			if containsAny(facts, config.Nationwide) {
				return true
			}
			return false
		}
		return containsAny(facts, []string{value.Text})
	default:
		return strings.Contains(facts, value.Text)
	}
}

func excludedRegion(config Config, facts string, place Place) bool {
	for _, alias := range place.Aliases {
		for _, phrase := range config.Exclusions {
			if strings.Contains(facts, phrase+alias) || strings.Contains(facts, alias+phrase) {
				return true
			}
		}
	}
	for _, sentence := range strings.Split(facts, "\n") {
		if !containsAny(sentence, place.Aliases) {
			continue
		}
		if containsAny(sentence, config.Exclusions) {
			return true
		}
	}
	if place.Remote && containsAny(facts, []string{"偏远"}) && containsAny(facts, config.Exclusions) {
		return true
	}
	return false
}

var quantityPattern = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?|[一二两三四五六七八九十百]+)\s*(人|份|斤|公斤|千克|克|箱|袋|件|盒|瓶|套|个|天|小时|分钟|岁)`)

// Extract pulls the explicit values a viewer named. Everything else (intent,
// topic, whether the question makes sense) stays with the model.
func Extract(config Config, question string) []Value {
	var values []Value
	seen := map[string]bool{}
	for _, place := range config.Places {
		if !containsAny(question, place.Aliases) || seen[place.Name] {
			continue
		}
		seen[place.Name] = true
		values = append(values, Value{Kind: "place", Text: place.Name})
	}
	for _, match := range quantityPattern.FindAllStringSubmatch(question, -1) {
		text := match[1] + match[2]
		if seen[text] {
			continue
		}
		seen[text] = true
		values = append(values, Value{Kind: "quantity", Text: text})
	}
	for _, word := range config.TimeWords {
		if !strings.Contains(question, word) || seen[word] {
			continue
		}
		seen[word] = true
		values = append(values, Value{Kind: "time", Text: word})
	}
	return values
}

// HasHedge reports whether the spoken answer admits an unconfirmed part.
func HasHedge(text string) bool {
	config, _, err := Load()
	if err != nil {
		return false
	}
	return containsAny(text, config.Hedges)
}

// OverClaims reports an answer that confirms delivery, timing or promises while
// hedging nothing.
func OverClaims(text string) bool {
	config, _, err := Load()
	if err != nil {
		return false
	}
	return containsAny(text, config.ClaimWords) && !HasHedge(text)
}

// BoundaryInstruction is the hard requirement appended to the answer prompt.
func BoundaryInstruction(uncovered []Value) string {
	if len(uncovered) == 0 {
		return ""
	}
	names := make([]string, 0, len(uncovered))
	for _, value := range uncovered {
		names = append(names, value.Text)
	}
	return "【未覆盖】问题里的「" + strings.Join(names, "、") + "」在答案事实里没有确认。" +
		"必须明确说出这一点还没核实；禁止确认包邮、免邮、可达、能发货、具体时效或承诺。"
}

// BoundarySentence is the safe spoken fallback when the model over-claims.
func BoundarySentence(uncovered []Value) string {
	if len(uncovered) == 0 {
		return "这一点我还没确认，先不跟你说死。"
	}
	return "不过" + uncovered[0].Text + "这边我还没确认到，先不跟你说死。"
}

// identityPattern catches a self-introduced name the model invents when it has
// no facts: "我叫小鹿" is a made-up identity, not a boundary answer.
var identityPattern = regexp.MustCompile(`(我叫|我的名字是|我名字叫|叫我)([\p{Han}A-Za-z0-9]{1,8})`)

// FabricatedIdentity reports a self-introduction that cannot come from the facts.
func FabricatedIdentity(text string) bool {
	if strings.Contains(text, "这里需要你填") || strings.Contains(text, "填自己") || strings.Contains(text, "括号") {
		return true
	}
	for _, match := range identityPattern.FindAllStringSubmatch(text, -1) {
		name := match[2]
		// "我叫什么名字" is the model echoing the question, not a name.
		if strings.HasPrefix(name, "什么") || strings.HasPrefix(name, "啥") || strings.HasPrefix(name, "哪") || strings.HasPrefix(name, "几") {
			continue
		}
		return true
	}
	return false
}

// BoundaryTemplate returns a random configured spoken fallback for one class.
func BoundaryTemplate(class string) string { return BoundaryTemplateFor(class, nil) }

// BoundaryTemplateFor fills {c} with the first uncovered value, so a configured
// fallback can talk about the actual region or quantity.
func BoundaryTemplateFor(class string, uncovered []string) string {
	config, _, err := Load()
	if err != nil {
		return ""
	}
	variants := config.BoundaryTemplates[class]
	if len(variants) == 0 {
		return ""
	}
	line := variants[rand.Intn(len(variants))]
	if len(uncovered) > 0 {
		line = strings.ReplaceAll(line, "{c}", uncovered[0])
	}
	return strings.TrimSpace(line)
}

// BoundaryPatterns is the rotation of sentence shapes handed to the model.
func BoundaryPatterns() []string {
	config, _, err := Load()
	if err != nil {
		return nil
	}
	return config.BoundaryPatterns
}

// BoundaryRhetorics is the rotation of rhetorical devices handed to the model.
func BoundaryRhetorics() []string {
	config, _, err := Load()
	if err != nil {
		return nil
	}
	return config.BoundaryRhetorics
}

// BoundaryStyles is the rotating tone list handed to the model.
func BoundaryStyles() []string {
	config, _, err := Load()
	if err != nil {
		return nil
	}
	return config.BoundaryStyles
}

// MetaBans are phrases that must never be spoken: they explain the system
// instead of answering the viewer.
func MetaBans() []string {
	config, _, err := Load()
	if err != nil {
		return nil
	}
	return config.MetaBans
}

// HasClaim reports a promise word in the answer.
func HasClaim(text string) bool {
	config, _, err := Load()
	if err != nil {
		return false
	}
	return containsAny(text, config.ClaimWords)
}

// benefitTokenPattern matches internal identifiers that must never be spoken.
var benefitTokenPattern = regexp.MustCompile(`current-benefit[0-9]*|福利=\d+[:：]?`)

// SpeakableFacts removes internal ids from a fact block before prompting.
func SpeakableFacts(facts string) string {
	out := benefitTokenPattern.ReplaceAllString(facts, "")
	out = strings.ReplaceAll(out, "；安全改写=", "；说法=")
	return strings.TrimSpace(out)
}

// ProductPhrases returns the phrases the fact package keeps repeating, most
// representative first. Different phrasings ("绸都杨鸭子" vs "四川绸都杨鸭子")
// all count, so an intro only has to name one of them.
func ProductPhrases(facts string) []string {
	counts := map[string]int{}
	for _, line := range strings.Split(facts, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		seen := map[string]bool{}
		for size := 4; size <= 10; size++ {
			for start := 0; start+size <= len(runes); start++ {
				phrase := string(runes[start : start+size])
				if !isHanPhrase(phrase) || seen[phrase] || introStopPhrases[phrase] {
					continue
				}
				seen[phrase] = true
				counts[phrase]++
			}
		}
	}
	type scored struct {
		phrase string
		score  int
	}
	var ranked []scored
	for phrase, count := range counts {
		if count < 2 {
			continue
		}
		ranked = append(ranked, scored{phrase, count * len([]rune(phrase))})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	out := make([]string, 0, 6)
	for _, item := range ranked {
		out = append(out, item.phrase)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

// DominantProductName is the single most representative product phrase.
func DominantProductName(facts string) string {
	phrases := ProductPhrases(facts)
	if len(phrases) == 0 {
		return ""
	}
	return phrases[0]
}

var introStopPhrases = map[string]bool{
	"官方直播间": true, "用户指定公共": true, "安全改写": true, "本直播间": true,
	"其中": true, "我们": true, "这个": true, "可以": true, "没有": true,
}

func isHanPhrase(text string) bool {
	for _, r := range text {
		if r < 0x4e00 || r > 0x9fff {
			return false
		}
	}
	return true
}

// IntroGrounded reports whether an intro names the room's dominant product or a
// configured category word. It is the cross-category replacement for hardcoded
// "must contain 美食/卤味" checks.
func IntroGrounded(text, facts string) bool {
	config, _, err := Load()
	if err == nil && containsAny(text, config.IntroRules.CategoryWords) {
		return true
	}
	product := DominantProductName(facts)
	for _, phrase := range ProductPhrases(facts) {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return product != "" && strings.Contains(text, product)
}

// PlacesIn returns the canonical place names a text mentions, matched through
// the same alias table the boundary check uses.
func PlacesIn(text string) []string {
	config, _, err := Load()
	if err != nil {
		return nil
	}
	var out []string
	for _, place := range config.Places {
		matched := false
		for _, alias := range append([]string{place.Name}, place.Aliases...) {
			if strings.TrimSpace(alias) != "" && strings.Contains(text, alias) {
				matched = true
				break
			}
		}
		if matched {
			out = append(out, place.Name)
		}
	}
	return out
}

// ValidateIntroFor checks an intro against the configured forbid words and,
// cross-category, against the room's dominant product name.
func ValidateIntroFor(text, facts string) error {
	config, _, err := Load()
	if err != nil {
		return fmt.Errorf("介绍规则不可用")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("空介绍")
	}
	if containsAny(text, config.IntroRules.ForbidWords) {
		return fmt.Errorf("出现误导词")
	}
	if !IntroGrounded(text, facts) {
		return fmt.Errorf("没说明卖什么")
	}
	return nil
}

// ValidateIntro checks a "what this room sells" intro against the configured
// rules: no live-animal wording, a category word must appear, and the opening
// must not lead with raw material instead of the product.
func ValidateIntro(text string) error {
	config, _, err := Load()
	if err != nil {
		return fmt.Errorf("介绍规则不可用")
	}
	rules := config.IntroRules
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("空介绍")
	}
	if containsAny(text, rules.ForbidWords) {
		return fmt.Errorf("出现误导词")
	}
	runes := []rune(text)
	head := text
	limit := rules.LeadRunes
	if limit <= 0 {
		limit = 12
	}
	if len(runes) > limit {
		head = string(runes[:limit])
	}
	if containsAny(head, rules.MaterialWords) && !containsAny(head, rules.CategoryWords) {
		return fmt.Errorf("开头讲原料")
	}
	if len(rules.CategoryWords) > 0 && !containsAny(text, rules.CategoryWords) {
		return fmt.Errorf("没说明品类")
	}
	return nil
}

func containsAny(text string, terms []string) bool {
	for _, term := range terms {
		if term != "" && strings.Contains(text, term) {
			return true
		}
	}
	return false
}
