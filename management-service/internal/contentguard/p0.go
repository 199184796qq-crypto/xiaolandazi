// Package contentguard is a deterministic, data-driven P0 gate. It does not
// call a model, infer certification, or certify legal/platform compliance.
package contentguard

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type Execution struct {
	DefaultScope        string   `json:"default_scope"`
	MaxRewrites         int      `json:"max_rewrites"`
	MaxConsecutiveSkips int      `json:"max_consecutive_skips"`
	RewriteScope        string   `json:"rewrite_scope"`
	RecheckScope        string   `json:"recheck_scope"`
	OnExhausted         string   `json:"on_exhausted"`
	CountRejected       bool     `json:"count_rejected_as_adopted"`
	AllowUserOverride   bool     `json:"allow_user_override"`
	FrontendShowMetrics bool     `json:"frontend_show_metrics"`
	LogFields           []string `json:"log_fields"`
}
type Normalization struct {
	Form            string            `json:"unicode_form"`
	CaseFold        bool              `json:"case_fold"`
	RemoveZeroWidth bool              `json:"remove_zero_width"`
	IgnoreSpaces    bool              `json:"ignore_spaces_for_match"`
	KeepOffsets     bool              `json:"keep_original_offsets"`
	Aliases         map[string]string `json:"aliases"`
}
type Rule struct {
	ID            string   `json:"id"`
	Severity      string   `json:"severity"`
	Category      string   `json:"category"`
	Scope         string   `json:"scope"`
	Condition     string   `json:"condition"`
	ReviewMode    string   `json:"review_mode,omitempty"`
	Terms         []string `json:"terms"`
	ExcludedTerms []string `json:"excluded_terms,omitempty"`
	Patterns      []string `json:"patterns"`
	RequiredBasis []string `json:"required_basis,omitempty"`
	Note          string   `json:"note"`
}
type Schema struct {
	Version       int           `json:"schema_version"`
	Layer         string        `json:"layer"`
	Severity      string        `json:"severity"`
	PolicyVersion string        `json:"policy_version"`
	Status        string        `json:"status"`
	PolicyBasis   string        `json:"policy_basis"`
	Execution     Execution     `json:"execution"`
	Normalization Normalization `json:"normalization"`
	Rules         []Rule        `json:"rules"`
}
type compiledRule struct {
	rule       Rule
	patterns   []*regexp.Regexp
	exclusions []*regexp.Regexp
}
type alias struct{ from, to []rune }
type Policy struct {
	Data    Schema
	SHA256  string
	rules   []compiledRule
	aliases []alias
}

func (p *Policy) JSON() string { raw, _ := json.Marshal(p.Data); return string(raw) }

// MatchKey applies the policy's own normalization rules so a reviewed term and
// the same term in generated speech can be compared without exposing the
// policy's private offset representation.
func (p *Policy) MatchKey(text string) string {
	normalized, _ := p.normalize(text)
	return string(normalized)
}

type Hit struct {
	RuleID     string `json:"rule_id"`
	Severity   string `json:"severity"`
	Category   string `json:"category"`
	Text       string `json:"matched_text"`
	Start      int    `json:"original_start"` // Unicode offsets, zero-based, end exclusive.
	End        int    `json:"original_end"`
	Condition  string `json:"condition"`
	ReviewMode string `json:"review_mode,omitempty"`
}
type Report struct {
	Passed        bool   `json:"passed"`
	PolicyVersion string `json:"policy_version"`
	SHA256        string `json:"rule_sha256"`
	Scope         string `json:"scope"`
	Hits          []Hit  `json:"hits"`
	Error         string `json:"error,omitempty"`
	LatencyMS     int64  `json:"latency_ms"`
}

var jsonBlock = regexp.MustCompile("(?s)```json\\s*(.*?)\\s*```")
var policyCache = struct {
	sync.Mutex
	policies map[string]*Policy
}{policies: map[string]*Policy{}}

