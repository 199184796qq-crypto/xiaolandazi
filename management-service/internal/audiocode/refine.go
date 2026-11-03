package audiocode

import "math"

// preambleTemplate 是前导音的参考波形，用于把候选偏移精确定位到采样。
var preambleTemplate = func() []float64 {
	t := make([]float64, PreambleSamples)
	for i := range t {
		t[i] = math.Cos(2 * math.Pi * SyncHz * float64(i) / SampleRate)
	}
	return t
}()

// refineOffset 在候选点附近用前导匹配滤波找精确起点。
// 数据载波对采样对齐极敏感（11 kHz 下错 1 个采样相位就转约 165°），
// 所以必须把偏移精到采样，不能靠能量峰。
func refineOffset(pcm []int16, candidate, radius int) int {
	lo := candidate - radius
	if lo < 0 {
		lo = 0
	}
	hi := candidate + radius
	if hi+PreambleSamples >= len(pcm) {
		hi = len(pcm) - PreambleSamples - 1
	}
	if hi < lo {
		return candidate
	}
	best, bestScore := candidate, math.Inf(-1)
	for off := lo; off <= hi; off++ {
		var score float64
		for i := 0; i < PreambleSamples; i++ {
			score += float64(pcm[off+i]) * preambleTemplate[i]
		}
		if score > bestScore {
			bestScore, best = score, off
		}
	}
	return best
}
