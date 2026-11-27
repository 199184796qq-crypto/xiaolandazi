// Package qapair maintains reviewed answers independently of imported facts.
package qapair

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
)

const SpaceVersion = "qa-question-v1"

type Entry struct {
	ID         string                       `json:"id,omitempty"`
	Revision   int64                        `json:"revision"`
	ClientID   string                       `json:"client_id,omitempty"`
	Question   string                       `json:"question"`
	Answer     string                       `json:"answer"`
	Aliases    []string                     `json:"aliases"`
	Targets    []model.MaterialObjectTarget `json:"object_targets"`
	Conditions string                       `json:"conditions,omitempty"`
	Exceptions string                       `json:"exceptions,omitempty"`
	ValidFrom  string                       `json:"valid_from,omitempty"`
	ValidUntil string                       `json:"valid_until,omitempty"`
	Original   string                       `json:"original,omitempty"`
	Enabled    bool                         `json:"enabled"`
}

type Record struct {
	Entry   Entry
	Model   string
	Space   string
	Vectors [][]float32
}

type Catalog struct {
	Revision       string  `json:"revision"`
	ObjectRevision string  `json:"object_revision"`
	Items          []Entry `json:"items"`
}

type Proposal struct {
	Item      Entry   `json:"item"`
	Conflicts []Entry `json:"conflicts"`
	Identical bool    `json:"identical"`
}

type Mutation struct {
	Entry   Entry       `json:"entry"`
	Model   string      `json:"model"`
	Space   string      `json:"space"`
	Vectors [][]float32 `json:"vectors"`
}

type CommitInput struct {
	OperationID    string            `json:"operation_id"`
	Revision       string            `json:"revision"`
	ObjectRevision string            `json:"object_revision"`
	Items          []Entry           `json:"items"`
	Choices        map[string]string `json:"choices"`
	DeleteID       string            `json:"delete_id,omitempty"`
}

func Hash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func QuestionKey(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, s)
}

func TargetKey(t model.MaterialObjectTarget) string {
	switch t.Kind {
	case "plan":
		return "plan"
	case "product":
		return fmt.Sprintf("product:%d", t.LinkID)
	case "store":
		return "store:" + t.StoreName
	}
	return ""
}

func Normalize(e Entry) (Entry, error) {
	e.Question = strings.TrimSpace(e.Question)
	if QuestionKey(e.Question) == "" || len([]rune(e.Question)) > 300 || strings.TrimSpace(e.Answer) == "" || len([]rune(e.Answer)) > 2000 {
		return e, errors.New("请填写完整问题和回答；问题最多300字，回答最多2000字")
	}
	if len(e.Aliases) > 10 || len(e.Targets) > 100 || len([]rune(e.Original)) > 20000 || len([]rune(e.Conditions)) > 1000 || len([]rune(e.Exceptions)) > 1000 {
		return e, errors.New("相似问法、归属或条件超出长度限制")
	}
	if len(e.ClientID) > 80 || len(e.ID) > 64 {
		return e, errors.New("问答标识不正确")
	}
	seen := map[string]bool{QuestionKey(e.Question): true}
	aliases := []string{}
	for _, a := range e.Aliases {
		a = strings.TrimSpace(a)
		k := QuestionKey(a)
		if k == "" || len([]rune(a)) > 300 {
			return e, errors.New("相似问法限1至300字")
		}
		if !seen[k] {
			aliases = append(aliases, a)
			seen[k] = true
		}
	}
	e.Aliases = aliases
	seen = map[string]bool{}
	targets := []model.MaterialObjectTarget{}
	for _, t := range e.Targets {
		switch t.Kind {
		case "plan":
			t = model.MaterialObjectTarget{Kind: "plan"}
		case "product":
			if t.LinkID <= 0 {
				return e, errors.New("请从当前方案选择链接")
			}
			t = model.MaterialObjectTarget{Kind: "product", LinkID: t.LinkID}
		case "store":
			t.StoreName = strings.TrimSpace(t.StoreName)
			if t.StoreName == "" || len([]rune(t.StoreName)) > 100 {
				return e, errors.New("请选择具体门店")
			}
			t = model.MaterialObjectTarget{Kind: "store", StoreName: t.StoreName}
		default:
			return e, errors.New("归属类型不正确")
		}
		k := TargetKey(t)
		if !seen[k] {
			targets = append(targets, t)
			seen[k] = true
		}
	}
	if seen["plan"] && len(targets) > 1 {
		return e, errors.New("全局不能与其它归属同时选择")
	}
	sort.Slice(targets, func(i, j int) bool { return TargetKey(targets[i]) < TargetKey(targets[j]) })
	e.Targets = targets
	var from, until time.Time
	var err error
	if e.ValidFrom != "" {
		from, err = time.Parse(time.RFC3339, e.ValidFrom)
		if err != nil {
			return e, errors.New("生效时间不正确")
		}
	}
	if e.ValidUntil != "" {
		until, err = time.Parse(time.RFC3339, e.ValidUntil)
		if err != nil {
			return e, errors.New("到期时间不正确")
		}
	}
	if !from.IsZero() && !until.IsZero() && !until.After(from) {
		return e, errors.New("到期时间应晚于生效时间")
	}
	if len(targets) == 0 {
		e.Enabled = false
	}
	return e, nil
}

