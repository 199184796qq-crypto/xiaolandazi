package qapair

import (
	"sort"
	"strings"
	"time"

	"livecompanion/management/internal/danmakutest"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/semantic"
)

type Match struct {
	ID         string  `json:"id"`
	Revision   int64   `json:"revision"`
	Question   string  `json:"question"`
	Answer     string  `json:"answer"`
	Scope      string  `json:"scope"`
	Conditions string  `json:"conditions,omitempty"`
	Exceptions string  `json:"exceptions,omitempty"`
	Score      float64 `json:"score"`
	Status     string  `json:"status"`
	Reason     string  `json:"reason,omitempty"`
}

type Group struct {
	Ask     string  `json:"ask"`
	Matches []Match `json:"matches"`
}

func Active(e Entry, now time.Time) bool {
	if !e.Enabled || len(e.Targets) == 0 {
		return false
	}
	if e.ValidFrom != "" {
		t, err := time.Parse(time.RFC3339, e.ValidFrom)
		if err != nil || now.Before(t) {
			return false
		}
	}
	if e.ValidUntil != "" {
		t, err := time.Parse(time.RFC3339, e.ValidUntil)
		if err != nil || !now.Before(t) {
			return false
		}
	}
	return true
}

func Scope(e Entry, links []danmakutest.Link) string {
	labels := []string{}
	for _, t := range e.Targets {
		switch t.Kind {
		case "plan":
			labels = append(labels, "全局")
		case "store":
			labels = append(labels, t.StoreName)
		case "product":
			for _, l := range links {
				if l.ID == t.LinkID {
					labels = append(labels, l.Name)
				}
			}
		}
	}
	return strings.Join(labels, "、")
}

const (
	AnswerMinScore = 0.65
	AnswerLimit    = 10
)

// PreviewCandidates scores an operator's query, including entries below the
// answer threshold. The workbench has no intent-derived object scope.
func PreviewCandidates(records []Record, query []float32, modelName string, now time.Time) []Match {
	ranked := rankCandidates(records, query, modelName, now, 0, nil, nil)
	return ranked[:min(len(ranked), AnswerLimit)]
}

func AnswerCandidates(records []Record, ask danmakutest.Ask, query []float32, modelName string, now time.Time, links []danmakutest.Link, stores []model.MaterialStoreObject) []Match {
	ranked := rankCandidates(records, query, modelName, now, AnswerMinScore, links, func(entry Entry) bool {
		if strings.TrimSpace(entry.Answer) == "" {
			return false
		}
		if AppliesTo(entry, nil, links, stores) {
			return true
		}
		if len(ask.Subjects) == 0 {
			return false
		}
		for _, subject := range ask.Subjects {
			if !AppliesTo(entry, []string{subject}, links, stores) {
				return false
			}
		}
		return true
	})
	for i := range ranked {
		m := &ranked[i]
		if m.Conditions != "" || m.Exceptions != "" {
			m.Status = "待核对"
		} else {
			m.Status = "可参考"
		}
	}
	return ranked
}

