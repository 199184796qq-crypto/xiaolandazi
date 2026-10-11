package contentpools

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Content is the entire model contract. All provenance is attached by callers.
type CorrectionContent struct {
	Summary      string `json:"理解说明"`
	Fact         string `json:"事实修正"`
	Forbidden    string `json:"表达限制"`
	Guidance     string `json:"展开指导"`
	Cancellation string `json:"取消说明"`
	Question     string `json:"澄清问题"`
}

func (c CorrectionContent) Persistent() bool {
	return c.Fact != "" || c.Forbidden != "" || c.Guidance != ""
}
func ParseCorrection(raw string) (CorrectionContent, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		if at := strings.IndexByte(s, '\n'); at >= 0 && strings.HasSuffix(s, "```") {
			s = strings.TrimSpace(s[at+1 : len(s)-3])
		}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &fields); err != nil {
		return CorrectionContent{}, errors.New("纠正整理未返回完整内容，原始结果已保留")
	}
	for _, k := range []string{"理解说明", "事实修正", "表达限制", "展开指导", "取消说明", "澄清问题"} {
		if _, ok := fields[k]; !ok {
			return CorrectionContent{}, fmt.Errorf("纠正整理缺少内容字段：%s", k)
		}
	}
	var c CorrectionContent
	d := json.NewDecoder(strings.NewReader(s))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil || d.Decode(new(any)) != io.EOF {
		return c, errors.New("纠正整理只允许本次内容字段")
	}
	for _, v := range []*string{&c.Summary, &c.Fact, &c.Forbidden, &c.Guidance, &c.Cancellation, &c.Question} {
		*v = strings.TrimSpace(*v)
		if utf8.RuneCountInString(*v) > 4000 {
			return c, errors.New("纠正内容过长")
		}
	}
	if c.Summary == "" || (c.Question == "" && !c.Persistent() && c.Cancellation == "") {
		return c, errors.New("纠正整理未给出可执行内容或澄清问题")
	}
	// A valid question takes precedence over any accompanying draft. The raw
	// draft remains recorded; callers must not adopt it while clarification waits.
	return c, nil
}

type Correction struct {
	Kind      string            `json:"kind,omitempty"`
	ID        string            `json:"id"`
	Products  []int64           `json:"products"`
	Sources   []string          `json:"sources"`
	PoolKey   string            `json:"pool_key,omitempty"`
	TaskID    string            `json:"task_id,omitempty"`
	Original  string            `json:"original"`
	Feedback  string            `json:"feedback"`
	Content   CorrectionContent `json:"content"`
	CreatedAt string            `json:"created_at"`
}

func correctionSourceRow(c Correction) SourceRow {
	return SourceRow{ID: "correction:" + c.ID, Area: "knowledge", Label: "用户纠正：" + c.Content.Summary, Products: c.Products, Fields: map[string]string{"事实修正": c.Content.Fact, "表达限制": c.Content.Forbidden, "展开指导": c.Content.Guidance, "取消说明": c.Content.Cancellation}}
}

func overlaps(a, b []int64) bool {
	for _, id := range a {
		if Contains(b, id) {
			return true
		}
	}
	return false
}

// The wire view has content and human-facing business labels only.
func CorrectionView(source Sources, products []int64) []map[string]any {
	out := []map[string]any{}
	for _, c := range source.Corrections {
		if !c.Content.Persistent() || !overlaps(c.Products, products) {
			continue
		}
		links := []string{}
		for _, p := range source.Products {
			if Contains(c.Products, p.ID) {
				links = append(links, p.Link)
			}
		}
		row := map[string]any{"适用商品链接": links, "用户原话（理解纠正范围，不能扩张限制）": c.Feedback, "用户确认的事实修正": c.Content.Fact, "表达限制": c.Content.Forbidden, "展开指导": c.Content.Guidance}
		if c.Kind == ExpressionConstraintKind {
			row["用户采用的实时表达约束"] = true
		}
		out = append(out, row)
	}
	return out
}

const ExpressionConstraintKind = "expression_constraint"

// Only explicitly adopted expression rules can override a running snapshot.
// Mutable product facts and other correction drafts keep their publication gate.
func ExpressionConstraintSource(source Sources) Sources {
	next := source
	next.Corrections = nil
	for _, c := range source.Corrections {
		if c.Kind == ExpressionConstraintKind && c.Content.Question == "" && c.Content.Forbidden != "" && c.Content.Fact == "" && c.Content.Guidance == "" && c.Content.Cancellation == "" {
			next.Corrections = append(next.Corrections, c)
		}
	}
	return next
}
func RenderCorrections(source Sources) string {
	all := []int64{}
	for _, p := range source.Products {
		all = append(all, p.ID)
	}
	rows := CorrectionView(source, all)
	if len(rows) == 0 {
		return ""
	}
	raw, _ := json.Marshal(rows)
	return "\n\n【用户已确认的长期纠正规则，按商品范围适用，后面的局部纠正覆盖前面的冲突部分】\n" + string(raw)
}

// Split an overlapping common pool before editing; never change other products.
func PartitionCorrectionPools(original Set, products []int64) (Set, error) {
	raw, _ := json.Marshal(original)
	var next Set
	if err := json.Unmarshal(raw, &next); err != nil {
		return next, err
	}
	pools := []Pool{}
	for _, p := range next.Pools {
		inside, outside := []int64{}, []int64{}
		for _, id := range p.Products {
			if Contains(products, id) {
				inside = append(inside, id)
			} else {
				outside = append(outside, id)
			}
		}
		if len(inside) == 0 || len(outside) == 0 {
			pools = append(pools, p)
			continue
		}
		for _, ids := range [][]int64{inside, outside} {
			b, _ := json.Marshal(p)
			var split Pool
			_ = json.Unmarshal(b, &split)
			suffix := Digest(ids)[:12]
			split.Key = "scope:" + Digest(p.Key + suffix)[:32]
			split.Products = ids
			split.Owner = 0
			split.Label = p.Label
			if len(ids) == 1 {
				split.Owner = ids[0]
			}
			for i := range split.Facts {
				scope := []int64{}
				for _, id := range split.Facts[i].Products {
					if Contains(ids, id) {
						scope = append(scope, id)
					}
				}
				split.Facts[i].Products = scope
			}
			for i := range split.Tasks {
				split.Tasks[i].ID = "split-" + Digest(p.Tasks[i].ID + suffix)[:32]
			}
			split.Fingerprint = Digest(split)
			pools = append(pools, split)
		}
	}
	next.Pools = pools
	return next, nil
}

// Only effective content is rebuilt; immutable old sets remain unchanged.
func RenderCorrectedFacts(set Set) string {
	lines := []string{}
	seen := map[string]bool{}
	for _, p := range set.Pools {
		for _, f := range p.Facts {
			if strings.TrimSpace(f.Text) == "" {
				continue
			}
			links := []string{}
			for _, item := range set.Sources.Products {
				if Contains(f.Products, item.ID) {
					links = append(links, item.Link)
				}
			}
			line := "- [商品事实] 适用链接=" + strings.Join(links, "、") + "；" + f.Text
			if !seen[line] {
				lines = append(lines, line)
				seen[line] = true
			}
		}
	}
	return strings.Join(lines, "\n") + RenderCorrections(set.Sources)
}
