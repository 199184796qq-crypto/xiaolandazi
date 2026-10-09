package contentpools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Edits cannot change a task's pool, applicable products or factual references.
// Added angles inherit the explicitly selected selling point's references.
type TaskEdit struct {
	ID             string `json:"id,omitempty"`
	Point          string `json:"selling_point"`
	Angle          string `json:"angle"`
	TargetAudience string `json:"target_audience,omitempty"`
	PainPoint      string `json:"pain_point,omitempty"`
}

type PoolEdit struct {
	ExpectedSetID int64      `json:"expected_set_id"`
	PoolKey       string     `json:"pool_key"`
	TaskIDs       []string   `json:"task_ids"`
	Tasks         []TaskEdit `json:"tasks"`
}

func ApplyEdit(original Set, edit PoolEdit) (Set, error) {
	if edit.ExpectedSetID != original.ID || len(edit.TaskIDs) == 0 || len(edit.TaskIDs) > 500 || len(edit.Tasks) > 500 || edit.Tasks == nil {
		return Set{}, errors.New("内容版本或编辑范围无效，请刷新后重试")
	}
	raw, err := json.Marshal(original)
	if err != nil {
		return Set{}, err
	}
	var next Set
	if err = json.Unmarshal(raw, &next); err != nil {
		return Set{}, err
	}
	poolIndex := -1
	for i, p := range next.Pools {
		if p.Key == edit.PoolKey {
			poolIndex = i
			break
		}
	}
	if poolIndex < 0 {
		return Set{}, errors.New("所选内容池不存在")
	}
	p := &next.Pools[poolIndex]
	selected := map[string]Task{}
	for _, id := range edit.TaskIDs {
		if id == "" {
			return Set{}, errors.New("内容编号不能为空")
		}
		if _, exists := selected[id]; exists {
			return Set{}, errors.New("编辑范围包含重复内容")
		}
		found := false
		for _, t := range p.Tasks {
			if t.ID == id {
				selected[id], found = t, true
				break
			}
		}
		if !found {
			return Set{}, errors.New("编辑内容不属于所选内容池")
		}
	}
	refs := []string{}
	seenRefs := map[string]bool{}
	for _, t := range p.Tasks {
		if _, ok := selected[t.ID]; ok {
			for _, id := range t.Sources {
				if !seenRefs[id] {
					refs, seenRefs[id] = append(refs, id), true
				}
			}
		}
	}
	replacements := []Task{}
	seen := map[string]bool{}
	for i, input := range edit.Tasks {
		input.Point, input.Angle = strings.TrimSpace(input.Point), strings.TrimSpace(input.Angle)
		input.PainPoint, input.TargetAudience = strings.TrimSpace(input.PainPoint), strings.TrimSpace(input.TargetAudience)
		if input.Point == "" || input.Angle == "" || utf8.RuneCountInString(input.Point) > 80 || utf8.RuneCountInString(input.Angle) > 1000 || utf8.RuneCountInString(input.PainPoint) > 500 || utf8.RuneCountInString(input.TargetAudience) > 500 {
			return Set{}, errors.New("卖点和内容不能为空，且不能超过编辑长度上限")
		}
		t := Task{ID: input.ID, Point: input.Point, Angle: input.Angle, TargetAudience: input.TargetAudience, PainPoint: input.PainPoint}
		if input.ID != "" {
			old, ok := selected[input.ID]
			if !ok || seen[input.ID] {
				return Set{}, errors.New("内容编号不在本次编辑范围或重复")
			}
			t.Sources = append([]string{}, old.Sources...)
		} else {
			t.ID = "manual-" + Digest(fmt.Sprintf("%d/%s/%d/%s", original.ID, p.Key, i, Digest(input)))[:32]
			t.Sources = append([]string{}, refs...)
		}
		seen[t.ID] = true
		replacements = append(replacements, t)
	}
	tasks := []Task{}
	inserted := false
	for _, t := range p.Tasks {
		if _, ok := selected[t.ID]; ok {
			if !inserted {
				tasks = append(tasks, replacements...)
				inserted = true
			}
		} else {
			tasks = append(tasks, t)
		}
	}
	p.Tasks, p.Reused = tasks, false
	if err = ValidateSet(next); err != nil {
		return Set{}, err
	}
	return next, nil
}
