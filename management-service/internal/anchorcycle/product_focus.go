package anchorcycle

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Immutable inputs are captured with the plan version; adoption counters and
// the product rotation cursor live in Redis only. No Core/audio protocol changes.
type ProductPolicy struct {
	Version      string         `json:"version"`
	RoleWeights  map[string]int `json:"role_weights,omitempty"`  // 角色→基准频率比例（main/traffic/benefit/profit/...），缺省见 defaultRoleWeight
	PointWeights map[string]int `json:"point_weights,omitempty"` // 卖点权重（卖点名→权重），缺省 1
	AngleWeights map[string]int `json:"angle_weights,omitempty"` // 角度权重（卖点名×角度名→权重），缺省 1
}

// defaultRoleWeight 返回角色的基准频率比例（后台可配，这里给缺省值）。
// 未配置的角色一律 1；用户可通过调节频率系数（frequency_weight）在其上放大/缩小。
func defaultRoleWeight(role string) int {
	switch role {
	case "main":
		return 40
	case "traffic":
		return 25
	case "benefit":
		return 20
	case "profit":
		return 10
	default:
		return 5
	}
}

// roleBaselineWeight 返回商品的角色基准频率（多角色取最大；未配置走默认值）。
func (p ProductPolicy) roleBaselineWeight(item FocusProduct) int {
	best := 0
	for _, role := range item.Roles {
		w := 0
		if p.RoleWeights != nil {
			w = p.RoleWeights[role]
		}
		if w <= 0 {
			w = defaultRoleWeight(role)
		}
		if w > best {
			best = w
		}
	}
	if best <= 0 {
		return 1
	}
	return best
}

func (p ProductPolicy) Validate() error {
	if p.Version == "" {
		return errors.New("商品讲解调度母版配置无效")
	}
	return nil
}

type FocusProduct struct {
	LinkKey         string              `json:"link_key"`
	Name            string              `json:"name"`
	Roles           []string            `json:"roles,omitempty"`
	Spec            string              `json:"spec,omitempty"`
	DailyPrice      string              `json:"daily_price,omitempty"`
	Quantity        string              `json:"quantity,omitempty"`
	FrequencyWeight int                 `json:"frequency_weight,omitempty"` // 手动微调系数×100（100=1.0 不干预，范围 50~200 即 0.5~2.0；0=未设置=1.0）
	SellingPoints   []FocusSellingPoint `json:"selling_points,omitempty"`
}

// FocusSellingPoint 是「每个商品独立」的卖点池成员：卖点名 + tier + 主推标记 + 权重，
// 以及该卖点下的角度（每个角度带独立讲解权重）。归属由商品（LinkKey）程序保证。
type FocusSellingPoint struct {
	ID             int64               `json:"id,omitempty"` // DB 稳定主键，跨场复用 Slot 用它，不受卖点措辞变化影响
	Name           string              `json:"name"`
	Tier           string              `json:"tier,omitempty"`
	Weight         int                 `json:"weight,omitempty"`
	IsMain         bool                `json:"is_main,omitempty"`
	TargetAudience string              `json:"target_audience,omitempty"`
	PainPoint      string              `json:"pain_point,omitempty"`
	Angles         []FocusSellingAngle `json:"angles,omitempty"`
}

type FocusSellingAngle struct {
	Angle  string `json:"angle"`
	Weight int    `json:"weight,omitempty"`
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
	if len(p.Products) > 0 {
		return p.Products[0].LinkKey
	}
	return ""
}
func (p *ProductFocus) ValidateProducts() error {
	seen := map[string]bool{}
	for _, item := range p.Products {
		if item.LinkKey == "" || seen[item.LinkKey] {
			return errors.New("商品调度存在空或重复链接")
		}
		seen[item.LinkKey] = true
	}
	if len(p.Products) < 2 {
		return errors.New("多商品讲解须至少两个商品；原方案不变")
	}
	return nil
}
func (p *ProductFocus) ResetRun() {
	p.CurrentLink, p.CurrentCycle = "", 0
	p.AuxiliaryActive, p.AuxiliaryCursor = false, 0
	p.CompletedMainRounds, p.LastAuxiliaryRound, p.MainChars, p.AuxiliaryChars = 0, 0, 0, 0
}

// EnsureProductFocus 仅在 CurrentLink 为空时初始化到加权序列首元素；之后轮换由 advanceProduct 显式推进。
func (s *State) EnsureProductFocus() {
	p := s.ProductFocus
	if p == nil {
		return
	}
	if p.CurrentLink == "" {
		seq := p.productSequence()
		if len(seq) == 0 {
			p.CurrentLink = p.MainLink()
		} else {
			p.CurrentLink = seq[0].LinkKey
			p.AuxiliaryCursor = 0
		}
	}
	p.CurrentCycle = s.CycleNumber
}

// advanceProduct 把 CurrentLink 推进到加权轮换序列的下一个商品（不区分主/辅）。
// 返回是否成功推进（供 body 段落切换使用）。
func (s *State) advanceProduct() bool {
	p := s.ProductFocus
	if p == nil {
		return false
	}
	seq := p.productSequence()
	if len(seq) == 0 {
		return false
	}
	p.AuxiliaryCursor = (p.AuxiliaryCursor + 1) % len(seq)
	p.CurrentLink = seq[p.AuxiliaryCursor].LinkKey
	return true
}

func (p *ProductFocus) allProducts() []FocusProduct {
	return p.Products
}