func rankCandidates(records []Record, query []float32, modelName string, now time.Time, minimum float64, links []danmakutest.Link, applies func(Entry) bool) []Match {
	var matches []Match
	for _, r := range records {
		e := r.Entry
		if !Active(e, now) || e.ID == "" || r.Model != modelName || r.Space != SpaceVersion || len(r.Vectors) != len(Questions(e)) ||
			(applies != nil && !applies(e)) {
			continue
		}
		var score float64
		scored := false
		for _, vector := range r.Vectors {
			if len(query) == 0 || len(query) != len(vector) {
				continue
			}
			scored = true
			score = max(score, semantic.CosineSimilarity(vector, query))
		}
		if scored && score >= minimum {
			matches = append(matches, Match{ID: e.ID, Revision: e.Revision, Question: e.Question, Answer: e.Answer, Scope: Scope(e, links),
				Conditions: strings.TrimSpace(e.Conditions), Exceptions: strings.TrimSpace(e.Exceptions), Score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Score > matches[j].Score })
	return matches
}

// AppliesTo preserves the scope of saved entries that predate plan-wide QA.
func AppliesTo(entry Entry, subjects []string, links []danmakutest.Link, stores []model.MaterialStoreObject) bool {
	ids, storeNames, unknown := danmakutest.ResolveSubjects(subjects, links, stores)
	return matchesScope(entry, ids, storeNames, unknown)
}

func matchesScope(entry Entry, ids map[int64]bool, stores map[string]bool, unknown bool) bool {
	for _, target := range entry.Targets {
		if target.Kind == "plan" || !unknown && (target.Kind == "product" && ids[target.LinkID] || target.Kind == "store" && stores[target.StoreName]) {
			return true
		}
	}
	return false
}

// Retrieve reuses the already purchased query vectors. Missing/unknown owners
// never make product-specific answers global. Alias hits are deduplicated.
func Retrieve(records []Record, links []danmakutest.Link, stores []model.MaterialStoreObject, asks []danmakutest.Ask, queries []string, vectors [][]float32, modelName string, now time.Time) []Group {
	out := []Group{}
	total, budget := 0, 0
	for _, ask := range asks {
		g := Group{Ask: ask.Text, Matches: []Match{}}
		ids, ss, unknown := danmakutest.ResolveSubjects(ask.Subjects, links, stores)
		qi := []int{}
		for _, q := range danmakutest.Queries("", []danmakutest.Ask{ask}) {
			for i, s := range queries {
				if s == q && i < len(vectors) {
					qi = append(qi, i)
				}
			}
		}
		if len(qi) == 0 && len(vectors) > 0 {
			qi = []int{0}
		}
		for _, r := range records {
			e := r.Entry
			if !Active(e, now) || r.Model != modelName || r.Space != SpaceVersion || len(r.Vectors) != len(Questions(e)) {
				continue
			}
			if !matchesScope(e, ids, ss, unknown) {
				continue
			}
			score := 0.0
			exact := false
			for j, v := range r.Vectors {
				for _, i := range qi {
					if len(v) > 0 && len(v) == len(vectors[i]) {
						s := semantic.CosineSimilarity(v, vectors[i])
						if s > score {
							score = s
						}
					}
				}
				if j < len(Questions(e)) && QuestionKey(Questions(e)[j]) == QuestionKey(ask.Text) {
					exact = true
				}
			}
			// A deliberately conservative candidate gate, not a probability or an
			// automatic direct-answer threshold. Evaluate it on real reviewed cases.
			if !exact && score < AnswerMinScore {
				continue
			}
			m := Match{ID: e.ID, Revision: e.Revision, Question: e.Question, Answer: e.Answer, Scope: Scope(e, links), Conditions: e.Conditions, Exceptions: e.Exceptions, Score: score, Status: "可参考"}
			if strings.TrimSpace(e.Conditions+e.Exceptions) != "" {
				m.Status = "待核对"
				m.Reason = "该问答含适用条件或例外，尚未确认本次满足；不能直接采用为肯定回答"
			}
			g.Matches = append(g.Matches, m)
		}
		sort.SliceStable(g.Matches, func(i, j int) bool { return g.Matches[i].Score > g.Matches[j].Score })
		if len(g.Matches) > 8 {
			g.Matches = g.Matches[:8]
		}
		adopted := 0
		for i := range g.Matches {
			m := &g.Matches[i]
			size := len([]rune(m.Question + m.Answer + m.Conditions + m.Exceptions))
			if m.Status == "可参考" {
				if adopted >= 2 || total >= 6 || budget+size > 6000 {
					m.Status = "未采用"
					m.Reason = "超过本次问答上下文预算，整条保留展示，未截断条件"
				} else {
					total++
					adopted++
					budget += size
				}
			}
		}
		out = append(out, g)
	}
	return out
}
