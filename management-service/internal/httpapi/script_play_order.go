package httpapi

import (
	"math/rand"
	"sort"
	"strings"

	"livecompanion/management/internal/model"
)

// The shortest warmed opening remains first, followed by the rest of its tag.
// Moving only that piece to the front must not split a tag around other tags.
func scriptInitialPlaybackOrder(v model.LiveAgentPlanVersion, units []volcengineScriptUnit) []int {
	groups := scriptPlayGroups(v, units)
	var opening, rest []int
	for _, group := range groups {
		first := false
		for _, i := range group {
			if i == 0 {
				first = true
			}
		}
		if first {
			opening = append(opening, 0)
			for _, i := range group {
				if i != 0 {
					opening = append(opening, i)
				}
			}
		} else {
			rest = append(rest, group...)
		}
	}
	return append(opening, rest...)
}

// scriptPlayGroups 把供料单元按话术标签分组，返回每组的单元下标。
//
// 分组依据是标签内的文本指纹：单元 Key 由文本算出，能唯一对回原标签。
// 老方案（单框或非 v2）没有分组信息，整份正文视为一个组。
func scriptPlayGroups(v model.LiveAgentPlanVersion, units []volcengineScriptUnit) [][]int {
	if len(units) == 0 {
		return nil
	}
	script, ok := recordingScriptFromContext(v.GenerationContext)
	if !ok || script.Version != 2 {
		all := make([]int, len(units))
		for i := range all {
			all[i] = i
		}
		return [][]int{all}
	}
	index := make(map[string][]int, len(units))
	byPool := map[string]int{}
	for i, unit := range units {
		index[unit.Key] = append(index[unit.Key], i)
		if unit.Pool != "" {
			byPool[unit.Pool] = i
		}
	}
	var groups [][]int
	used := make([]bool, len(units))
	for tabIndex, tab := range script.Tabs {
		var group []int
		for _, entry := range tab.Entries {
			text := strings.TrimSpace(entry.Text)
			if text == "" {
				continue
			}
			key := scriptFirstParagraphKey(v, text)
			pos, ok := byPool[scriptEntryPoolID(tab, tabIndex, entry.EntryID)]
			if !ok {
				for _, candidate := range index[key] {
					if !used[candidate] {
						pos = candidate
						ok = true
						break
					}
				}
			}
			if !ok || used[pos] {
				continue
			}
			used[pos] = true
			group = append(group, pos)
		}
		if len(group) > 0 {
			groups = append(groups, group)
		}
	}
	// 没有归到任何标签的单元（例如长段被拆开）并入最后一个组，避免丢内容。
	var rest []int
	for i, seen := range used {
		if !seen {
			rest = append(rest, i)
		}
	}
	if len(rest) > 0 {
		if len(groups) == 0 {
			groups = append(groups, rest)
		} else {
			groups[len(groups)-1] = append(groups[len(groups)-1], rest...)
		}
	}
	return groups
}

// scriptPlayFixed 返回每个组（标签）是否固定。
func scriptPlayFixed(v model.LiveAgentPlanVersion) []bool {
	script, ok := recordingScriptFromContext(v.GenerationContext)
	if !ok || script.Version != 2 {
		return nil
	}
	fixed := make([]bool, 0, len(script.Tabs))
	for _, tab := range script.Tabs {
		has := len(tab.Entries) > 0
		if !has {
			continue
		}
		fixed = append(fixed, tab.Pinned)
	}
	return fixed
}

// scriptPlayRoundOrder 生成本轮供料顺序。
//
// 第 0 轮保持原始顺序：检查点校验要求首轮是恒等排列，改它会与历史检查点冲突。
// 之后每轮按标签分组：固定标签锁位，其余标签随机；组内单元随机。
// 单选模式需要检查点支持"部分顺序"，当前仍是全量排列，故与全读一致。
func scriptPlayRoundOrder(v model.LiveAgentPlanVersion, units []volcengineScriptUnit, round, previous int, previousOrder []int, rng *rand.Rand) []int {
	n := len(units)
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	if n <= 1 || round == 0 {
		return order
	}
	groups := scriptPlayGroups(v, units)
	if len(groups) <= 1 {
		order = shuffleAvoidingPrevious(order, previousOrder, rng)
		order = rotateFirstAwayFrom(order, previous)
		return order
	}
	fixed := scriptPlayFixed(v)
	// Pin whole groups at the front; never fix a boundary collision by swapping
	// one piece across tags. Search legal group orders for fewest old adjacencies.
	oldPairs := map[[2]int]bool{}
	for i := 0; i+1 < len(previousOrder); i++ {
		oldPairs[[2]int{previousOrder[i], previousOrder[i+1]}] = true
	}
	bestScore := int(^uint(0) >> 1)
	var best []int
	for attempt := 0; attempt < 60; attempt++ {
		var pinned, free []int
		for i := range groups {
			if i < len(fixed) && fixed[i] {
				pinned = append(pinned, i)
			} else {
				free = append(free, i)
			}
		}
		rng.Shuffle(len(free), func(i, j int) { free[i], free[j] = free[j], free[i] })
		tagOrder := append(pinned, free...)
		var candidate []int
		for _, gid := range tagOrder {
			for _, j := range rng.Perm(len(groups[gid])) {
				candidate = append(candidate, groups[gid][j])
			}
		}
		score := 0
		if candidate[0] == previous {
			score += n * 2
		}
		for i := 0; i+1 < len(candidate); i++ {
			if oldPairs[[2]int{candidate[i], candidate[i+1]}] {
				score++
			}
		}
		if score < bestScore {
			bestScore = score
			best = append([]int(nil), candidate...)
		}
		if score == 0 {
			break
		}
	}
	copy(order, best)
	position := len(best)
	// 极端情况下分组没有覆盖全部单元，补上漏掉的，保证仍是排列。
	for i := position; i < n; i++ {
		for candidate := 0; candidate < n; candidate++ {
			found := false
			for j := 0; j < i; j++ {
				if order[j] == candidate {
					found = true
					break
				}
			}
			if !found {
				order[i] = candidate
				break
			}
		}
	}
	_ = previous
	return order
}

