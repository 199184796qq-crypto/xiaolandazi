package anchorcycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strings"
)

// Work cards contain intent and program-owned provenance, never preview speech.
// Saved generations accumulate; per-run selection never approves spoken content.
type AngleHook struct {
	ID       string   `json:"hook_id"`
	Title    string   `json:"title"`
	Concern  string   `json:"audience_concern"`
	Entry    string   `json:"entry_method"`
	Action   string   `json:"explanation_action"`
	Facts    []string `json:"fact_ids"`
	Products []string `json:"product_ids"`
	Benefits []string `json:"benefit_ids"`
}
type HookFact struct {
	ExpressionGuidance []map[string]string `json:"expression_guidance,omitempty"`
	ID                 string              `json:"id"`
	Products           []string            `json:"product_ids"`
	Benefits           []string            `json:"benefit_ids"`
	Data               string              `json:"data"`
}

// Captured by the program from the saved task's source associations. Nil means
// a legacy task without a binding; an empty scope never grants all plan facts.
// This provenance stays in run state and is not sent as an index for models.
type TaskFactScope struct {
	Corrections []map[string]any `json:"corrections,omitempty"`
	Facts       []HookFact       `json:"facts"`
}
type HookCalculation struct {
	Label    string   `json:"label"`
	Operator string   `json:"operator"`
	Operands []string `json:"operands"`
	Facts    []string `json:"fact_ids"`
	Unit     string   `json:"unit"`
}
type ComputedHookCalculation struct {
	HookCalculation
	Result    string `json:"result"`
	Precision int    `json:"decimal_places"`
}

func (s *State) HookProducts() []string {
	ids := s.Task().ProductLinks()
	if len(ids) == 0 && s.Task().FocusLink != "" {
		ids = []string{s.Task().FocusLink}
	}
	if len(ids) == 0 && s.ProductFocus != nil {
		for _, p := range s.ProductFocus.Products {
			ids = append(ids, p.LinkKey)
		}
	}
	if len(ids) == 0 {
		ids = []string{"current"}
	}
	return ids
}

func (s *State) HookFacts() []HookFact {
	products := s.HookProducts()
	facts := []HookFact{}
	seen := map[string]bool{}
	add := func(id, data string, links, benefits []string, guidance []map[string]string) {
		if strings.TrimSpace(data) == "" {
			return
		}
		if id == "" {
			id = fmt.Sprintf("F%03d", len(facts)+1)
		}
		if seen[id] {
			return
		}
		seen[id] = true
		if len(links) == 0 {
			links = products
		}
		if benefits == nil {
			benefits = []string{}
		}
		// Older saved versions retain welfare identifiers in the fact line.
		// Expose those literal identifiers instead of inviting the planner to
		// invent a reference; typed source identifiers remain unchanged.
		if len(benefits) == 0 {
			if at := strings.Index(data, "福利="); at >= 0 {
				parts := strings.FieldsFunc(data[at+len("福利="):], func(r rune) bool { return r == '；' || r == ';' || r == '\n' })
				if len(parts) > 0 {
					if id := strings.TrimSpace(parts[0]); id != "" {
						benefits = append(benefits, id)
					}
				}
			}
		}
		facts = append(facts, HookFact{ID: id, Data: data, Products: links, Benefits: benefits, ExpressionGuidance: guidance})
	}
	if scope := s.Task().FactScope; scope != nil {
		for _, f := range scope.Facts {
			allowed := len(f.Products) == 0 || containsLink(products, "current")
			for _, link := range products {
				allowed = allowed || containsLink(f.Products, link)
			}
			if allowed {
				add(f.ID, f.Data, f.Products, f.Benefits, f.ExpressionGuidance)
			}
		}
		return facts
	}
	if s.ProductFocus == nil {
		for _, line := range strings.Split(s.Facts, "\n") {
			add("", line, products, nil, nil)
		}
		return facts
	}
	task := s.Task()
	for _, f := range s.ProductFocus.Facts {
		// Reuse the established per-product scope; public applicability never
		// grants permission to introduce another product into this task.
		one := *s
		focus := *s.ProductFocus
		focus.Facts = []ScopedFact{f}
		one.ProductFocus = &focus
		if one.ProductFacts(task) != "" {
			add(f.ID, f.Text, f.Links, f.BenefitIDs, nil)
		}
	}
	return facts
}

// Used only to supply task-local material. ProductFacts retains the complete
// product scope for existing review/provenance consumers.
func (s *State) GenerationFacts() string {
	lines := []string{}
	for _, f := range s.HookFacts() {
		lines = append(lines, f.Data)
	}
	return strings.Join(lines, "\n")
}

