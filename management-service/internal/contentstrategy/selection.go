package contentstrategy

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"livecompanion/management/internal/interactionpolicy"
)

type SelectionRequest struct {
	Mode, AllowedFacts, Question, PurposeID, CurrentTask string
	Recent                                               []string
	ActiveBenefit                                        bool
	ActiveFactIDs                                        map[string]bool
}
type Selection struct {
	AnswerOutline *AnswerOutline `json:"answer_outline,omitempty"`
	PurposeID     string         `json:"purpose_id,omitempty"`
	Required      []Row          `json:"required_tasks"`
	Optional      []Row          `json:"optional_tasks"`
	Facts         []Fact         `json:"full_fact_dependencies"`
	Reason        string         `json:"reason"`
}

// Content-only planning shared by live interaction and plan-editor tests.
// Identity, source dependencies and execution state remain program-owned.
type AnswerOutline struct {
	Purpose string   `json:"purpose"`
	Angle   string   `json:"angle"`
	Aspects []string `json:"aspects"`
	Avoid   []string `json:"avoid"`
}

func ValidateRowPolicy(row Row, required bool) error {
	if required && (row.Priority == 0 || row.Role == "" || len(row.Conditions) == 0) {
		return fmt.Errorf("角度缺少优先级、任务类型或采用条件")
	}
	if row.Priority < 0 || row.Priority > 100 || (row.Role != "" && row.Role != "required" && row.Role != "optional") {
		return fmt.Errorf("角度优先级或任务类型无效")
	}
	if len(row.Conditions) > 8 {
		return fmt.Errorf("采用条件过多")
	}
	for _, c := range row.Conditions {
		switch c.Type {
		case "always", "question", "mainline", "active_benefit":
			if c.Value != "" {
				return fmt.Errorf("条件不允许附加值：%s", c.Type)
			}
		case "purpose_match", "task_contains":
			if !validText(c.Value, 80) {
				return fmt.Errorf("条件值缺失：%s", c.Type)
			}
		default:
			return fmt.Errorf("未知采用条件：%s", c.Type)
		}
	}
	return nil
}

func rowAllowed(row Row, request SelectionRequest) bool {
	for _, c := range row.Conditions {
		switch c.Type {
		case "question":
			if request.Mode != "互动" {
				return false
			}
		case "mainline":
			if request.Mode != "主线" {
				return false
			}
		case "active_benefit":
			if !request.ActiveBenefit {
				return false
			}
			valid := false
			for _, id := range row.FactIDs {
				if request.ActiveFactIDs[id] {
					valid = true
				}
			}
			if !valid {
				return false
			}
		case "purpose_match":
			if request.PurposeID != c.Value {
				return false
			}
		case "task_contains":
			if !strings.Contains(request.CurrentTask, c.Value) {
				return false
			}
		case "always":
		default:
			return false
		}
	}
	return true
}

