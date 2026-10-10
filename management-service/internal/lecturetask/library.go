// Package lecturetask holds plan assets. Library contents are supplied only to
// the selected lecture request, never appended to the shared business prompt.
package lecturetask

import (
	"encoding/json"
	"errors"
	"strings"
)

type Fact struct {
	Storage  string `json:"storage,omitempty"`
	ID       int64  `json:"id"`
	Version  int64  `json:"version"`
	Category string `json:"category"`
	Name     string `json:"name"`
	Text     string `json:"text"`
	Quote    string `json:"quote"`
}
type Angle struct {
	Title string `json:"title"`
	Goal  string `json:"goal"`
}
type Task struct {
	Subject       string   `json:"subject,omitempty"`
	RootFact      *Fact    `json:"root_fact,omitempty"`
	QuestionTerms []string `json:"question_terms"`
	ID            int64    `json:"id"`
	Revision      int64    `json:"revision"`
	Title         string   `json:"title"`
	Purpose       string   `json:"purpose"`
	Angles        []Angle  `json:"angles"`
	Facts         []Fact   `json:"facts"`
	Links         []string `json:"links"`
	Enabled       bool     `json:"enabled"`
	Valid         bool     `json:"valid"`
	Source        string   `json:"source,omitempty"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
}
type Policy struct {
	Enabled   bool   `json:"enabled"`
	Frequency string `json:"frequency"`
}

func DefaultPolicy() Policy { return Policy{true, "normal"} }
func (p Policy) Validate() error {
	if p.Frequency != "normal" && p.Frequency != "less" && p.Frequency != "more" {
		return errors.New("讲解频率无效")
	}
	return nil
}

type Library struct {
	Tasks       []Task `json:"tasks"`
	Policy      Policy `json:"policy"`
	Fingerprint string `json:"fingerprint"`
}

func (t Task) Applicable(link string) bool { return len(t.Links) == 0 || contains(t.Links, link) }
func contains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}
func (t Task) Evidence() string {
	var lines []string
	if t.Subject != "" {
		lines = append(lines, "资料对象："+t.Subject)
	}
	if t.RootFact != nil {
		lines = append(lines, "与商品的已确认关系："+t.RootFact.Text)
	}
	for _, f := range t.Facts {
		lines = append(lines, f.Name+"："+f.Text)
	}
	return strings.Join(lines, "\n")
}
func Decode(value any) (Library, error) {
	var lib Library
	raw, err := json.Marshal(value)
	if err != nil {
		return lib, err
	}
	err = json.Unmarshal(raw, &lib)
	if err == nil {
		err = lib.Policy.Validate()
	}
	return lib, err
}

func QuestionEvidence(lib Library, question string) string {
	chosen := -1
	best := 0
	for i, t := range lib.Tasks {
		if !t.Valid {
			continue
		}
		allowed := len(t.Links) == 0
		for _, link := range t.Links {
			if strings.Contains(question, link+"号") {
				allowed = true
			}
		}
		if !allowed {
			continue
		}
		score := 0
		for _, term := range t.QuestionTerms {
			if len([]rune(term)) >= 2 && strings.Contains(question, term) {
				score += len([]rune(term))
			}
		}
		if score > best {
			chosen, best = i, score
		}
	}
	if chosen < 0 {
		return ""
	}
	// Ordinary question answering receives level-one product relations only.
	// Level-two evidence is loaded by an explicitly selected lecture task.
	root := lib.Tasks[chosen].RootFact
	if root == nil {
		return ""
	}
	text := root.Name + "：" + root.Text
	if len([]rune(text)) > 2400 {
		return ""
	}
	return "\n# 当前问题相关的商品关联事实\n" + text
}
