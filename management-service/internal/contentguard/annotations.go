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
)

// Annotation is a lexical review marker, never a qualification decision or
// permission to output a draft. Evidence and contextual P2 checks are separate.
type Annotation struct {
	Hit
	Note           string `json:"note"`
	ReviewIdentity string `json:"review_identity,omitempty"`
	ReviewTitle    string `json:"review_title,omitempty"`
}
type AnnotationResult struct {
	Hits       []Annotation      `json:"hits"`
	RuleHashes map[string]string `json:"rule_hashes"`
	Mode       string            `json:"mode"`
}
type annotationEntry struct {
	ID                    string   `json:"id"`
	Title                 string   `json:"title"`
	Severity              string   `json:"severity"`
	Category              string   `json:"category"`
	Terms                 []string `json:"terms"`
	ExcludedTerms         []string `json:"excluded_terms"`
	Patterns              []string `json:"patterns"`
	Condition             string   `json:"condition"`
	RequiredEvidenceTypes []string `json:"required_evidence_types"`
	RequiredProof         string   `json:"required_proof"`
	SourceSection         string   `json:"source_section"`
	SourceReplacement     *string  `json:"source_replacement"`
	PreferredReplacement  string   `json:"preferred_replacement"`
	RequiredFactKeys      []string `json:"required_fact_keys"`
	FallbackReplacement   string   `json:"fallback_replacement"`
	Note                  string   `json:"note"`
	ReviewIdentity        string   `json:"review_identity,omitempty"`
}
type annotationMother struct {
	Version                  int               `json:"schema_version"`
	Layer                    string            `json:"layer"`
	Industry                 string            `json:"industry"`
	Severity                 string            `json:"severity"`
	PolicyVersion            string            `json:"policy_version"`
	Status                   string            `json:"status"`
	PolicyBasis              string            `json:"policy_basis"`
	Execution                json.RawMessage   `json:"execution"`
	CardDefaults             json.RawMessage   `json:"card_defaults"`
	CardContract             json.RawMessage   `json:"card_instance_contract"`
	Normalization            *Normalization    `json:"normalization"`
	Cards                    []annotationEntry `json:"card_templates"`
	Rules                    []annotationEntry `json:"rules"`
	ExcludedP0Claims         []string          `json:"excluded_p0_claims"`
	SourceConflictResolution string            `json:"source_conflict_resolution"`
	Logging                  json.RawMessage   `json:"logging"`
}
type annotationPolicy struct {
	matcher *Policy
	entries map[string]annotationEntry
	hash    string
}

var annotationCache = struct {
	sync.Mutex
	values map[string]*annotationPolicy
}{values: map[string]*annotationPolicy{}}

func compileAnnotation(md, tier string) (*annotationPolicy, error) {
	if len(md) == 0 || len(md) > 256*1024 {
		return nil, errors.New("风险标注母版为空或过大")
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(md)))
	key := tier + ":" + hash
	annotationCache.Lock()
	cached := annotationCache.values[key]
	annotationCache.Unlock()
	if cached != nil {
		return cached, nil
	}
	blocks := jsonBlock.FindAllStringSubmatch(md, -1)
	if len(blocks) != 1 {
		return nil, errors.New("风险标注母版须只有一个JSON块")
	}
	var data annotationMother
	d := json.NewDecoder(strings.NewReader(blocks[0][1]))
	d.DisallowUnknownFields()
	if err := d.Decode(&data); err != nil {
		return nil, fmt.Errorf("风险标注母版结构错误: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("风险标注JSON有额外内容")
	}
	if data.Version != 1 || data.Layer != "L2" || data.Industry != "ordinary_food" || data.Severity != tier || data.PolicyVersion == "" {
		return nil, errors.New("风险标注层级或范围无效")
	}
	if (tier == "P1" && len(data.Rules) > 0) || (tier == "P2" && len(data.Cards) > 0) {
		return nil, errors.New("风险母版混用了P1/P2结构")
	}
	entries := data.Cards
	if tier == "P2" {
		entries = data.Rules
	} else if tier != "P1" {
		return nil, errors.New("风险标注档位无效")
	}
	if len(entries) == 0 || len(entries) > 500 {
		return nil, errors.New("风险标注规则数量无效")
	}
	n := Normalization{Form: "NFKC", CaseFold: true, RemoveZeroWidth: true, IgnoreSpaces: true, KeepOffsets: true}
	if data.Normalization != nil {
		n = *data.Normalization
	}
	if n.Form != "NFKC" || !n.KeepOffsets || len(n.Aliases) > 0 {
		return nil, errors.New("风险标注归一化配置暂不支持")
	}
	policy := &annotationPolicy{matcher: &Policy{Data: Schema{Severity: tier, PolicyVersion: data.PolicyVersion, Normalization: n}}, entries: map[string]annotationEntry{}, hash: hash}
	for _, e := range entries {
		if e.ReviewIdentity != "" && e.ReviewIdentity != "atomic_attribute" {
			return nil, errors.New("未知风险卡语义归并方式")
		}
		if e.ID == "" || policy.entries[e.ID].ID != "" || len(e.Terms)+len(e.Patterns) == 0 || e.Category == "" {
			return nil, errors.New("风险标注规则ID或匹配条件无效")
		}
		if tier == "P2" {
			if e.Severity != "P2" {
				return nil, errors.New("P2规则档位无效")
			}
			switch e.Condition {
			case "always_if_no_higher_priority_block", "context_includes_unsupported_scarcity_or_purchase_claim", "inventory_claim_without_matching_current_inventory_basis", "scarcity_claim_without_matching_inventory_or_activity_basis":
			default:
				return nil, fmt.Errorf("未知P2标注条件: %s", e.Condition)
			}
		}
		cr := compiledRule{rule: Rule{ID: e.ID, Severity: tier, Category: e.Category, Scope: "ordinary_food", Condition: e.Condition}}
		if tier == "P1" {
			cr.rule.Condition = "evidence_review_required"
		}
		terms := []string{}
		for _, term := range e.Terms {
			normalized, _ := policy.matcher.normalize(term)
			if len(normalized) == 0 {
				return nil, errors.New("风险标注词为空")
			}
			terms = append(terms, regexp.QuoteMeta(string(normalized)))
		}
		sort.Slice(terms, func(i, j int) bool { return len(terms[i]) > len(terms[j]) })
		patterns := append([]string{}, e.Patterns...)
		if len(terms) > 0 {
			patterns = append(patterns, strings.Join(terms, "|"))
		}
		for _, pattern := range patterns {
			re, err := regexp.Compile(pattern)
			if err != nil || re.MatchString("") {
				return nil, errors.New("风险标注正则无效或命中空串")
			}
			cr.patterns = append(cr.patterns, re)
		}
		policy.matcher.rules = append(policy.matcher.rules, cr)
		policy.entries[e.ID] = e
	}
	annotationCache.Lock()
	if len(annotationCache.values) >= 64 {
		annotationCache.values = map[string]*annotationPolicy{}
	}
	annotationCache.values[key] = policy
	annotationCache.Unlock()
	return policy, nil
}