func ParseHookPlan(raw string, facts []HookFact, products []string) (*AnglePlan, error) {
	body := strings.TrimSpace(raw)
	if strings.HasPrefix(body, "```") {
		if i := strings.IndexByte(body, '\n'); i >= 0 && strings.HasSuffix(body, "```") {
			body = strings.TrimSpace(body[i+1 : len(body)-3])
		}
	}
	var wire struct {
		Goal         string            `json:"communication_goal"`
		Hooks        []AngleHook       `json:"hooks"`
		Calculations []HookCalculation `json:"calculations,omitempty"`
	}
	d := json.NewDecoder(strings.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&wire); err != nil {
		return nil, fmt.Errorf("钩子规划结构不兼容: %w", err)
	}
	if d.Decode(new(any)) != io.EOF || strings.TrimSpace(wire.Goal) == "" || len(wire.Hooks) < 1 || len(wire.Hooks) > 5 || len(wire.Calculations) > 8 {
		return nil, errors.New("钩子规划缺少必要结构或数量越界")
	}
	byID := map[string]HookFact{}
	for _, f := range facts {
		byID[f.ID] = f
	}
	seen := map[string]bool{}
	for _, h := range wire.Hooks {
		if strings.TrimSpace(h.ID) == "" || seen[h.ID] || strings.TrimSpace(h.Title) == "" || strings.TrimSpace(h.Concern) == "" || strings.TrimSpace(h.Entry) == "" || strings.TrimSpace(h.Action) == "" || len(h.Facts) == 0 || len(h.Products) == 0 || h.Benefits == nil {
			return nil, errors.New("钩子字段不完整或编号重复")
		}
		seen[h.ID] = true
		for _, id := range h.Products {
			if !containsLink(products, id) {
				return nil, errors.New("钩子引用未授权商品编号")
			}
		}
		benefits := map[string]bool{}
		for _, id := range h.Facts {
			f, ok := byID[id]
			if !ok {
				return nil, errors.New("钩子引用不存在的事实编号")
			}
			matches := false
			for _, product := range h.Products {
				if containsLink(f.Products, product) {
					matches = true
				}
			}
			if !matches {
				return nil, errors.New("钩子事实与商品编号不匹配")
			}
			for _, benefit := range f.Benefits {
				benefits[benefit] = true
			}
		}
		for _, id := range h.Benefits {
			if !benefits[id] {
				return nil, errors.New("钩子福利未引用对应事实编号")
			}
		}
	}
	return &AnglePlan{Goal: wire.Goal, Cards: wire.Hooks, Calculations: wire.Calculations, Raw: raw}, nil
}

func (s *State) CurrentAngleHook() *AngleHook {
	p := s.CurrentAnglePlan()
	if p == nil || len(p.Cards) == 0 {
		return nil
	}
	start := (s.Used[s.Task().Key].Count + len(s.Attempts)) % len(p.Cards)
	best := start
	u := s.AngleUsage(s.AnglePlanKey())
	for offset := 1; offset < len(p.Cards); offset++ {
		i := (start + offset) % len(p.Cards)
		a, b := p.Cards[i].ID, p.Cards[best].ID
		if u.DirectionPlays[a] < u.DirectionPlays[b] || (u.DirectionPlays[a] == u.DirectionPlays[b] && p.LifetimePlays[a] < p.LifetimePlays[b]) {
			best = i
		}
	}
	h := p.Cards[best]
	return &h
}

var decimalOperand = regexp.MustCompile(`^-?[0-9]{1,12}(\.[0-9]{1,6})?$`)
var factNumber = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
var spokenDecimalPrice = regexp.MustCompile(`([0-9]+)块([0-9]{1,2})(?:毛|角|分)?`)

// Computes explicitly requested arithmetic using rational values. It neither
// guesses a promotion nor scans/rejects the generated speech. Unsupported
// operations/unknown source operands are omitted and recorded, never fatal.
func (s *State) CurrentHookCalculations() ([]ComputedHookCalculation, []string) {
	h, p := s.CurrentAngleHook(), s.CurrentAnglePlan()
	if h == nil || p == nil {
		return nil, nil
	}
	facts := map[string]HookFact{}
	for _, f := range s.HookFacts() {
		facts[f.ID] = f
	}
	results, warnings := []ComputedHookCalculation{}, []string{}
	for _, c := range p.Calculations {
		selected := len(c.Facts) > 0
		for _, id := range c.Facts {
			if !containsLink(h.Facts, id) {
				selected = false
			}
		}
		if !selected {
			continue
		}
		value, err := computeHookCalculation(c, facts)
		if err != nil {
			warnings = append(warnings, c.Label+": "+err.Error())
			continue
		}
		results = append(results, ComputedHookCalculation{HookCalculation: c, Result: value, Precision: 6})
	}
	return results, warnings
}

func computeHookCalculation(c HookCalculation, facts map[string]HookFact) (string, error) {
	if c.Label == "" || len(c.Operands) < 2 || len(c.Operands) > 8 || len(c.Facts) == 0 {
		return "", errors.New("计算结构缺失")
	}
	numbers := []*big.Rat{}
	available := map[string]bool{}
	for _, id := range c.Facts {
		f, ok := facts[id]
		if !ok {
			return "", errors.New("计算引用未知事实")
		}
		data := f.Data
		for _, marker := range []string{"禁说=", "安全改写="} {
			if at := strings.Index(data, marker); at >= 0 {
				data = data[:at]
			}
		}
		data = spokenDecimalPrice.ReplaceAllString(data, "$1.$2")
		for _, n := range factNumber.FindAllString(data, -1) {
			if v, ok := new(big.Rat).SetString(n); ok {
				available[v.RatString()] = true
			}
		}
	}
	for _, operand := range c.Operands {
		if !decimalOperand.MatchString(operand) {
			return "", errors.New("非十进制计算输入")
		}
		v, _ := new(big.Rat).SetString(operand)
		if !available[v.RatString()] {
			return "", errors.New("计算输入未引用来源数值")
		}
		numbers = append(numbers, v)
	}
	value := new(big.Rat).Set(numbers[0])
	for _, n := range numbers[1:] {
		switch c.Operator {
		case "add":
			value.Add(value, n)
		case "subtract":
			value.Sub(value, n)
		case "multiply":
			value.Mul(value, n)
		case "divide":
			if n.Sign() == 0 {
				return "", errors.New("除数为零")
			}
			value.Quo(value, n)
		default:
			return "", errors.New("不支持的计算操作")
		}
	}
	return strings.TrimRight(strings.TrimRight(value.FloatString(6), "0"), "."), nil
}