// Cache immutable compiled matchers across rooms; the caller persists the
// exact mother snapshot in its ephemeral run, not just a mutable file path.
func Compile(md string) (*Policy, error) {
	if len(md) == 0 || len(md) > 256*1024 {
		return nil, errors.New("P0母版为空或超过256KB")
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(md)))
	policyCache.Lock()
	cached := policyCache.policies[hash]
	policyCache.Unlock()
	if cached != nil {
		return cached, nil
	}
	blocks := jsonBlock.FindAllStringSubmatch(md, -1)
	if len(blocks) != 1 {
		return nil, errors.New("P0母版须有且只有一个JSON代码块")
	}
	p := &Policy{SHA256: hash}
	d := json.NewDecoder(strings.NewReader(blocks[0][1]))
	d.DisallowUnknownFields()
	if err := d.Decode(&p.Data); err != nil {
		return nil, fmt.Errorf("P0母版结构错误: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("P0母版JSON包含多余内容")
	}
	data := &p.Data
	if data.Version != 1 || data.Layer != "L1" || data.Severity != "P0" || data.PolicyVersion == "" || len(data.Rules) == 0 || len(data.Rules) > 500 {
		return nil, errors.New("P0层级、版本或规则数量无效")
	}
	e := data.Execution
	if (e.DefaultScope != "all" && e.DefaultScope != "ordinary_food") || e.MaxRewrites < 0 || e.MaxRewrites > 5 || e.MaxConsecutiveSkips < 1 || e.MaxConsecutiveSkips > 100 || e.RewriteScope != "matched_sentence_or_paragraph" || e.RecheckScope != "whole_candidate" || e.OnExhausted != "skip_current_selling_point_angle" || e.AllowUserOverride || e.CountRejected {
		return nil, errors.New("P0执行配置不支持或尝试绕过硬拦截")
	}
	if data.Normalization.Form != "NFKC" || !data.Normalization.KeepOffsets {
		return nil, errors.New("P0归一化须使用NFKC并保留原文位置")
	}
	seenAliases := map[string]string{}
	for from, to := range data.Normalization.Aliases {
		f, _ := p.basicNormalize(from)
		t, _ := p.basicNormalize(to)
		if len(f) == 0 || len(t) == 0 {
			return nil, errors.New("P0变体映射不能为空")
		}
		if prior, ok := seenAliases[string(f)]; ok && prior != string(t) {
			return nil, errors.New("P0变体归一化映射冲突")
		}
		seenAliases[string(f)] = string(t)
	}
	for f, t := range seenAliases {
		p.aliases = append(p.aliases, alias{[]rune(f), []rune(t)})
	}
	sort.Slice(p.aliases, func(i, j int) bool {
		if len(p.aliases[i].from) != len(p.aliases[j].from) {
			return len(p.aliases[i].from) > len(p.aliases[j].from)
		}
		return string(p.aliases[i].from) < string(p.aliases[j].from)
	})
	ids := map[string]bool{}
	for _, r := range data.Rules {
		if r.ReviewMode == "" {
			r.ReviewMode = "hard"
		}
		if r.ID == "" || ids[r.ID] || r.Severity != "P0" || (r.Scope != "all" && r.Scope != "ordinary_food") || (r.Condition != "always" && r.Condition != "when_unsubstantiated") || (r.ReviewMode != "hard" && r.ReviewMode != "semantic") || (len(r.Terms) == 0 && len(r.Patterns) == 0) {
			return nil, fmt.Errorf("P0规则%s结构、范围或条件无效", r.ID)
		}
		ids[r.ID] = true
		cr := compiledRule{rule: r}
		terms := []string{}
		for _, term := range r.Terms {
			normalized, _ := p.normalize(term)
			if len(normalized) == 0 {
				return nil, errors.New("P0匹配词不能为空")
			}
			terms = append(terms, string(normalized))
		}
		sort.Slice(terms, func(i, j int) bool { return len(terms[i]) > len(terms[j]) })
		if len(terms) > 0 {
			for i := range terms {
				terms[i] = regexp.QuoteMeta(terms[i])
			}
			cr.patterns = append(cr.patterns, regexp.MustCompile("(?:"+strings.Join(terms, "|")+")"))
		}
		for _, excluded := range r.ExcludedTerms {
			normalized, _ := p.normalize(excluded)
			if len(normalized) == 0 {
				return nil, fmt.Errorf("P0规则%s排除词不能为空", r.ID)
			}
			cr.exclusions = append(cr.exclusions, regexp.MustCompile(regexp.QuoteMeta(string(normalized))))
		}
		for _, pattern := range r.Patterns {
			re, err := regexp.Compile(pattern)
			if err != nil || re.MatchString("") {
				return nil, fmt.Errorf("P0规则%s正则无效或可命中空串", r.ID)
			}
			cr.patterns = append(cr.patterns, re)
		}
		p.rules = append(p.rules, cr)
	}
	policyCache.Lock()
	if len(policyCache.policies) >= 64 {
		policyCache.policies = map[string]*Policy{}
	}
	policyCache.policies[hash] = p
	policyCache.Unlock()
	return p, nil
}

