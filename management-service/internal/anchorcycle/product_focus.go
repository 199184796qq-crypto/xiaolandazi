package anchorcycle

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Immutable inputs are captured with the plan version; adoption counters and
// the optional auxiliary branch live in Redis only. No Core/audio protocol changes.
type ProductPolicy struct {
	Version           string `json:"version"`
	MainSharePercent  int    `json:"main_share_percent"`
	MinMainRounds     int    `json:"min_main_rounds"`
	AuxiliaryMaxChars int    `json:"auxiliary_max_chars"`
}

func (p ProductPolicy) Validate() error {
	if p.Version == "" || p.MainSharePercent < 50 || p.MainSharePercent > 90 || p.MinMainRounds < 1 || p.MinMainRounds > 20 || p.AuxiliaryMaxChars < 80 || p.AuxiliaryMaxChars > 300 {
		return errors.New("商品讲解调度母版配置无效")
	}
	return nil
}

type FocusProduct struct {
	LinkKey string   `json:"link_key"`
	Name    string   `json:"name"`
	Roles   []string `json:"roles,omitempty"`
}
type ScopedFact struct {
	ID         string   `json:"id,omitempty"`
	BenefitIDs []string `json:"benefit_ids,omitempty"`
	Applicable bool     `json:"applicable,omitempty"`
	Text       string   `json:"text"`
	Links      []string `json:"links"`
	Shared     bool     `json:"shared"`
}
type ScopedTask struct {
	Links      []string `json:"links"`
	Shared     bool     `json:"shared"`
	Comparison bool     `json:"comparison"`
}
type ProductFocus struct {
	Policy              ProductPolicy         `json:"policy"`
	PolicySource        string                `json:"policy_source"`
	FactsSHA            string                `json:"facts_sha256"`
	Products            []FocusProduct        `json:"products"`
	Facts               []ScopedFact          `json:"facts"`
	Tasks               map[string]ScopedTask `json:"tasks"`
	CurrentLink         string                `json:"current_link,omitempty"`
	CurrentCycle        int                   `json:"current_cycle,omitempty"`
	AuxiliaryActive     bool                  `json:"auxiliary_active,omitempty"`
	AuxiliaryCursor     int                   `json:"auxiliary_cursor,omitempty"`
	CompletedMainRounds int                   `json:"completed_main_rounds,omitempty"`
	LastAuxiliaryRound  int                   `json:"last_auxiliary_round,omitempty"`
	MainChars           int                   `json:"main_chars,omitempty"`
	AuxiliaryChars      int                   `json:"auxiliary_chars,omitempty"`
}
type ProductAudit struct {
	Passed       bool   `json:"passed"`
	TextSHA      string `json:"text_sha256"`
	ScopeSHA     string `json:"scope_sha256"`
	Reason       string `json:"reason,omitempty"`
	Quote        string `json:"quote,omitempty"`
	PromptSource string `json:"prompt_source,omitempty"`
}

