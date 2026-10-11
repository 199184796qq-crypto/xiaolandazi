// Package contentpools owns immutable product/common content and source links.
// Display text is never an association key. It makes no provider calls.
package contentpools

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Product struct {
	ID    int64    `json:"id"`
	Link  string   `json:"link"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}
type SourceRow struct {
	ID       string            `json:"id"`
	Area     string            `json:"area"`
	Label    string            `json:"label"`
	Products []int64           `json:"products"`
	Fields   map[string]string `json:"fields"`
}
type Sources struct {
	Corrections []Correction `json:"corrections,omitempty"`
	Revision    int64        `json:"revision"`
	ObservedAt  int64        `json:"observed_at,omitempty"`
	Products    []Product    `json:"products"`
	Rows        []SourceRow  `json:"rows"`
}

func Digest(v any) string     { b, _ := json.Marshal(v); return fmt.Sprintf("%x", sha256.Sum256(b)) }
func (s Sources) SHA() string { s.Revision = 0; s.ObservedAt = 0; return Digest(s) }

type Change struct {
	ID       string  `json:"id"`
	Area     string  `json:"area"`
	Label    string  `json:"label"`
	Products []int64 `json:"products"`
	Before   string  `json:"before"`
	After    string  `json:"after"`
}

// Net changes are measured against the saved snapshot, not the previous poll.
func Changes(before, after Sources) []Change {
	for _, c := range before.Corrections {
		before.Rows = append(append([]SourceRow{}, before.Rows...), correctionSourceRow(c))
	}
	for _, c := range after.Corrections {
		after.Rows = append(append([]SourceRow{}, after.Rows...), correctionSourceRow(c))
	}
	a, b := map[string]SourceRow{}, map[string]SourceRow{}
	for _, r := range before.Rows {
		a[r.ID] = r
	}
	for _, r := range after.Rows {
		b[r.ID] = r
	}
	ids := map[string]bool{}
	for k := range a {
		ids[k] = true
	}
	for k := range b {
		ids[k] = true
	}
	keys := []string{}
	for k := range ids {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	result := []Change{}
	for _, id := range keys {
		x, y := a[id], b[id]
		fields := map[string]bool{}
		for k := range x.Fields {
			fields[k] = true
		}
		for k := range y.Fields {
			fields[k] = true
		}
		names := []string{}
		for k := range fields {
			names = append(names, k)
		}
		sort.Strings(names)
		if x.ID == "" {
			names = []string{"新增"}
		} else if y.ID == "" {
			names = []string{"删除"}
		}
		for _, k := range names {
			old, next := x.Fields[k], y.Fields[k]
			if k == "新增" {
				next = y.Label
			}
			if k == "删除" {
				old = x.Label
			}
			if old == next {
				continue
			}
			row := y
			if row.ID == "" {
				row = x
			}
			result = append(result, Change{ID: id + ":" + k, Area: row.Area, Label: row.Label + " · " + k, Products: row.Products, Before: old, After: next})
		}
	}
	return result
}

type Fact struct {
	ID       string   `json:"id"`
	Text     string   `json:"text"`
	Sources  []string `json:"sources"`
	Products []int64  `json:"products"`
	Owner    int64    `json:"owner,omitempty"`
	Private  bool     `json:"private,omitempty"`
}
type Task struct {
	Disabled       bool     `json:"disabled,omitempty"`
	ID             string   `json:"id"`
	Point          string   `json:"selling_point"`
	Angle          string   `json:"angle"`
	TargetAudience string   `json:"target_audience,omitempty"`
	PainPoint      string   `json:"pain_point,omitempty"`
	Sources        []string `json:"source_ids"`
}
type Pool struct {
	Key         string  `json:"key"`
	Label       string  `json:"label"`
	Owner       int64   `json:"owner"`
	Products    []int64 `json:"products"`
	Facts       []Fact  `json:"facts"`
	Fingerprint string  `json:"fingerprint"`
	Tasks       []Task  `json:"tasks"`
	Reused      bool    `json:"reused"`
}
type Set struct {
	ID         int64    `json:"id"`
	SourceSHA  string   `json:"source_sha"`
	Sources    Sources  `json:"sources"`
	SafeFacts  string   `json:"safe_facts"`
	SafetyHash string   `json:"safety_hash"`
	SafetyID   int64    `json:"safety_id"`
	Pools      []Pool   `json:"pools"`
	Notices    []Notice `json:"notices,omitempty"`
}

// Notices retain excluded/unfinished material outside the executable task pool.
type Notice struct {
	Pool      string `json:"pool"`
	Content   string `json:"content,omitempty"`
	Kind      string `json:"kind"`
	Reason    string `json:"reason"`
	Retryable bool   `json:"retryable,omitempty"`
}

func (s Set) HasUnfinishedContent() bool {
	for _, n := range s.Notices {
		if n.Retryable {
			return true
		}
	}
	return false
}

func IDs(values []int64) []int64 {
	m := map[int64]bool{}
	for _, v := range values {
		if v > 0 {
			m[v] = true
		}
	}
	r := []int64{}
	for v := range m {
		r = append(r, v)
	}
	sort.Slice(r, func(i, j int) bool { return r[i] < r[j] })
	return r
}
func Contains(ids []int64, id int64) bool {
	for _, v := range ids {
		if id == v {
			return true
		}
	}
	return false
}
func GroupPools(facts []Fact, products []Product, promptSHA string) []Pool {
	m := map[string]*Pool{}
	for _, p := range products {
		k := fmt.Sprintf("product:%d", p.ID)
		m[k] = &Pool{Key: k, Label: p.Link + "号 · " + p.Name, Owner: p.ID, Products: []int64{p.ID}, Facts: []Fact{}, Tasks: []Task{}}
	}
	for _, f := range facts {
		f.Products = IDs(f.Products)
		if len(f.Products) == 0 {
			continue
		}
		k := fmt.Sprintf("product:%d", f.Products[0])
		owner := f.Products[0]
		if len(f.Products) > 1 {
			k = "common:" + Digest(f.Products)[:16]
			owner = 0
		}
		if m[k] == nil {
			m[k] = &Pool{Key: k, Label: "公共内容", Owner: owner, Products: f.Products, Facts: []Fact{}, Tasks: []Task{}}
		}
		m[k].Facts = append(m[k].Facts, f)
	}
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	r := []Pool{}
	for _, k := range keys {
		p := m[k]
		p.Fingerprint = Digest(struct {
			Facts    []Fact
			Products []int64
			Prompt   string
		}{p.Facts, p.Products, promptSHA})
		r = append(r, *p)
	}
	return r
}
func ValidateSet(set Set) error {
	if set.SourceSHA == "" || set.SourceSHA != set.Sources.SHA() || strings.TrimSpace(set.SafeFacts) == "" {
		return errors.New("内容版本缺少对应的来源快照")
	}
	known := map[int64]bool{}
	for _, p := range set.Sources.Products {
		if p.ID <= 0 || p.Link == "" || known[p.ID] {
			return errors.New("商品编号重复或缺失")
		}
		known[p.ID] = true
	}
	if len(known) == 0 {
		return errors.New("方案没有生效商品")
	}
	sources := map[string]bool{}
	for _, r := range set.Sources.Rows {
		if r.ID == "" || sources[r.ID] {
			return errors.New("资料来源编号重复或缺失")
		}
		sources[r.ID] = true
	}
	seen := map[string]bool{}
	poolKeys := map[string]bool{}
	for _, p := range set.Pools {
		if len(IDs(p.Products)) != len(p.Products) {
			return errors.New("内容池适用商品重复")
		}
		if p.Key == "" || poolKeys[p.Key] || len(p.Products) == 0 {
			return errors.New("内容池范围无效")
		}
		poolKeys[p.Key] = true
		if p.Owner > 0 && (len(p.Products) != 1 || p.Products[0] != p.Owner) {
			return errors.New("商品专属池范围不匹配")
		}
		facts := map[string]Fact{}
		for _, f := range p.Facts {
			if f.ID == "" || len(f.Sources) == 0 {
				return errors.New("事实来源缺失")
			}
			if _, exists := facts[f.ID]; exists {
				return errors.New("事实编号重复")
			}
			for _, id := range f.Sources {
				if !sources[id] {
					return errors.New("事实引用了快照之外的资料")
				}
			}
			facts[f.ID] = f
		}
		for _, id := range p.Products {
			if !known[id] {
				return errors.New("内容池引用其他方案的商品")
			}
		}
		for _, t := range p.Tasks {
			if t.ID == "" || seen[t.ID] || strings.TrimSpace(t.Point) == "" || strings.TrimSpace(t.Angle) == "" || len(t.Sources) == 0 {
				return errors.New("任务编号或事实依据无效")
			}
			seen[t.ID] = true
			for _, id := range t.Sources {
				f, ok := facts[id]
				if !ok {
					return errors.New("任务引用了池外事实")
				}
				for _, product := range p.Products {
					if !Contains(f.Products, product) {
						return errors.New("任务的事实不适用于当前商品")
					}
				}
			}
		}
	}
	if len(seen) > 10000 {
		return errors.New("方案任务总数超过10000条，请减少重复任务后更新")
	}
	// An empty pool is a valid draft. It does not authorize any executable task.
	// Other products and approved content remain editable/savable.
	return nil
}