type span struct{ start, end int }

func (p *Policy) basicNormalize(text string) ([]rune, []span) {
	output := []rune{}
	offsets := []span{}
	for i, r := range []rune(text) {
		if p.Data.Normalization.RemoveZeroWidth && unicode.Is(unicode.Cf, r) {
			continue
		}
		if p.Data.Normalization.IgnoreSpaces && unicode.IsSpace(r) {
			continue
		}
		for _, n := range norm.NFKC.String(string(r)) {
			if p.Data.Normalization.CaseFold {
				n = unicode.ToLower(n)
			}
			output = append(output, n)
			offsets = append(offsets, span{i, i + 1})
		}
	}
	return output, offsets
}
func (p *Policy) normalize(text string) ([]rune, []span) {
	input, offsets := p.basicNormalize(text)
	out := []rune{}
	positions := []span{}
	for i := 0; i < len(input); {
		matched := false
		for _, a := range p.aliases {
			if i+len(a.from) > len(input) || string(input[i:i+len(a.from)]) != string(a.from) {
				continue
			}
			for _, r := range a.to {
				out = append(out, r)
				positions = append(positions, span{offsets[i].start, offsets[i+len(a.from)-1].end})
			}
			i += len(a.from)
			matched = true
			break
		}
		if !matched {
			out = append(out, input[i])
			positions = append(positions, offsets[i])
			i++
		}
	}
	return out, positions
}

// Conditional rules fail closed here. No verified-evidence provider is wired
// in this P0-only phase; copying a number or default qualification flag into a
// fact package is not verification and never bypasses the gate.
func (p *Policy) Scan(text, scope string) Report {
	started := time.Now()
	r := Report{Passed: true, PolicyVersion: p.Data.PolicyVersion, SHA256: p.SHA256, Scope: scope, Hits: []Hit{}}
	if scope != "all" && scope != "ordinary_food" {
		r.Passed = false
		r.Error = "P0审核范围无效"
		return r
	}
	original := []rune(text)
	if len(original) > 100000 {
		r.Passed = false
		r.Error = "P0候选文本超过检查上限"
		return r
	}
	if strings.TrimSpace(text) == "" {
		r.Passed = false
		r.Error = "P0候选文本为空"
		return r
	}
	input, offsets := p.normalize(text)
	normalized := string(input)
	// Precompute byte-to-rune positions once; repeated dense matches must not
	// repeatedly count the entire prefix (quadratic work on malicious drafts).
	runePositions := make(map[int]int, len(input)+1)
	for at := range normalized {
		runePositions[at] = len(runePositions)
	}
	runePositions[len(normalized)] = len(input)
	seen := map[string]bool{}
	for _, cr := range p.rules {
		if cr.rule.Scope != "all" && cr.rule.Scope != scope {
			continue
		}
		exclusions := []span{}
		for _, pattern := range cr.exclusions {
			for _, match := range pattern.FindAllStringIndex(normalized, -1) {
				exclusions = append(exclusions, span{start: match[0], end: match[1]})
			}
		}
		for _, pattern := range cr.patterns {
			for _, match := range pattern.FindAllStringIndex(normalized, -1) {
				excluded := false
				for _, exclusion := range exclusions {
					if exclusion.start <= match[0] && exclusion.end >= match[1] {
						excluded = true
						break
					}
				}
				if excluded {
					continue
				}
				start, end := runePositions[match[0]], runePositions[match[1]]
				if start == end {
					continue
				}
				left, right := offsets[start].start, offsets[end-1].end
				key := fmt.Sprintf("%s:%d:%d", cr.rule.ID, left, right)
				if seen[key] {
					continue
				}
				seen[key] = true
				r.Hits = append(r.Hits, Hit{RuleID: cr.rule.ID, Severity: cr.rule.Severity, Category: cr.rule.Category, Text: string(original[left:right]), Start: left, End: right, Condition: cr.rule.Condition, ReviewMode: cr.rule.ReviewMode})
			}
		}
	}
	sort.Slice(r.Hits, func(i, j int) bool {
		if r.Hits[i].Start != r.Hits[j].Start {
			return r.Hits[i].Start < r.Hits[j].Start
		}
		return r.Hits[i].End > r.Hits[j].End
	})
	r.Passed = len(r.Hits) == 0
	r.LatencyMS = time.Since(started).Milliseconds()
	return r
}

