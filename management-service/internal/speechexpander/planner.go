// Package speechexpander turns a requested duration into a deterministic
// virtual-clock plan. It never invents copy or business facts; an LLM renders
// each planned unit against the authorized fact context.
package speechexpander

import (
	"hash/fnv"
	"math"
	"strings"

	"livecompanion/management/internal/model"
)

const defaultCharsPerMinute = 250
const targetStepSeconds = 60

type Input struct {
	DurationMinutes int
	TargetChars     int
	VariantCount    int
	FactKeys        []string
	BenefitKeys     []string
	LinkKeys        []string
}

type scenario struct {
	name                string
	heat                string
	online              int
	entries             int
	chats               int
	questionPressure    float64
	turnover            float64
	conversion          float64
	stageOffset         int
	expressionMoveShift int
}

var fixedScenarios = []scenario{
	{name: "steady_room", heat: "WARM", online: 36, entries: 4, chats: 2, questionPressure: 0.20, turnover: 0.18},
	{name: "newcomer_wave", heat: "WARM", online: 82, entries: 16, chats: 4, questionPressure: 0.25, turnover: 0.62, stageOffset: 1, expressionMoveShift: 1},
	{name: "quiet_room", heat: "COLD", online: 5, entries: 1, chats: 0, questionPressure: 0.05, turnover: 0.08, stageOffset: 2, expressionMoveShift: 2},
	{name: "active_questions", heat: "HOT", online: 128, entries: 9, chats: 18, questionPressure: 0.78, turnover: 0.31, stageOffset: 3, expressionMoveShift: 3},
	{name: "conversion_momentum", heat: "HOT", online: 67, entries: 7, chats: 8, questionPressure: 0.36, turnover: 0.22, conversion: 0.75, stageOffset: 4, expressionMoveShift: 4},
}

type stage struct {
	key  string
	goal string
}

var cycleStages = []stage{
	{key: "reentry", goal: "照顾新进入者，用当前正式事实自然接住本轮，不声称看到了具体某人进场"},
	{key: "fact_direct", goal: "直接讲清本单元选择的正式事实，不做摘要式罗列"},
	{key: "fact_unfold", goal: "把同一事实拆成口语短句并解释原意，不推导新的原因或效果"},
	{key: "fact_restate", goal: "隔开后换序重述已讲事实，让重复承担提醒作用而不是机械换词"},
	{key: "benefit_link", goal: "只在已有有效福利或正式链接时自然承接，否则继续正式事实"},
	{key: "recap_confirm", goal: "用主播式短确认和回顾收住当前重点，不制造购买或观众状态"},
	{key: "cycle_bridge", goal: "自然结束本轮并接到下一轮，允许提出无需假装已有回应的开放式互动"},
}

var expressionMoves = []string{
	"直述重点",
	"拆成短句",
	"问后自答",
	"换序重述",
	"短句确认",
	"回顾承接",
	"先收束再补一句",
}