func (p *annotationPolicy) exclusionRanges(text string) map[string][]span {
	ranges := map[string][]span{}
	input, offsets := p.matcher.normalize(text)
	for id, e := range p.entries {
		for _, term := range e.ExcludedTerms {
			normalized, _ := p.matcher.normalize(term)
			if len(normalized) == 0 {
				continue
			}
			for i := 0; i+len(normalized) <= len(input); i++ {
				if string(input[i:i+len(normalized)]) == string(normalized) {
					ranges[id] = append(ranges[id], span{offsets[i].start, offsets[i+len(normalized)-1].end})
				}
			}
		}
		if len(ranges[id]) > 0 {
			sort.Slice(ranges[id], func(i, j int) bool { return ranges[id][i].start < ranges[id][j].start })
			merged := []span{}
			for _, r := range ranges[id] {
				if len(merged) > 0 && merged[len(merged)-1].end >= r.start {
					merged[len(merged)-1].end = max(merged[len(merged)-1].end, r.end)
				} else {
					merged = append(merged, r)
				}
			}
			ranges[id] = merged
		}
	}
	return ranges
}

// Read-only marking: does not rewrite text, populate evidence or run a model.
// Context-dependent P2 terms are marked for review, not called violations.
func Annotate(text, p0MD, p1MD, p2MD string) (AnnotationResult, error) {
	out := AnnotationResult{Hits: []Annotation{}, RuleHashes: map[string]string{}, Mode: "lexical_review_only"}
	if len([]rune(text)) > 100000 {
		return out, errors.New("风险标注文本超过100000字符")
	}
	p0, err := Compile(p0MD)
	if err != nil {
		return out, err
	}
	p1, err := compileAnnotation(p1MD, "P1")
	if err != nil {
		return out, err
	}
	p2, err := compileAnnotation(p2MD, "P2")
	if err != nil {
		return out, err
	}
	out.RuleHashes = map[string]string{"P0": p0.SHA256, "P1": p1.hash, "P2": p2.hash}
	if strings.TrimSpace(text) == "" {
		return out, nil
	}
	for _, h := range p0.Scan(text, "ordinary_food").Hits {
		out.Hits = append(out.Hits, Annotation{Hit: h, Note: "P0母版字面命中；保守策略提示"})
	}
	for _, p := range []*annotationPolicy{p1, p2} {
		excluded := p.exclusionRanges(text)
		for _, h := range p.matcher.Scan(text, "ordinary_food").Hits {
			ranges := excluded[h.RuleID]
			at := sort.Search(len(ranges), func(i int) bool { return ranges[i].start > h.Start }) - 1
			if at >= 0 && ranges[at].end >= h.End {
				continue
			}
			e := p.entries[h.RuleID]
			note := e.Note
			if h.Severity == "P1" {
				note = "需核对对应资质/依据；标色不表示已有或缺少凭证：" + e.RequiredProof
			} else {
				note = "需检查表达及上下文；标色不等于已判违规：" + note
			}
			out.Hits = append(out.Hits, Annotation{Hit: h, Note: note, ReviewIdentity: e.ReviewIdentity, ReviewTitle: e.Title})
		}
	}
	sort.SliceStable(out.Hits, func(i, j int) bool {
		if out.Hits[i].Start != out.Hits[j].Start {
			return out.Hits[i].Start < out.Hits[j].Start
		}
		return out.Hits[i].Severity < out.Hits[j].Severity
	})
	return out, nil
}