type Unit struct {
	ID    string `json:"id"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
	Hits  []Hit  `json:"hits"`
}
type Repair struct {
	BaseText      string `json:"base_text"`
	Units         []Unit `json:"units"`
	RewriteNumber int    `json:"rewrite_number"`
}

func PlanRepair(text string, report Report, number int) Repair {
	runes := []rune(text)
	result := Repair{BaseText: text, RewriteNumber: number, Units: []Unit{}}
	start := 0
	appendUnit := func(end int) {
		hits := []Hit{}
		for _, h := range report.Hits {
			if h.Start < end && h.End > start {
				hits = append(hits, h)
			}
		}
		if len(hits) > 0 {
			result.Units = append(result.Units, Unit{ID: fmt.Sprintf("part_%d", len(result.Units)+1), Start: start, End: end, Text: string(runes[start:end]), Hits: hits})
		}
		start = end
	}
	for i, r := range runes {
		if strings.ContainsRune("。！？!?\n", r) {
			appendUnit(i + 1)
		}
	}
	if start < len(runes) {
		appendUnit(len(runes))
	}
	if len(result.Units) == 0 {
		result.Units = append(result.Units, Unit{ID: "part_1", Start: 0, End: len(runes), Text: text, Hits: report.Hits})
	}
	return result
}

// The model supplies replacements only; the server owns all original ranges.
// Unknown/duplicate/missing IDs never turn into a full-text rewrite fallback.
func (r Repair) Apply(raw string, maxChars int) (string, error) {
	if maxChars <= 0 {
		maxChars = 100000 // The UI's unlimited option still has a safety ceiling.
	}
	text := strings.TrimSpace(raw)
	if blocks := jsonBlock.FindAllStringSubmatch(text, -1); len(blocks) == 1 {
		text = blocks[0][1]
	}
	var envelope struct {
		Replacements []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"replacements"`
	}
	d := json.NewDecoder(strings.NewReader(text))
	d.DisallowUnknownFields()
	if err := d.Decode(&envelope); err != nil {
		return "", fmt.Errorf("局部重写须返回replacements JSON: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return "", errors.New("局部重写返回包含额外内容")
	}
	if len(envelope.Replacements) != len(r.Units) {
		return "", errors.New("局部重写必须覆盖全部且仅覆盖指定句段")
	}
	allowed := map[string]bool{}
	replacement := map[string]string{}
	for _, u := range r.Units {
		allowed[u.ID] = true
	}
	for _, item := range envelope.Replacements {
		if !allowed[item.ID] || replacement[item.ID] != "" || strings.TrimSpace(item.Text) == "" || len([]rune(item.Text)) > maxChars {
			return "", errors.New("局部重写句段ID重复、未知、空白或过长")
		}
		replacement[item.ID] = item.Text
	}
	base := []rune(r.BaseText)
	var out strings.Builder
	cursor := 0
	for _, u := range r.Units {
		if u.Start < cursor || u.End > len(base) || u.End < u.Start {
			return "", errors.New("局部重写原文范围无效")
		}
		out.WriteString(string(base[cursor:u.Start]))
		out.WriteString(replacement[u.ID])
		cursor = u.End
	}
	out.WriteString(string(base[cursor:]))
	if len([]rune(out.String())) > maxChars {
		return "", errors.New("局部重写组装后超过本段字数上限")
	}
	return out.String(), nil
}