func ProductDigest(value any) string {
	raw, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func spokenCandidate(c Candidate) string {
	if c.OutputText != "" {
		return strings.TrimSpace(c.OutputText)
	}
	return strings.TrimSpace(c.Text)
}
func hasProductRole(p FocusProduct, role string) bool {
	for _, v := range p.Roles {
		if v == role {
			return true
		}
	}
	return false
}

func (p *ProductFocus) MainLink() string {
	for _, item := range p.Products {
		if hasProductRole(item, "main") {
			return item.LinkKey
		}
	}
	return ""
}
func (p *ProductFocus) ValidateProducts() error {
	seen, mainCount := map[string]bool{}, 0
	for _, item := range p.Products {
		if item.LinkKey == "" || seen[item.LinkKey] {
			return errors.New("商品调度存在空或重复链接")
		}
		seen[item.LinkKey] = true
		if hasProductRole(item, "main") {
			mainCount++
		}
	}
	if len(p.Products) < 2 || mainCount != 1 {
		return errors.New("多商品讲解须在商品卡中设置且仅设置一个主推商品，其余定位作为辅推；原方案不变")
	}
	return nil
}
func (p *ProductFocus) ResetRun() {
	p.CurrentLink, p.CurrentCycle = "", 0
	p.AuxiliaryActive, p.AuxiliaryCursor = false, 0
	p.CompletedMainRounds, p.LastAuxiliaryRound, p.MainChars, p.AuxiliaryChars = 0, 0, 0, 0
}
func (s *State) EnsureProductFocus() {
	p := s.ProductFocus
	if p == nil {
		return
	}
	if !p.AuxiliaryActive {
		p.CurrentLink = p.MainLink()
	}
	p.CurrentCycle = s.CycleNumber
}
func (s *State) AuxiliaryCharBudget() int {
	p := s.ProductFocus
	if p == nil || p.Policy.MainSharePercent < 50 {
		return 0
	}
	return min(s.Config.MaxChars, p.Policy.AuxiliaryMaxChars, max(0, p.MainChars*(100-p.Policy.MainSharePercent)/p.Policy.MainSharePercent-p.AuxiliaryChars))
}

// An auxiliary is one bounded body paragraph after a completed main round,
// never a second opening/body/closing cycle. Only adopted speech spends budget.
func (s *State) beginAuxiliary() bool {
	p := s.ProductFocus
	if p == nil || s.PriceHook != nil || p.CompletedMainRounds-p.LastAuxiliaryRound < p.Policy.MinMainRounds || s.AuxiliaryCharBudget() < 80 {
		return false
	}
	feedbackPending := s.FeedbackPending
	other := p.auxiliaryProducts()
	for i := 0; i < len(other); i++ {
		s.Status, s.Error = "ready", ""
		s.FeedbackPending = feedbackPending
		p.CurrentLink = other[(p.AuxiliaryCursor+i)%len(other)].LinkKey
		p.AuxiliaryActive, s.Phase = true, "body"
		s.chooseNext()
		if s.Status != "spacing_blocked" && s.Status != "compliance_blocked" {
			return true
		}
	}
	p.AuxiliaryActive, s.Phase, s.Status, s.Error = false, "opening", "ready", ""
	s.FeedbackPending = feedbackPending
	s.EnsureProductFocus()
	return false
}
func (p *ProductFocus) auxiliaryProducts() []FocusProduct {
	other := []FocusProduct{}
	for _, item := range p.Products {
		if !hasProductRole(item, "main") {
			other = append(other, item)
		}
	}
	// Traffic is visited before other secondary roles; ties retain card order.
	sort.SliceStable(other, func(i, j int) bool {
		return hasProductRole(other[i], "traffic") && !hasProductRole(other[j], "traffic")
	})
	return other
}
func (s *State) recordProductAdoption(t Task, text string) {
	p := s.ProductFocus
	if p == nil {
		return
	}
	chars := len([]rune(strings.TrimSpace(text)))
	if t.ProductMode == "auxiliary" {
		p.AuxiliaryChars += chars
		p.LastAuxiliaryRound = p.CompletedMainRounds
		for i, product := range p.auxiliaryProducts() {
			if product.LinkKey == t.FocusLink {
				p.AuxiliaryCursor = i + 1
				break
			}
		}
	} else {
		p.MainChars += chars
		if s.Phase == "closing" {
			p.CompletedMainRounds++
		}
	}
}

// Used when a fresh immutable product plan is attached to a new run.
func (s *State) SelectProductTask() { s.chooseNext() }
func containsLink(links []string, key string) bool {
	for _, v := range links {
		if v == key {
			return true
		}
	}
	return false
}
func (t Task) ProductLinks() []string {
	var links []string
	_ = json.Unmarshal([]byte(t.AllowedLinksJSON), &links)
	return links
}
func (t *Task) setProductLinks(links []string) {
	raw, _ := json.Marshal(links)
	t.AllowedLinksJSON = string(raw)
}
func (s *State) productTaskEligible(t Task) bool {
	if s.ProductFocus == nil {
		return true
	}
	a, ok := s.ProductFocus.Tasks[t.Key]
	if !ok {
		return false
	}
	p := s.ProductFocus
	if p.AuxiliaryActive {
		if a.Shared || !containsLink(a.Links, p.CurrentLink) {
			return false
		}
		if a.Comparison {
			for _, key := range a.Links {
				if key != p.CurrentLink && key != p.MainLink() {
					return false
				}
			}
		}
		return true
	}
	if a.Comparison {
		return false
	}
	return a.Shared || containsLink(a.Links, s.ProductFocus.CurrentLink)
}
func (s *State) focusTask(t Task) Task {
	if s.ProductFocus == nil {
		return t
	}
	s.EnsureProductFocus()
	t.FocusLink = s.ProductFocus.CurrentLink
	t.setProductLinks([]string{t.FocusLink})
	t.ProductMode = "single"
	t.ProductMaxChars = s.Config.MaxChars
	if s.PriceHook != nil && s.PriceHook.Task.FocusLink != "" {
		t.FocusLink = s.PriceHook.Task.FocusLink
		t.AllowedLinksJSON = s.PriceHook.Task.AllowedLinksJSON
		t.ProductMode = s.PriceHook.Task.ProductMode
		return t
	}
	if s.ProductFocus.AuxiliaryActive && s.PriceHook == nil {
		t.setProductLinks([]string{s.ProductFocus.MainLink(), t.FocusLink})
		t.ProductMode = "auxiliary"
		t.ProductMaxChars = s.AuxiliaryCharBudget()
	}
	return t
}
func (s *State) ProductFacts(t Task) string {
	if t.ProductMode == "lecture" {
		return t.LectureFacts
	}
	if s.ProductFocus == nil {
		return s.Facts
	}
	lines := []string{}
	for _, fact := range s.ProductFocus.Facts {
		allowed := fact.Shared
		if !allowed && fact.Applicable {
			for _, key := range fact.Links {
				if key == t.FocusLink {
					allowed = true
					break
				}
			}
		} else if !allowed && len(fact.Links) > 0 {
			allowed = true
			for _, key := range fact.Links {
				if !containsLink(t.ProductLinks(), key) {
					allowed = false
					break
				}
			}
		}
		if allowed {
			lines = append(lines, fact.Text)
		}
	}
	return strings.Join(lines, "\n")
}
func (s *State) ProductScopeSHA(t Task) string {
	return ProductDigest(struct {
		Focus, Mode, Facts string
		Links              []string
		Policy             ProductPolicy
		MaxChars           int
	}{t.FocusLink, t.ProductMode, s.ProductFacts(t), t.ProductLinks(), s.ProductFocus.Policy, t.ProductMaxChars})
}

// Numbers alone are never link references (prices, dates and ages may coincide).
func LinkAliases(key string) []string {
	key = strings.TrimSpace(key)
	if regexp.MustCompile(`^[0-9]{1,3}$`).MatchString(key) {
		result := []string{key + "号链接", key + "号", "链接" + key, "链接 " + key}
		if len(key) == 1 && key != "0" {
			ch := []rune("零一二三四五六七八九")[int(key[0]-'0')]
			result = append(result, string(ch)+"号链接", string(ch)+"号", "链接"+string(ch))
		}
		return result
	}
	return []string{key}
}

var linkAddressPrefix = regexp.MustCompile(`(街|路|巷|道|弄|胡同|栋|幢|楼|室|院|门牌|地址|住址|店址|月|第)\s*$`)
var linkAddressSuffix = regexp.MustCompile(`^\s*(楼|栋|幢|室|院)`)

func MentionsLink(text, key string) bool {
	for _, alias := range LinkAliases(key) {
		pattern := regexp.QuoteMeta(alias)
		if len(alias) > 0 && alias[0] >= '0' && alias[0] <= '9' {
			pattern = `(^|[^0-9])` + pattern
		}
		if len(alias) > 0 && alias[len(alias)-1] >= '0' && alias[len(alias)-1] <= '9' {
			pattern += `([^0-9]|$)`
		}
		for _, location := range regexp.MustCompile(pattern).FindAllStringIndex(text, -1) {
			// A bare ordinal is ambiguous. Street numbers and dates are not SKU
			// references; explicit “号链接/链接N” still win. Semantic auditing
			// remains mandatory and neither input nor output is rewritten here.
			if strings.HasSuffix(alias, "号") {
				start := location[0] + strings.Index(text[location[0]:location[1]], alias)
				end := start + len(alias)
				if linkAddressPrefix.MatchString(text[:start]) || linkAddressSuffix.MatchString(text[end:]) {
					continue
				}
			}
			return true
		}
	}
	return false
}
func (s *State) ProductTextIssue(text string, t Task) string {
	if s.ProductFocus == nil {
		return ""
	}
	for _, item := range s.ProductFocus.Products {
		if !containsLink(t.ProductLinks(), item.LinkKey) && MentionsLink(text, item.LinkKey) {
			return "product_focus_cross_link"
		}
	}
	return ""
}
func (s *State) productCandidateIssue(c Candidate) string {
	if s.ProductFocus == nil {
		return ""
	}
	t := s.Task()
	if t.ProductMode == "auxiliary" && len([]rune(spokenCandidate(c))) > t.ProductMaxChars {
		return "product_focus_auxiliary_budget"
	}
	if issue := s.ProductTextIssue(c.Text, t); issue != "" {
		return issue
	}
	if issue := s.ProductTextIssue(spokenCandidate(c), t); issue != "" {
		return issue
	}
	if c.ProductAudit == nil || c.ProductAudit.TextSHA != ProductDigest(spokenCandidate(c)) || c.ProductAudit.ScopeSHA != s.ProductScopeSHA(t) {
		return "product_focus_audit_missing"
	}
	if !c.ProductAudit.Passed {
		return "product_focus_semantic_rejected"
	}
	return ""
}
func (s *State) CheckProductVoice(segment Segment) error {
	if s.ProductFocus == nil {
		return nil
	}
	t := segment.Task
	c := segment.Selected
	if t.FocusLink == "" || c.ContentIssue != "" || c.ProductAudit == nil || !c.ProductAudit.Passed || c.ProductAudit.TextSHA != ProductDigest(spokenCandidate(c)) || c.ProductAudit.ScopeSHA != s.ProductScopeSHA(t) || s.ProductTextIssue(spokenCandidate(c), t) != "" || (t.ProductMode == "auxiliary" && len([]rune(spokenCandidate(c))) > t.ProductMaxChars) {
		return errors.New("商品范围未通过检查，不能进入声音队列")
	}
	return nil
}
