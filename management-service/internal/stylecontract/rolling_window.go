package stylecontract

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

// RollingWindowChars is deliberately much larger than one generated speech
// unit. A speaker style is a distribution over sustained delivery, not a
// checklist that every 100-200 character segment must satisfy by itself.
const RollingWindowChars = 1000

type RollingHabitDelta struct {
	Kind         string   `json:"kind"`
	Term         string   `json:"term,omitempty"`
	Alternatives []string `json:"alternatives,omitempty"`
	Actual       int      `json:"actual"`
	Target       int      `json:"target"`
	Delta        int      `json:"delta"`
	Direction    string   `json:"direction"`
}

type RollingWindowState struct {
	Version      string              `json:"version"`
	WindowTarget int                 `json:"window_target"`
	WindowChars  int                 `json:"window_chars"`
	Ready        bool                `json:"ready"`
	Strict       bool                `json:"strict"`
	TargetScore  int                 `json:"target_score"`
	Passed       bool                `json:"passed"`
	StyleScore   int                 `json:"style_score"`
	TermScore    int                 `json:"term_score"`
	Runtime      RuntimeEvaluation   `json:"runtime"`
	Needs        []RollingHabitDelta `json:"needs"`
	Overused     []RollingHabitDelta `json:"overused"`
}

// FidelityTargetScore turns the user's high-fidelity setting into an observed
// release target. 100 means the strictest non-copying mode and must sustain at
// least 85/100 in a full rolling window. Lower settings remain advisory.
func FidelityTargetScore(heat int) int {
	heat = clampInt(heat, 0, 100)
	if heat < 90 {
		return 0
	}
	return 80 + (heat-90)/2
}

func partialFidelityTarget(target, chars int) int {
	if target == 0 || chars < 150 {
		return 0
	}
	switch {
	case chars < 400:
		return max(65, target-10)
	case chars < 700:
		return max(65, target-6)
	case chars < RollingWindowChars:
		return max(65, target-3)
	default:
		return target
	}
}

func trailingWindow(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if limit <= 0 || len(runes) <= limit {
		return string(runes)
	}
	return string(runes[len(runes)-limit:])
}

func actualTermCount(kind, text, term string, alternatives []RuntimeHabitTerm) int {
	if kind == "particle" {
		return particleCount(text, term)
	}
	actual := strings.Count(text, term)
	// Keep shorter self/address variants from double-counting a longer variant.
	for _, longer := range alternatives {
		if utf8.RuneCountInString(longer.Text) <= utf8.RuneCountInString(term) || !strings.Contains(longer.Text, term) {
			continue
		}
		actual -= strings.Count(text, longer.Text) * strings.Count(longer.Text, term)
	}
	return max(actual, 0)
}

func rollingTermDeltas(budget RuntimeBudget, text string) (int, []RollingHabitDelta, []RollingHabitDelta) {
	weighted, totalWeight := 0, 0
	needs, overused := []RollingHabitDelta{}, []RollingHabitDelta{}
	for _, group := range budget.HabitGroups {
		alternatives := make([]string, 0, len(group.Terms))
		for _, term := range group.Terms {
			alternatives = append(alternatives, term.Text)
		}
		for _, term := range group.Terms {
			if term.SourceCount <= 0 || term.TargetCount <= 0 {
				continue
			}
			actual := actualTermCount(group.Kind, text, term.Text, group.Terms)
			low, high := max(0, term.TargetCount-1), term.TargetCount+1
			score := rangeScore(actual, low, high)
			weighted += score * term.SourceCount
			totalWeight += term.SourceCount
			if actual < low {
				needs = append(needs, RollingHabitDelta{Kind: group.Kind, Term: term.Text, Alternatives: alternatives, Actual: actual, Target: term.TargetCount, Delta: low - actual, Direction: "under"})
			} else if actual > high {
				overused = append(overused, RollingHabitDelta{Kind: group.Kind, Term: term.Text, Alternatives: alternatives, Actual: actual, Target: term.TargetCount, Delta: actual - high, Direction: "over"})
			}
		}
	}
	if totalWeight == 0 {
		return 100, needs, overused
	}
	sort.SliceStable(needs, func(i, j int) bool { return needs[i].Delta > needs[j].Delta })
	sort.SliceStable(overused, func(i, j int) bool { return overused[i].Delta > overused[j].Delta })
	return weighted / totalWeight, needs, overused
}

// EvaluateRollingWindow scores only the most recent 1000 characters. Before
// the window is full the state is explicitly marked accumulating, so callers
// do not mistake two short segments for a reliable description of the voice.
func EvaluateRollingWindow(profile model.LiveAgentPlanAnchorStyleProfile, source, generated string, heat int) RollingWindowState {
	window := trailingWindow(generated, RollingWindowChars)
	chars := utf8.RuneCountInString(window)
	state := RollingWindowState{Version: "anchor-style-window/v2", WindowTarget: RollingWindowChars, WindowChars: chars, Ready: chars >= RollingWindowChars, Passed: true}
	if !Valid(profile) || chars == 0 {
		return state
	}
	budget := CompileRuntimeBudget(profile, RuntimeOptions{TargetChars: chars, Heat: heat, Scene: RuntimeSceneMainline})
	state.Runtime = EvaluateRuntimeCandidate(budget, source, window)
	state.TermScore, state.Needs, state.Overused = rollingTermDeltas(budget, window)
	state.StyleScore = (state.Runtime.StyleScore*60 + state.TermScore*40) / 100
	state.TargetScore = partialFidelityTarget(FidelityTargetScore(heat), chars)
	state.Strict = state.TargetScore > 0
	state.Passed = !state.Strict || state.StyleScore >= state.TargetScore
	if state.Strict {
		for _, group := range state.Runtime.HabitGroups {
			if group.MinCount > 0 && group.Score < 70 {
				state.Passed = false
			}
		}
		for _, issue := range state.Runtime.Issues {
			switch issue.Code {
			case "ungrounded_self_address", "speaker_identity_drift", "unsupported_dialect_marker", "habit_overuse", "habit_total_overuse":
				state.Passed = false
			}
		}
	}
	return state
}