// Lexical scoring is only a conservative task filter. Ambiguous viewer
// purpose selection is done by the persistent resolver, never this scorer.
func overlap(a, b string) int {
	score := 0
	seen := map[string]bool{}
	runes := []rune(a)
	for i := 0; i+1 < len(runes); i++ {
		pair := string(runes[i : i+2])
		if !seen[pair] && strings.Contains(b, pair) {
			score++
			seen[pair] = true
		}
	}
	return score
}
func (s *Snapshot) Select(request SelectionRequest) Selection {
	result := Selection{PurposeID: request.PurposeID, Required: []Row{}, Optional: []Row{}, Facts: []Fact{}, Reason: "program conditions, complete fact dependencies, priority and recent use"}
	if s == nil || !s.Enabled {
		return result
	}
	allowed := map[string]bool{}
	for _, f := range s.Facts {
		if strings.Contains(request.AllowedFacts, f.Text) && (!strings.HasPrefix(strings.TrimSpace(f.Text), "- [有效福利]") || request.ActiveFactIDs[f.ID]) {
			allowed[f.ID] = true
		}
	}
	purposes := map[string]Purpose{}
	for _, p := range s.Purposes {
		purposes[p.ID] = p
	}
	if request.Mode == "主线" && request.PurposeID == "" {
		best := 0
		for _, row := range s.Rows {
			ok := true
			for _, id := range row.FactIDs {
				if !allowed[id] {
					ok = false
				}
			}
			if !ok {
				continue
			}
			p := purposes[row.PurposeID]
			if score := overlap(request.CurrentTask, p.Name+row.Angle+row.Task+row.When); score > best {
				best = score
				request.PurposeID = row.PurposeID
			}
		}
		result.PurposeID = request.PurposeID
	}
	type ranked struct {
		row   Row
		score int
	}
	candidates := []ranked{}
	for _, row := range s.Rows {
		if !rowAllowed(row, request) {
			continue
		}
		ok := true
		for _, id := range row.FactIDs {
			if !allowed[id] {
				ok = false
			}
		}
		if !ok {
			continue
		}
		p := purposes[row.PurposeID]
		if request.Mode == "互动" && request.PurposeID != "" && row.PurposeID != request.PurposeID {
			continue
		}
		query := request.CurrentTask
		if request.Mode == "互动" {
			query = request.Question
		}
		relevance := overlap(query, p.Name+" "+p.Concern+" "+row.Angle+" "+row.Task+" "+row.When)
		if query != "" && request.PurposeID == "" && relevance == 0 {
			continue
		}
		priority := row.Priority
		if priority == 0 {
			priority = 50
		}
		score := priority + min(30, relevance*3)
		for _, recent := range request.Recent {
			if strings.Contains(recent, row.Angle) || overlap(row.Task, recent) >= 4 {
				score -= 60
			}
		}
		candidates = append(candidates, ranked{row, score})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	for _, candidate := range candidates {
		if candidate.row.Role == "required" && len(result.Required) < 2 {
			result.Required = append(result.Required, candidate.row)
		}
	}
	for _, candidate := range candidates {
		if candidate.row.Role != "required" && len(result.Optional) < 1 {
			result.Optional = append(result.Optional, candidate.row)
		}
	}
	used := map[string]bool{}
	for _, row := range append(append([]Row{}, result.Required...), result.Optional...) {
		for _, id := range row.FactIDs {
			used[id] = true
		}
	}
	for _, f := range s.Facts {
		if used[f.ID] {
			result.Facts = append(result.Facts, f)
		}
	}
	return result
}

func (s *Snapshot) RenderSelection(request SelectionRequest) (string, Selection, error) {
	selected := s.Select(request)
	if len(selected.Required)+len(selected.Optional) == 0 {
		return "", selected, nil
	}
	data, _ := json.Marshal(map[string]any{"使用路径": request.Mode, "观众问题": request.Question, "当前轮盘任务": request.CurrentTask, "内容计划": selected})
	raw, err := interactionpolicy.Read("fact-purpose-selected-generation.md")
	if err != nil {
		return "", selected, err
	}
	return strings.ReplaceAll(string(raw), "{{STRATEGY_DATA}}", string(data)), selected, nil
}

// Only reviewed benefit records inside the published facts can authorize an
// offer. Every declared date must parse and contain the current time.
func ActiveBenefitFacts(facts []Fact, now time.Time) map[string]bool {
	active := map[string]bool{}
	for _, fact := range facts {
		if !strings.HasPrefix(fact.Text, "- [有效福利]") {
			continue
		}
		valid := true
		for _, field := range strings.Split(strings.Split(fact.Text, "\n")[0], "；") {
			for _, key := range []string{"开始=", "结束="} {
				if strings.HasPrefix(field, key) {
					at, err := time.Parse(time.RFC3339, strings.TrimSpace(strings.TrimPrefix(field, key)))
					if err != nil || (key == "开始=" && now.Before(at)) || (key == "结束=" && !now.Before(at)) {
						valid = false
					}
				}
			}
		}
		if valid {
			active[fact.ID] = true
		}
	}
	return active
}
