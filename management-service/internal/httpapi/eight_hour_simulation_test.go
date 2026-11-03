package httpapi

import (
	"fmt"
	"math/rand"
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
)

// 模拟 8 小时同音色播放：5 条话术、每条 8 个版本，
// 看"相邻两条的组合"重复概率，以及同一版本两次之间的间隔。
func TestSimulateEightHourAdjacency(t *testing.T) {
	chars := []int{802, 938, 470, 677, 959}
	const versionsPerPiece = 8
	const totalMinutes = 480.0
	const runesPerMin = 250.0

	units := make([]volcengineScriptUnit, len(chars))
	for i, c := range chars {
		units[i] = volcengineScriptUnit{Text: repeatRune("字", c), Key: fmt.Sprintf("p%d", i), Pool: fmt.Sprintf("p%d", i)}
	}
	v := model.LiveAgentPlanVersion{}
	v.VoiceIdentity.VoiceID = "voice-a"
	v.GenerationContext = map[string]any{"recording_script": map[string]any{"version": 2}}

	// 每个片段 8 个版本（原始 + 7 改写）
	lastPlayed := make([]map[string]int, len(chars)) // versionRef -> play order index
	for i := range lastPlayed {
		lastPlayed[i] = map[string]int{}
	}
	lastUse := make([][]int, len(chars)) // 每个版本最后一次被播的序号
	for i := range lastUse {
		lastUse[i] = make([]int, versionsPerPiece)
		for j := range lastUse[i] {
			lastUse[i][j] = -1
		}
	}
	refreshes := 0
	nextVersionCount := make([]int, len(chars))
	rotationUsed := make([][]bool, len(chars))
	for i := range rotationUsed {
		rotationUsed[i] = make([]bool, versionsPerPiece)
	}

	rng := rand.New(rand.NewSource(20261102))
	var played []string // "piece-version"
	previousOrder := []int(nil)
	round := 0
	elapsed := 0.0
	playIndex := 0

	for elapsed < totalMinutes {
		order := scriptPlayRoundOrder(v, units, round, -1, previousOrder, rng)
		previousOrder = append([]int(nil), order...)
		for _, piece := range order {
			if elapsed >= totalMinutes {
				break
			}
			// 选版本：先按"本轮未用"，再按最久没播
			choice := -1
			for ver := 0; ver < versionsPerPiece; ver++ {
				if !rotationUsed[piece][ver] {
					if choice < 0 || lastUse[piece][ver] < lastUse[piece][choice] {
						choice = ver
					}
				}
			}
			if choice < 0 {
				for ver := range rotationUsed[piece] {
					rotationUsed[piece][ver] = false
				}
				choice = 0
				for ver := 1; ver < versionsPerPiece; ver++ {
					if lastUse[piece][ver] < lastUse[piece][choice] {
						choice = ver
					}
				}
			}
			rotationUsed[piece][choice] = true
			lastUse[piece][choice] = playIndex
			played = append(played, fmt.Sprintf("p%d-v%d", piece, choice))
			playIndex++
			elapsed += float64(chars[piece]) / runesPerMin
			// 满池后每次播放 40% 概率换掉最久没播的，补一条新版本。
			if !rotationUsed[piece][0] || allUsed(rotationUsed[piece]) {
				if refreshRoll(rng.Float64()) {
					oldestIdx := 0
					for ver := 1; ver < versionsPerPiece; ver++ {
						if lastUse[piece][ver] < lastUse[piece][oldestIdx] {
							oldestIdx = ver
						}
					}
					nextVersionCount[piece]++
					lastUse[piece][oldestIdx] = -1
					rotationUsed[piece][oldestIdx] = false
					refreshes++
				}
			}
		}
		round++
	}

	// 统计相邻组合重复
	bigrams := map[string]int{}
	for i := 0; i+1 < len(played); i++ {
		bigrams[played[i]+"→"+played[i+1]]++
	}
	dupBigrams := 0
	maxBigramRepeat := 0
	for _, count := range bigrams {
		if count > 1 {
			dupBigrams++
		}
		if count > maxBigramRepeat {
			maxBigramRepeat = count
		}
	}
	// 同一版本两次之间的最小间隔（按播放条数）
	minGap := 1 << 30
	lastSeen := map[string]int{}
	for i, item := range played {
		if prev, ok := lastSeen[item]; ok {
			if gap := i - prev; gap < minGap {
				minGap = gap
			}
		}
		lastSeen[item] = i
	}
	// 同一个音频这一轮的前一条/后一条，是否和它上一次出现时一模一样。
	type context struct{ prev, next string }
	seenContext := map[string]context{}
	samePair, samePrev, sameNext, comparisons := 0, 0, 0, 0
	for i := 0; i < len(played); i++ {
		cur := played[i]
		prev, next := "", ""
		if i > 0 {
			prev = played[i-1]
		}
		if i+1 < len(played) {
			next = played[i+1]
		}
		if old, ok := seenContext[cur]; ok {
			comparisons++
			if old.prev == prev && old.next == next {
				samePair++
			}
			if old.prev == prev {
				samePrev++
			}
			if old.next == next {
				sameNext++
			}
		}
		seenContext[cur] = context{prev: prev, next: next}
	}
	t.Logf("同一音频与前次出现对比 %d 次：前后都相同 %d (%.0f%%)，仅前面相同 %d (%.0f%%)，仅后面相同 %d (%.0f%%)",
		comparisons, samePair, 100*float64(samePair)/float64(maxInt(comparisons, 1)),
		samePrev, 100*float64(samePrev)/float64(maxInt(comparisons, 1)),
		sameNext, 100*float64(sameNext)/float64(maxInt(comparisons, 1)))
	t.Logf("8 小时内换新 %d 次（约 %.0f 分钟一次）", refreshes, float64(totalMinutes)/float64(maxInt(refreshes, 1)))
	t.Logf("8 小时播放 %d 条（%.1f 轮）", len(played), float64(round))
	t.Logf("不同相邻组合 %d 种，重复出现的 %d 种，单组最多重复 %d 次", len(bigrams), dupBigrams, maxBigramRepeat)
	t.Logf("同一版本两次之间最少隔 %d 条", minGap)
}

func allUsed(used []bool) bool {
	for _, u := range used {
		if !u {
			return false
		}
	}
	return true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func repeatRune(s string, n int) string {
	out := make([]rune, n)
	for i := range out {
		out[i] = []rune(s)[0]
	}
	return string(out)
}

var _ = scriptplay.Version{}