// StrictFidelityIssues is the release gate for high-fidelity generation. It
// evaluates the already-generated history plus the proposed segment, so only
// the local segment needs repair and accepted earlier speech is never rewritten.
func StrictFidelityIssues(profile model.LiveAgentPlanAnchorStyleProfile, generated string, heat int) (RollingWindowState, []string) {
	state := EvaluateRollingWindow(profile, "", generated, heat)
	if !state.Strict {
		return state, nil
	}
	issues := []string{}
	if state.StyleScore < state.TargetScore {
		issues = append(issues, fmt.Sprintf("最近%d字风格落实率%d/100，当前阶段至少需要%d/100", state.WindowChars, state.StyleScore, state.TargetScore))
	}
	for _, group := range state.Runtime.HabitGroups {
		if group.MinCount > 0 && group.Score < 70 {
			issues = append(issues, fmt.Sprintf("%s分布只有%d/100（实际%d次，参考%d到%d次）", runtimeKindLabel(group.Kind), group.Score, group.ActualCount, group.MinCount, group.MaxCount))
		}
	}
	for _, issue := range state.Runtime.Issues {
		switch issue.Code {
		case "ungrounded_self_address", "speaker_identity_drift", "unsupported_dialect_marker", "habit_overuse", "habit_total_overuse":
			issues = append(issues, issue.Message)
		}
	}
	return state, issues
}

func RenderStrictFidelityRepair(state RollingWindowState, issues []string) string {
	if len(issues) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("高还原档风格验收未通过：")
	b.WriteString(strings.Join(issues, "；"))
	if len(state.Needs) > 0 {
		b.WriteString("。在不改变事实和总长度的前提下，分散补足：")
		for index, item := range state.Needs {
			if index >= 10 {
				break
			}
			if index > 0 {
				b.WriteString("、")
			}
			fmt.Fprintf(&b, "%s%q约%d次", runtimeKindLabel(item.Kind), item.Term, item.Delta)
		}
	}
	if len(state.Overused) > 0 {
		b.WriteString("。同时减少：")
		for index, item := range state.Overused {
			if index >= 6 {
				break
			}
			if index > 0 {
				b.WriteString("、")
			}
			fmt.Fprintf(&b, "%q约%d次", item.Term, item.Delta)
		}
	}
	b.WriteString("。只改当前小段，不照抄样本，不增删或改写商品事实。")
	return b.String()
}

// RenderRollingWindowGuidance converts accumulated delivery into a small debt
// signal for the next segment. It never assigns a hard quota to the next unit.
func RenderRollingWindowGuidance(profile model.LiveAgentPlanAnchorStyleProfile, generated string, nextChars, heat int) string {
	if !Valid(profile) {
		return "未提供可统计的真人主播底层风格；当前小段只执行时间任务和已启用的方案级策略外挂。"
	}
	window := trailingWindow(generated, RollingWindowChars)
	currentChars := utf8.RuneCountInString(window)
	projected := min(RollingWindowChars, max(20, currentChars+max(nextChars, 0)))
	budget := CompileRuntimeBudget(profile, RuntimeOptions{TargetChars: projected, Heat: heat, Scene: RuntimeSceneMainline})
	_, needs, overused := rollingTermDeltas(budget, window)
	var b strings.Builder
	fmt.Fprintf(&b, "主播风格还原强度=%d/100；该参数只控制表达还原度，不改变事实延展权限。主播风格滑动窗口：已累计%d/%d字；这是跨小段统计指导，不是本段逐项打卡。", heat, currentChars, RollingWindowChars)
	if target := FidelityTargetScore(heat); target > 0 {
		fmt.Fprintf(&b, "当前是高还原档，满1000字实际稳定分必须达到%d/100；候选小段会在提交前验收并定向返修。", target)
	}
	if currentChars >= RollingWindowChars {
		b.WriteString("当前按最近1000字滚动统计。")
	}
	if len(needs) > 0 {
		if heat >= 90 {
			b.WriteString("后续必须在合适语境分散补足：")
		} else {
			b.WriteString("后续可在合适语境自然补足：")
		}
		for index, item := range needs {
			limit := 6
			if heat >= 90 {
				limit = 10
			}
			if index >= limit {
				break
			}
			if index > 0 {
				b.WriteString("、")
			}
			fmt.Fprintf(&b, "%s%q约%d次", runtimeKindLabel(item.Kind), item.Term, item.Delta)
		}
		b.WriteString("。")
	}
	if len(overused) > 0 {
		b.WriteString("近期偏多，下一两段先少用：")
		for index, item := range overused {
			if index >= 4 {
				break
			}
			if index > 0 {
				b.WriteString("、")
			}
			fmt.Fprintf(&b, "%q", item.Term)
		}
		b.WriteString("。")
	}
	b.WriteString("只在句意合适时补，不堆在同一句，不改变本段事实和业务任务；数字比价、连续算账、事实回环不属于本窗口。")
	return b.String()
}