func cleanKeys(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func keysForStep(values []string, stepIndex, offset, count int) []string {
	if len(values) == 0 || count <= 0 {
		return nil
	}
	if count > len(values) {
		count = len(values)
	}
	result := make([]string, 0, count)
	for i := 0; i < count; i++ {
		result = append(result, values[(stepIndex+offset+i)%len(values)])
	}
	return result
}

func BuildFixedPlans(input Input) []model.LiveSpeechExpansionPlan {
	if input.DurationMinutes <= 0 {
		input.DurationMinutes = 1
	}
	if input.TargetChars <= 0 {
		input.TargetChars = input.DurationMinutes * defaultCharsPerMinute
	}
	if input.VariantCount <= 0 {
		input.VariantCount = 1
	}
	if input.VariantCount > 5 {
		input.VariantCount = 5
	}
	factKeys := cleanKeys(input.FactKeys)
	benefitKeys := cleanKeys(input.BenefitKeys)
	linkKeys := cleanKeys(input.LinkKeys)
	durationSeconds := input.DurationMinutes * 60
	stepCount := int(math.Ceil(float64(durationSeconds) / targetStepSeconds))
	if stepCount < 1 {
		stepCount = 1
	}
	keys := []string{"A", "B", "C", "D", "E"}
	plans := make([]model.LiveSpeechExpansionPlan, 0, input.VariantCount)
	for variant := 0; variant < input.VariantCount; variant++ {
		sim := fixedScenarios[variant%len(fixedScenarios)]
		plan := model.LiveSpeechExpansionPlan{
			Version: model.LiveSpeechExpansionVersion, Mode: "fixed_simulation", VariantKey: keys[variant],
			DurationSeconds: durationSeconds, TargetChars: input.TargetChars,
			Steps: make([]model.LiveSpeechExpansionStep, 0, stepCount),
		}
		allocatedChars := 0
		for index := 0; index < stepCount; index++ {
			start := index * targetStepSeconds
			end := start + targetStepSeconds
			if end > durationSeconds {
				end = durationSeconds
			}
			chars := input.TargetChars / stepCount
			if index < input.TargetChars%stepCount {
				chars++
			}
			allocatedChars += chars
			stageIndex := (index + sim.stageOffset) % len(cycleStages)
			currentStage := cycleStages[stageIndex]
			// The final time unit must close the current round. Letting the fixed
			// cycle land on reentry here caused "刚进来的朋友" to restart the
			// whole pitch immediately before the requested ending.
			if index == stepCount-1 && stepCount > 1 {
				currentStage = cycleStages[5] // recap_confirm
			}
			room := model.LiveSpeechExpansionRoomState{
				Scenario: sim.name, Heat: sim.heat, OnlineCount: sim.online + index*(sim.entries-sim.chats)/4,
				EntriesPerMinute: sim.entries, ChatsPerMinute: sim.chats, QuestionPressure: sim.questionPressure,
				AudienceTurnover5m: sim.turnover, ConversionSignal: sim.conversion,
			}
			if room.OnlineCount < 0 {
				room.OnlineCount = 0
			}
			moveIndex := (index + sim.expressionMoveShift) % len(expressionMoves)
			secondMove := (moveIndex + 2 + variant) % len(expressionMoves)
			stepBenefitKeys := []string(nil)
			stepLinkKeys := []string(nil)
			// Promotions and links are support material, not a compulsory footer
			// for every minute. Put the benefit at the dedicated stage and allow
			// a link only there or during a recap/closing unit.
			if currentStage.key == "benefit_link" {
				stepBenefitKeys = keysForStep(benefitKeys, index, variant, 1)
			}
			if currentStage.key == "benefit_link" || currentStage.key == "recap_confirm" {
				stepLinkKeys = keysForStep(linkKeys, index, variant, 1)
			}
			interactionOpportunity := currentStage.key == "cycle_bridge" || (sim.questionPressure >= 0.6 && index%3 == 2)
			// A finite preview or pre-generated clip cannot open a fresh audience
			// loop in its last two units and then immediately close before any
			// response can be observed. Live Core interactions remain independent.
			if index >= stepCount-2 {
				interactionOpportunity = false
			}
			plan.Steps = append(plan.Steps, model.LiveSpeechExpansionStep{
				Index: index + 1, StartSecond: start, EndSecond: end,
				CycleIndex: index/len(cycleStages) + 1, Stage: currentStage.key, Goal: currentStage.goal,
				TargetChars: chars, ExpressionMoves: []string{expressionMoves[moveIndex], expressionMoves[secondMove]},
				// One primary fact per time unit. Benefits and links may support it,
				// but a renderer must not receive two co-equal facts and turn every
				// unit into a miniature complete sales script.
				FactKeys:    keysForStep(factKeys, index, variant, 1),
				BenefitKeys: stepBenefitKeys, LinkKeys: stepLinkKeys,
				InteractionOpportunity: interactionOpportunity, Room: room,
			})
		}
		if allocatedChars == input.TargetChars {
			plans = append(plans, plan)
		}
	}
	return plans
}

// AssignStyleOverlays places enabled expression capabilities on concrete
// virtual-clock units. It does not change content stages, fact selection or
// interaction ownership. Occasional capabilities are spread deterministically
// and prefer unoccupied units so several humanization effects do not pile up in
// one sentence.
func AssignStyleOverlays(plans []model.LiveSpeechExpansionPlan, profile model.LiveAgentPlanStyleOverlayProfile) []model.LiveSpeechExpansionPlan {
	for planIndex := range plans {
		plan := &plans[planIndex]
		if len(plan.Steps) == 0 {
			continue
		}
		occupied := make([]bool, len(plan.Steps))
		for _, item := range profile.Items {
			if !item.Enabled {
				continue
			}
			label := strings.TrimSpace(item.Rule.Label)
			if label == "" {
				continue
			}
			if item.Rule.Application == "always" {
				for stepIndex := range plan.Steps {
					plan.Steps[stepIndex].StyleCapabilities = appendUnique(plan.Steps[stepIndex].StyleCapabilities, label)
				}
				continue
			}
			maxCount := int(math.Ceil(float64(plan.TargetChars*item.Rule.MainlineMaxPer1000Chars) / 1000))
			minCount := int(math.Floor(float64(plan.TargetChars*item.Rule.MainlineMinPer1000Chars) / 1000))
			if maxCount > len(plan.Steps) {
				maxCount = len(plan.Steps)
			}
			if minCount > maxCount {
				minCount = maxCount
			}
			count := minCount
			if maxCount > minCount {
				count += int(math.Round(float64(maxCount-minCount) * float64(item.Rule.Strength) / 100))
			}
			if count == 0 && maxCount > 0 && item.Rule.Strength > 0 {
				count = 1
			}
			if count <= 0 {
				continue
			}
			offset := stableOffset(plan.VariantKey, item.ID, label) % len(plan.Steps)
			gap := len(plan.Steps) / count
			if gap < 1 {
				gap = 1
			}
			chosen := map[int]bool{}
			for occurrence := 0; occurrence < count; occurrence++ {
				candidate := (offset + occurrence*gap) % len(plan.Steps)
				stepIndex := findAvailableStep(candidate, occupied, chosen)
				if stepIndex < 0 {
					break
				}
				plan.Steps[stepIndex].StyleCapabilities = appendUnique(plan.Steps[stepIndex].StyleCapabilities, label)
				occupied[stepIndex] = true
				chosen[stepIndex] = true
			}
		}
	}
	return plans
}

func stableOffset(parts ...string) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.Join(parts, "\x00")))
	return int(hash.Sum32() & 0x7fffffff)
}

func findAvailableStep(start int, occupied []bool, chosen map[int]bool) int {
	for offset := 0; offset < len(occupied); offset++ {
		index := (start + offset) % len(occupied)
		if !occupied[index] && !chosen[index] && !hasOccupiedNeighbor(index, occupied) {
			return index
		}
	}
	for offset := 0; offset < len(occupied); offset++ {
		index := (start + offset) % len(occupied)
		if !occupied[index] && !chosen[index] {
			return index
		}
	}
	// Only stack capabilities when every time unit is already occupied.
	for offset := 0; offset < len(occupied); offset++ {
		index := (start + offset) % len(occupied)
		if !chosen[index] {
			return index
		}
	}
	return -1
}

func hasOccupiedNeighbor(index int, occupied []bool) bool {
	return (index > 0 && occupied[index-1]) || (index+1 < len(occupied) && occupied[index+1])
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
