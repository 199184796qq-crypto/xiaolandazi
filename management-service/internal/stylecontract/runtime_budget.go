package stylecontract

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

const RuntimeVersion = "anchor-runtime/v1"

type RuntimeScene string

const (
	RuntimeSceneMainline    RuntimeScene = "mainline"
	RuntimeSceneInteraction RuntimeScene = "interaction"
	RuntimeSceneSerious     RuntimeScene = "serious"
)

type RuntimeOptions struct {
	TargetChars int          `json:"target_chars"`
	Heat        int          `json:"heat"`
	Scene       RuntimeScene `json:"scene"`
}

type RuntimeHabitTerm struct {
	Text        string `json:"text"`
	SourceCount int    `json:"source_count"`
	TargetCount int    `json:"target_count"`
}

// RuntimeHabitBudget groups literal variants so the generator may say one
// natural audience address instead of mechanically stacking every variant.
type RuntimeHabitBudget struct {
	Kind        string             `json:"kind"`
	SourceCount int                `json:"source_count"`
	MinCount    int                `json:"min_count"`
	MaxCount    int                `json:"max_count"`
	Terms       []RuntimeHabitTerm `json:"terms"`
}

// RuntimeBudget is derived entirely from normalized source statistics. It is
// not written by a model and can therefore be reproduced across providers.
type RuntimeBudget struct {
	Version                    string               `json:"version"`
	Protocol                   string               `json:"protocol"`
	Scene                      RuntimeScene         `json:"scene"`
	Heat                       int                  `json:"heat"`
	TargetChars                int                  `json:"target_chars"`
	SourceChars                int                  `json:"source_chars"`
	SourceAverageSentenceChars int                  `json:"source_average_sentence_chars"`
	SentenceCharsMin           int                  `json:"sentence_chars_min"`
	SentenceCharsMax           int                  `json:"sentence_chars_max"`
	TotalHabitMin              int                  `json:"total_habit_min"`
	TotalHabitMax              int                  `json:"total_habit_max"`
	HabitGroups                []RuntimeHabitBudget `json:"habit_groups"`
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func scaledHabitCount(sourceCount, targetChars, sourceChars, heat int) float64 {
	if sourceCount <= 0 || targetChars <= 0 || sourceChars <= 0 || heat <= 0 {
		return 0
	}
	return float64(sourceCount) * float64(targetChars) * float64(heat) / float64(sourceChars*100)
}

// CompileRuntimeBudget converts a saved style contract and a system-owned heat
// setting into bounded lexical and rhythm targets. Heat controls how strongly
// the source habits are reproduced; it is deliberately unrelated to sampling
// temperature.
func CompileRuntimeBudget(profile model.LiveAgentPlanAnchorStyleProfile, options RuntimeOptions) RuntimeBudget {
	options.TargetChars = clampInt(options.TargetChars, 20, 20000)
	options.Heat = clampInt(options.Heat, 0, 100)
	switch options.Scene {
	case RuntimeSceneInteraction, RuntimeSceneSerious, RuntimeSceneMainline:
	default:
		options.Scene = RuntimeSceneMainline
	}
	budget := RuntimeBudget{
		Version:     RuntimeVersion,
		Protocol:    Version,
		Scene:       options.Scene,
		Heat:        options.Heat,
		TargetChars: options.TargetChars,
	}
	if !Valid(profile) {
		return budget
	}
	delivery := profile.Delivery
	budget.SourceChars = delivery.SampleChars
	budget.SourceAverageSentenceChars = delivery.AverageSentenceChars
	if options.Heat > 0 && delivery.AverageSentenceChars > 0 {
		// A high heat narrows cadence towards the sample. A low heat only keeps a
		// loose guardrail and does not pretend to have a learned neutral vector.
		tolerancePct := 60 - options.Heat*35/100
		budget.SentenceCharsMin = max(2, delivery.AverageSentenceChars*(100-tolerancePct)/100)
		budget.SentenceCharsMax = max(budget.SentenceCharsMin, int(math.Ceil(float64(delivery.AverageSentenceChars*(100+tolerancePct))/100)))
	}

	byKind := map[string][]model.LiveAnchorLiteralHabit{}
	for _, habit := range delivery.Habits {
		byKind[habit.Kind] = append(byKind[habit.Kind], habit)
	}
	order := []string{"self_address", "audience_address", "audience_pronoun", "particle", "connector", "catchphrase", "dialect_marker"}
	for _, kind := range order {
		habits := byKind[kind]
		if len(habits) == 0 {
			continue
		}
		sort.SliceStable(habits, func(i, j int) bool {
			if habits[i].Count == habits[j].Count {
				return utf8.RuneCountInString(habits[i].Text) > utf8.RuneCountInString(habits[j].Text)
			}
			return habits[i].Count > habits[j].Count
		})
		group := RuntimeHabitBudget{Kind: kind, Terms: []RuntimeHabitTerm{}}
		for _, habit := range habits {
			// Normalize counted variants into disjoint occurrences. For example,
			// strings.Count("我们家", "我们") also increments the shorter term;
			// summing both raw counts would make the runtime budget overuse self
			// references even though variants are alternatives.
			disjointCount := habit.Count
			for _, longer := range habits {
				if utf8.RuneCountInString(longer.Text) <= utf8.RuneCountInString(habit.Text) {
					continue
				}
				disjointCount -= longer.Count * strings.Count(longer.Text, habit.Text)
			}
			disjointCount = max(disjointCount, 0)
			group.SourceCount += disjointCount
			scaled := scaledHabitCount(disjointCount, options.TargetChars, delivery.SampleChars, options.Heat)
			group.Terms = append(group.Terms, RuntimeHabitTerm{Text: habit.Text, SourceCount: disjointCount, TargetCount: int(math.Round(scaled))})
		}
		scaled := scaledHabitCount(group.SourceCount, options.TargetChars, delivery.SampleChars, options.Heat)
		target := int(math.Round(scaled))
		group.MinCount = max(0, target-1)
		group.MaxCount = target + 1
		if target == 0 && scaled >= 0.35 {
			group.MaxCount = 1
		}
		if options.Heat == 0 {
			group.MinCount, group.MaxCount = 0, 0
		}
		switch options.Scene {
		case RuntimeSceneInteraction:
			group.MinCount = 0
			group.MaxCount = min(group.MaxCount, 1)
		case RuntimeSceneSerious:
			group.MinCount = 0
			if kind == "particle" || kind == "catchphrase" {
				group.MaxCount = 0
			} else {
				group.MaxCount = min(group.MaxCount, 1)
			}
		}
		budget.TotalHabitMin += group.MinCount
		budget.TotalHabitMax += group.MaxCount
		budget.HabitGroups = append(budget.HabitGroups, group)
	}
	if options.Scene == RuntimeSceneInteraction {
		budget.TotalHabitMin = 0
		budget.TotalHabitMax = min(budget.TotalHabitMax, 2)
	}
	if options.Scene == RuntimeSceneSerious {
		budget.TotalHabitMin = 0
		budget.TotalHabitMax = min(budget.TotalHabitMax, 2)
	}
	return budget
}

func runtimeKindLabel(kind string) string {
	labels := map[string]string{
		"self_address":     "主播自称",
		"audience_address": "观众称呼",
		"audience_pronoun": "观众指代",
		"particle":         "句末语气词",
		"connector":        "口头连接词",
		"catchphrase":      "口头禅",
		"dialect_marker":   "方言标记",
	}
	if value := labels[kind]; value != "" {
		return value
	}
	return kind
}

// RenderRuntimeBudget is compact by design: downstream models receive counts
// and alternatives, while the longer rulebook remains the semantic boundary.
func RenderRuntimeBudget(budget RuntimeBudget) string {
	var b strings.Builder
	fmt.Fprintf(&b, "运行预算 · %s；场景=%s；目标约%d字；风格热度=%d/100。\n", budget.Version, budget.Scene, budget.TargetChars, budget.Heat)
	if budget.SentenceCharsMax > 0 {
		fmt.Fprintf(&b, "平均分句长度尽量保持在%d到%d字。\n", budget.SentenceCharsMin, budget.SentenceCharsMax)
	}
	for _, group := range budget.HabitGroups {
		terms := make([]string, 0, len(group.Terms))
		for _, term := range group.Terms {
			terms = append(terms, term.Text)
		}
		fmt.Fprintf(&b, "%s可从[%s]中自然选择，整段合计%d到%d次；变体是替代关系，不要逐个堆叠。\n", runtimeKindLabel(group.Kind), strings.Join(terms, "、"), group.MinCount, group.MaxCount)
	}
	fmt.Fprintf(&b, "以上有证据原词整段合计不超过%d次；短答和严肃场景允许不用，不为达标牺牲事实准确与自然表达。", budget.TotalHabitMax)
	return b.String()
}

type RuntimeHabitEvaluation struct {
	Kind         string   `json:"kind"`
	Alternatives []string `json:"alternatives"`
	ActualCount  int      `json:"actual_count"`
	MinCount     int      `json:"min_count"`
	MaxCount     int      `json:"max_count"`
	Score        int      `json:"score"`
	Distribution int      `json:"distribution_score"`
}

type RuntimeEvaluationIssue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type RuntimeEvaluation struct {
	Passed               bool                     `json:"passed"`
	CandidateChars       int                      `json:"candidate_chars"`
	AverageSentenceChars int                      `json:"average_sentence_chars"`
	StyleScore           int                      `json:"style_score"`
	LexicalScore         int                      `json:"lexical_score"`
	DistributionScore    int                      `json:"distribution_score"`
	RhythmScore          int                      `json:"rhythm_score"`
	CopyContainmentPct   int                      `json:"copy_containment_pct"`
	LongestSharedRunes   int                      `json:"longest_shared_runes"`
	HabitGroups          []RuntimeHabitEvaluation `json:"habit_groups"`
	Issues               []RuntimeEvaluationIssue `json:"issues"`
}

func countAnyNonOverlapping(text string, alternatives []string) int {
	runes := []rune(text)
	words := make([][]rune, 0, len(alternatives))
	for _, alternative := range alternatives {
		if word := []rune(strings.TrimSpace(alternative)); len(word) > 0 {
			words = append(words, word)
		}
	}
	sort.SliceStable(words, func(i, j int) bool { return len(words[i]) > len(words[j]) })
	count := 0
	for index := 0; index < len(runes); {
		matched := 0
		for _, word := range words {
			if index+len(word) > len(runes) {
				continue
			}
			if string(runes[index:index+len(word)]) == string(word) {
				matched = len(word)
				break
			}
		}
		if matched > 0 {
			count++
			index += matched
		} else {
			index++
		}
	}
	return count
}

func compactComparableRunes(text string) []rune {
	result := make([]rune, 0, utf8.RuneCountInString(text))
	for _, r := range text {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		result = append(result, unicode.ToLower(r))
	}
	return result
}

func runeWindows(runes []rune, size int) map[string]struct{} {
	result := map[string]struct{}{}
	if size <= 0 || len(runes) < size {
		return result
	}
	for index := 0; index+size <= len(runes); index++ {
		result[string(runes[index:index+size])] = struct{}{}
	}
	return result
}

func copyContainment(source, candidate string, size int) int {
	sourceRunes, candidateRunes := compactComparableRunes(source), compactComparableRunes(candidate)
	if len(candidateRunes) == 0 {
		return 0
	}
	if len(candidateRunes) < size {
		if strings.Contains(string(sourceRunes), string(candidateRunes)) {
			return 100
		}
		return 0
	}
	sourceWindows := runeWindows(sourceRunes, size)
	candidateWindows := runeWindows(candidateRunes, size)
	matched := 0
	for window := range candidateWindows {
		if _, ok := sourceWindows[window]; ok {
			matched++
		}
	}
	if len(candidateWindows) == 0 {
		return 0
	}
	return matched * 100 / len(candidateWindows)
}

func longestSharedWindow(source, candidate string, ceiling int) int {
	sourceRunes, candidateRunes := compactComparableRunes(source), compactComparableRunes(candidate)
	ceiling = min(ceiling, len(candidateRunes))
	ceiling = min(ceiling, len(sourceRunes))
	for size := ceiling; size >= 12; size-- {
		sourceWindows := runeWindows(sourceRunes, size)
		for index := 0; index+size <= len(candidateRunes); index++ {
			if _, ok := sourceWindows[string(candidateRunes[index:index+size])]; ok {
				return size
			}
		}
	}
	return 0
}

func rangeScore(actual, low, high int) int {
	if low <= actual && actual <= high {
		return 100
	}
	if actual < low {
		if low == 0 {
			return 100
		}
		return clampInt(actual*100/low, 0, 100)
	}
	if actual == 0 {
		return 0
	}
	return clampInt(high*100/actual, 0, 100)
}

// jensenShannonScore compares the relative mix of variants independently from
// their total density. It is symmetric, bounded, and penalizes both replacing
// a dominant source marker and over-concentrating on a minor one.
func jensenShannonScore(source, actual []float64) int {
	if len(source) == 0 || len(source) != len(actual) {
		return 100
	}
	sourceTotal, actualTotal := 0.0, 0.0
	for index := range source {
		sourceTotal += source[index]
		actualTotal += actual[index]
	}
	if sourceTotal == 0 {
		return 100
	}
	if actualTotal == 0 {
		return 0
	}
	divergence := 0.0
	for index := range source {
		p, q := source[index]/sourceTotal, actual[index]/actualTotal
		m := (p + q) / 2
		if p > 0 {
			divergence += 0.5 * p * math.Log2(p/m)
		}
		if q > 0 {
			divergence += 0.5 * q * math.Log2(q/m)
		}
	}
	return clampInt(int(math.Round((1-divergence)*100)), 0, 100)
}

// EvaluateRuntimeCandidate checks objective style budgets and source copying.
// Fact grounding remains a separate hard gate owned by the generation context.
func EvaluateRuntimeCandidate(budget RuntimeBudget, source, candidate string) RuntimeEvaluation {
	evaluation := RuntimeEvaluation{Passed: true, HabitGroups: []RuntimeHabitEvaluation{}, Issues: []RuntimeEvaluationIssue{}}
	addIssue := func(severity, code, message string) {
		evaluation.Issues = append(evaluation.Issues, RuntimeEvaluationIssue{Severity: severity, Code: code, Message: message})
		if severity == "error" {
			evaluation.Passed = false
		}
	}
	evaluation.CandidateChars = utf8.RuneCountInString(strings.TrimSpace(candidate))
	allowedSelf := map[string]bool{}
	allowedDialect := map[string]bool{}
	for _, group := range budget.HabitGroups {
		for _, term := range group.Terms {
			if group.Kind == "self_address" {
				allowedSelf[term.Text] = true
			}
			if group.Kind == "dialect_marker" {
				allowedDialect[term.Text] = true
			}
		}
	}
	seenUngroundedSelf := map[string]bool{}
	for _, term := range liveSelfAddress.FindAllString(candidate, -1) {
		if allowedSelf[term] || seenUngroundedSelf[term] {
			continue
		}
		seenUngroundedSelf[term] = true
		addIssue("error", "ungrounded_self_address", fmt.Sprintf("候选使用了样本没有的主播方自指%q，应沿用有证据原词或省略主语", term))
	}
	if budget.Heat >= 90 && len(allowedSelf) > 0 {
		identityDrift := regexp.MustCompile(`(?:^|[。！？!?；;\n\r])\s*(他们家|他家|她家)`)
		if match := identityDrift.FindStringSubmatch(candidate); len(match) > 1 {
			addIssue("error", "speaker_identity_drift", fmt.Sprintf("主播主体从第一方漂成了%q；若仍在讲当前商家，应改回样本中的第一方自指", match[1]))
		}
	}
	if budget.Heat >= 90 {
		seenDialect := map[string]bool{}
		for _, term := range liveDialectMarker.FindAllString(candidate, -1) {
			if allowedDialect[term] || seenDialect[term] {
				continue
			}
			seenDialect[term] = true
			addIssue("error", "unsupported_dialect_marker", fmt.Sprintf("候选加入了样本没有的方言词%q；高还原档只能使用样本有证据的地域原词", term))
		}
	}
	sentenceCount := 0
	for _, sentence := range sentenceBreak.Split(candidate, -1) {
		if strings.TrimSpace(sentence) != "" {
			sentenceCount++
		}
	}
	if sentenceCount > 0 {
		evaluation.AverageSentenceChars = evaluation.CandidateChars / sentenceCount
	}
	if budget.TargetChars > 0 {
		low, high := budget.TargetChars*75/100, budget.TargetChars*125/100
		if evaluation.CandidateChars < low || evaluation.CandidateChars > high {
			addIssue("error", "length_out_of_range", fmt.Sprintf("候选%d字，不在目标%d字的允许区间%d到%d字", evaluation.CandidateChars, budget.TargetChars, low, high))
		}
	}

	weightedScore, distributionWeighted, totalWeight, totalActual := 0, 0, 0, 0
	for _, group := range budget.HabitGroups {
		alternatives := make([]string, 0, len(group.Terms))
		sourceMix := make([]float64, 0, len(group.Terms))
		actualMix := make([]float64, 0, len(group.Terms))
		actual := 0
		for _, term := range group.Terms {
			alternatives = append(alternatives, term.Text)
			sourceMix = append(sourceMix, float64(term.SourceCount))
			termActual := actualTermCount(group.Kind, candidate, term.Text, group.Terms)
			actualMix = append(actualMix, float64(termActual))
			actual += termActual
		}
		score := rangeScore(actual, group.MinCount, group.MaxCount)
		distribution := jensenShannonScore(sourceMix, actualMix)
		weight := max(group.SourceCount, 1)
		weightedScore += score * weight
		distributionWeighted += distribution * weight
		totalWeight += weight
		totalActual += actual
		evaluation.HabitGroups = append(evaluation.HabitGroups, RuntimeHabitEvaluation{Kind: group.Kind, Alternatives: alternatives, ActualCount: actual, MinCount: group.MinCount, MaxCount: group.MaxCount, Score: score, Distribution: distribution})
		if actual > group.MaxCount {
			addIssue("error", "habit_overuse", fmt.Sprintf("%s使用%d次，超过预算上限%d次", runtimeKindLabel(group.Kind), actual, group.MaxCount))
		} else if actual < group.MinCount {
			addIssue("warning", "habit_underuse", fmt.Sprintf("%s使用%d次，低于参考下限%d次", runtimeKindLabel(group.Kind), actual, group.MinCount))
		}
	}
	if totalWeight == 0 {
		evaluation.LexicalScore = 100
		evaluation.DistributionScore = 100
	} else {
		densityScore := weightedScore / totalWeight
		evaluation.DistributionScore = distributionWeighted / totalWeight
		evaluation.LexicalScore = (densityScore*70 + evaluation.DistributionScore*30) / 100
	}
	if totalActual > budget.TotalHabitMax {
		addIssue("error", "habit_total_overuse", fmt.Sprintf("有证据原词合计%d次，超过整段上限%d次", totalActual, budget.TotalHabitMax))
	}
	if budget.SentenceCharsMax == 0 {
		evaluation.RhythmScore = 100
	} else {
		evaluation.RhythmScore = rangeScore(evaluation.AverageSentenceChars, budget.SentenceCharsMin, budget.SentenceCharsMax)
		if evaluation.AverageSentenceChars < budget.SentenceCharsMin || evaluation.AverageSentenceChars > budget.SentenceCharsMax {
			addIssue("warning", "rhythm_out_of_range", fmt.Sprintf("平均分句%d字，参考区间为%d到%d字", evaluation.AverageSentenceChars, budget.SentenceCharsMin, budget.SentenceCharsMax))
		}
	}
	evaluation.StyleScore = (evaluation.LexicalScore*70 + evaluation.RhythmScore*30) / 100
	if evaluation.StyleScore < 65 {
		addIssue("error", "style_below_threshold", fmt.Sprintf("客观风格得分%d，低于离线门槛65", evaluation.StyleScore))
	}
	evaluation.CopyContainmentPct = copyContainment(source, candidate, 12)
	evaluation.LongestSharedRunes = longestSharedWindow(source, candidate, 96)
	if evaluation.CopyContainmentPct >= 55 || evaluation.LongestSharedRunes >= 80 {
		addIssue("error", "source_copy_risk", fmt.Sprintf("候选与样本的12字片段覆盖%d%%，最长连续复用%d字", evaluation.CopyContainmentPct, evaluation.LongestSharedRunes))
	} else if evaluation.CopyContainmentPct >= 35 || evaluation.LongestSharedRunes >= 36 {
		addIssue("warning", "source_copy_warning", fmt.Sprintf("候选与样本存在较多相同表达：覆盖%d%%，最长连续%d字", evaluation.CopyContainmentPct, evaluation.LongestSharedRunes))
	}
	return evaluation
}