func Questions(e Entry) []string { return append([]string{e.Question}, e.Aliases...) }

func Same(a, b Entry) bool {
	return a.Question == b.Question && a.Answer == b.Answer && slices.Equal(a.Aliases, b.Aliases) && slices.Equal(a.Targets, b.Targets) && a.Conditions == b.Conditions && a.Exceptions == b.Exceptions && a.ValidFrom == b.ValidFrom && a.ValidUntil == b.ValidUntil && a.Enabled == b.Enabled && a.Original == b.Original
}

func Overlaps(a, b Entry) bool {
	// Only explicit shared owners collide. Global and local entries can coexist;
	// inheritance does not turn distinct owners into duplicate records.
	for _, x := range a.Targets {
		for _, y := range b.Targets {
			if TargetKey(x) == TargetKey(y) {
				return true
			}
		}
	}
	return len(a.Targets) == 0 && len(b.Targets) == 0
}

func Prepare(items []Entry, records []Record) ([]Proposal, error) {
	if len(items) == 0 || len(items) > 100 {
		return nil, errors.New("请一次保存1至100条问答")
	}
	out := []Proposal{}
	seen := map[string]bool{}
	for i, item := range items {
		e, err := Normalize(item)
		if err != nil {
			return nil, fmt.Errorf("第%d条：%w", i+1, err)
		}
		if e.ClientID == "" {
			e.ClientID = fmt.Sprintf("row-%d", i+1)
		}
		if seen[e.ClientID] {
			return nil, errors.New("问答标识重复")
		}
		seen[e.ClientID] = true
		for _, p := range out {
			if QuestionKey(e.Question) == QuestionKey(p.Item.Question) && Overlaps(e, p.Item) {
				return nil, errors.New("本次有相同对象下的重复问题，请先选择保留其中一条")
			}
		}
		p := Proposal{Item: e, Conflicts: []Entry{}}
		foundID := e.ID == ""
		for _, r := range records {
			old := r.Entry
			if e.ID == old.ID {
				foundID = true
				if old.Revision != e.Revision {
					return nil, errors.New("问答已变化，请刷新后重试")
				}
			}
			if e.ID == old.ID || QuestionKey(e.Question) == QuestionKey(old.Question) && Overlaps(e, old) {
				p.Conflicts = append(p.Conflicts, old)
				if Same(e, old) {
					p.Identical = true
				}
			}
		}
		if !foundID {
			return nil, errors.New("原问答不存在，请刷新")
		}
		out = append(out, p)
	}
	return out, nil
}

// Changes requires explicit decisions before any paid work. Residual owners
// keep the exact old content and vectors when only some owners are replaced.
func Changes(proposals []Proposal, records []Record, choices map[string]string) ([]Mutation, []string, int, error) {
	byID := map[string]Record{}
	for _, r := range records {
		byID[r.Entry.ID] = r
	}
	removed := map[string]bool{}
	writes := []Mutation{}
	kept := 0
	for _, p := range proposals {
		action := choices[p.Item.ClientID]
		if p.Identical {
			kept++
			continue
		}
		if len(p.Conflicts) > 0 && action != "replace" && action != "keep" {
			return nil, nil, 0, errors.New("请选择使用本次回答或保留库中回答")
		}
		if action == "keep" {
			kept++
			continue
		}
		if action != "" && action != "replace" {
			return nil, nil, 0, errors.New("保留选项不正确")
		}
		for _, old := range p.Conflicts {
			if removed[old.ID] {
				return nil, nil, 0, errors.New("多条修改涉及同一原问答，请分次处理")
			}
			removed[old.ID] = true
			if p.Item.ID == old.ID {
				continue
			}
			left := []model.MaterialObjectTarget{}
			for _, target := range old.Targets {
				over := false
				for _, t := range p.Item.Targets {
					if TargetKey(target) == TargetKey(t) {
						over = true
					}
				}
				if !over {
					left = append(left, target)
				}
			}
			if len(left) > 0 {
				r := byID[old.ID]
				r.Entry.Targets = left
				writes = append(writes, Mutation{r.Entry, r.Model, r.Space, r.Vectors})
			}
		}
		writes = append(writes, Mutation{Entry: p.Item})
	}
	ids := []string{}
	for id := range removed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return writes, ids, kept, nil
}