// unitGroupIndex 返回每个供料单元所属的标签组下标。
func unitGroupIndex(v model.LiveAgentPlanVersion, units []volcengineScriptUnit) []int {
	groups := scriptPlayGroups(v, units)
	groupOf := make([]int, len(units))
	for i := range groupOf {
		groupOf[i] = -1
	}
	for gid, group := range groups {
		for _, idx := range group {
			if idx >= 0 && idx < len(groupOf) {
				groupOf[idx] = gid
			}
		}
	}
	return groupOf
}

// scriptPlaySkipInSingleMode 判断当前位置在单选模式下是否应当跳过。
//
// 单选模式一轮每个标签只播一个片段：本轮该标签已经播过前面那条，后面同组的都跳过。
// 这样不用改检查点结构（Order 仍是完整排列），也能实现单选。
func scriptPlaySkipInSingleMode(v model.LiveAgentPlanVersion, units []volcengineScriptUnit, order []int, position int) bool {
	script, ok := recordingScriptFromContext(v.GenerationContext)
	if !ok || script.EffectivePlayMode() != "single" {
		return false
	}
	if position < 0 || position >= len(order) {
		return false
	}
	groupOf := unitGroupIndex(v, units)
	current := order[position]
	if current < 0 || current >= len(groupOf) {
		return false
	}
	group := groupOf[current]
	if group < 0 {
		return false
	}
	for i := 0; i < position; i++ {
		idx := order[i]
		if idx >= 0 && idx < len(groupOf) && groupOf[idx] == group {
			return true
		}
	}
	return false
}

// shuffleAvoidingPrevious 打乱顺序，但尽量让相邻组合和上一轮不同。
//
// 观众记不住每条话术，但会记住"这两条总是连在一起"。
// 所以要求：首条不同、相接的两条组合不重复；做不到就取重叠最少的一种。
func shuffleAvoidingPrevious(order []int, previousOrder []int, rng *rand.Rand) []int {
	n := len(order)
	if n < 2 || len(previousOrder) < 2 {
		rng.Shuffle(n, func(i, j int) { order[i], order[j] = order[j], order[i] })
		return order
	}
	prevPairs := map[[2]int]bool{}
	for i := 0; i+1 < len(previousOrder); i++ {
		prevPairs[[2]int{previousOrder[i], previousOrder[i+1]}] = true
	}
	best := append([]int(nil), order...)
	bestOverlap := n + 1
	for attempt := 0; attempt < 60; attempt++ {
		candidate := append([]int(nil), order...)
		rng.Shuffle(n, func(i, j int) { candidate[i], candidate[j] = candidate[j], candidate[i] })
		if candidate[0] == previousOrder[0] {
			continue
		}
		overlap := 0
		for i := 0; i+1 < len(candidate); i++ {
			if prevPairs[[2]int{candidate[i], candidate[i+1]}] {
				overlap++
			}
		}
		if overlap < bestOverlap {
			best, bestOverlap = candidate, overlap
		}
		if overlap == 0 {
			break
		}
	}
	copy(order, best)
	return order
}

// rotateFirstAwayFrom 保证新一轮第一条不等于上一轮最后一条。
//
// 轮内已经去重，但轮与轮的交界没人管：上一轮最后一条和这一轮第一条
// 撞成同一片段时，观众会连续听到两遍同样的内容。
func rotateFirstAwayFrom(order []int, lastPlayed int) []int {
	if len(order) < 2 || lastPlayed < 0 {
		return order
	}
	if order[0] != lastPlayed {
		return order
	}
	for i := 1; i < len(order); i++ {
		if order[i] != lastPlayed {
			order[0], order[i] = order[i], order[0]
			break
		}
	}
	return order
}

// Single mode visits every piece of a tag before reusing one. Counts survive
// restart and the full permutation remains compatible with old checkpoints.
func scriptSingleSelectionOrder(v model.LiveAgentPlanVersion, units []volcengineScriptUnit, order, counts []int, previous int) []int {
	script, ok := recordingScriptFromContext(v.GenerationContext)
	if !ok || script.EffectivePlayMode() != "single" {
		return order
	}
	groupOf := unitGroupIndex(v, units)
	for _, group := range scriptPlayGroups(v, units) {
		positions := []int{}
		members := []int{}
		for pos, index := range order {
			if groupOf[index] == groupOf[group[0]] {
				positions = append(positions, pos)
				members = append(members, index)
			}
		}
		count := func(i int) int {
			if i < len(counts) {
				return counts[i]
			}
			return 0
		}
		sort.SliceStable(members, func(i, j int) bool {
			a, b := count(members[i]), count(members[j])
			if a != b {
				return a < b
			}
			return members[i] != previous && members[j] == previous
		})
		for i, pos := range positions {
			order[pos] = members[i]
		}
	}
	return order
}