// coefficient 返回商品的手动微调系数（frequency_weight×100，默认 1.0，钳制 0.5~2.0）。
func (p *ProductFocus) coefficient(item FocusProduct) float64 {
	c := item.FrequencyWeight
	if c <= 0 {
		return 1.0
	}
	f := float64(c) / 100.0
	if f < 0.5 {
		f = 0.5
	}
	if f > 2.0 {
		f = 2.0
	}
	return f
}

// normalizedProductWeights 返回「链接→归一化频率占比」（总和=1.0）。
// 每个商品的原始频率 = 角色基准比例 × 手动微调系数，再整体归一化。
func (p *ProductFocus) normalizedProductWeights() map[string]float64 {
	if len(p.Products) == 0 {
		return map[string]float64{}
	}
	raw := make([]float64, len(p.Products))
	total := 0.0
	for i, item := range p.Products {
		raw[i] = float64(p.Policy.roleBaselineWeight(item)) * p.coefficient(item)
		total += raw[i]
	}
	out := map[string]float64{}
	if total <= 0 {
		each := 1.0 / float64(len(p.Products))
		for _, item := range p.Products {
			out[item.LinkKey] = each
		}
		return out
	}
	for i, item := range p.Products {
		out[item.LinkKey] = raw[i] / total
	}
	return out
}

// productSequence 返回按「角色基准 × 手动系数」归一化后加权交错展开的全商品轮换序列：
// 频率占比越高的商品出现次数越多，且同类尽量不相邻。所有商品平等参与（含主推）。
func (p *ProductFocus) productSequence() []FocusProduct {
	items := p.allProducts()
	if len(items) == 0 {
		return nil
	}
	if len(items) == 1 {
		return items
	}
	weights := p.normalizedProductWeights()
	// 归一化占比转成整数权重（×10000 取整，避免浮点误差），再做加权交错。
	scale := 10000
	intWeights := make([]int, len(items))
	total := 0
	for i, item := range items {
		w := int(weights[item.LinkKey]*float64(scale) + 0.5)
		if w < 1 {
			w = 1
		}
		intWeights[i] = w
		total += w
	}
	counts := make([]int, len(items))
	seq := make([]FocusProduct, 0, total)
	for placed := 0; placed < total; placed++ {
		best, bestRatio := -1, math.MaxFloat64
		for i := range items {
			ratio := float64(counts[i]) / float64(intWeights[i])
			if ratio < bestRatio || (ratio == bestRatio && (best < 0 || i < best)) {
				best, bestRatio = i, ratio
			}
		}
		counts[best]++
		seq = append(seq, items[best])
	}
	return seq
}
func (s *State) recordProductAdoption(t Task, text string) {
	p := s.ProductFocus
	if p == nil {
		return
	}
	chars := len([]rune(strings.TrimSpace(text)))
	p.MainChars += chars
	if s.Phase == "closing" {
		p.CompletedMainRounds++
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

// productSellingPointKey 返回「商品链接×卖点名」的加权键。同一卖点在不同商品下独立加权，
// 复用与计数也按商品隔离（Key 已带商品前缀）。
func productSellingPointKey(linkKey, sellingPoint string) string {
	return linkKey + "×" + sellingPoint
}

// ExpandProductSellingPoints 把每个商品独立的卖点池展开成任务队列（商品轮→卖点轮→角度）。
// 展开后的 Task.Key 带商品链接前缀，保证同名卖点在不同商品下不复用/不撞 key；
// SellingPoint/Angle 保持原始名（用于生成提示词），LinkKey 记录商品归属。
// 同时按卖点/角度的权重回填 Policy.PointWeights/AngleWeights（带商品前缀的键），
// 供 chooseNext 的卖点级、角度级加权调度使用。
func ExpandProductSellingPoints(products []FocusProduct, policy *ProductPolicy) []Task {
	if policy == nil {
		return nil
	}
	if policy.PointWeights == nil {
		policy.PointWeights = map[string]int{}
	}
	if policy.AngleWeights == nil {
		policy.AngleWeights = map[string]int{}
	}
	tasks := make([]Task, 0)
	for _, product := range products {
		for _, sp := range product.SellingPoints {
			pointKey := productSellingPointKey(product.LinkKey, sp.Name)
			weight := sp.Weight
			if weight < 1 {
				weight = 1
			}
			policy.PointWeights[pointKey] = weight
			for ai, angle := range sp.Angles {
				angleWeight := angle.Weight
				if angleWeight < 1 {
					angleWeight = 1
				}
				// 跨场复用稳定键：卖点用 DB 主键（若有），角度用卖点内循环序号，
				// 卖点/角度措辞变化不影响跨场命中。卖点无 ID 时退回卖点名（兼容旧数据）。
				spIdent := sp.Name
				if sp.ID > 0 {
					spIdent = fmt.Sprintf("#%d", sp.ID)
				}
				task := Task{
					LinkKey:        product.LinkKey,
					SellingPoint:   sp.Name,
					Angle:          angle.Angle,
					SlotKey:        fmt.Sprintf("%s×%s×#%d", product.LinkKey, spIdent, ai+1),
					Tier:           sp.Tier,
					TargetAudience: sp.TargetAudience,
					PainPoint:      sp.PainPoint,
				}
				task.Key = task.schedulingKey()
				policy.AngleWeights[task.Key] = angleWeight
				tasks = append(tasks, task)
			}
		}
	}
	return tasks
}
