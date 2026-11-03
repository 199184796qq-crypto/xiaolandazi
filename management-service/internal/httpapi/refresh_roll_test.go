package httpapi

import (
	"math/rand"
	"testing"
)

// 满槽时每次播放掷骰子：跑 100 次，看实际换新次数是否接近 40。
func TestRefreshRollIsAboutFortyPercent(t *testing.T) {
	rng := rand.New(rand.NewSource(20261102))
	const trials = 100
	refreshed := 0
	for i := 0; i < trials; i++ {
		if refreshRoll(rng.Float64()) {
			refreshed++
		}
	}
	t.Logf("100 次播放里换新 %d 次（目标 40 次）", refreshed)
	if refreshed < 25 || refreshed > 55 {
		t.Fatalf("换新次数偏离 40%% 太多: %d/100", refreshed)
	}
}

// 大样本再确认一次概率本身没问题。
func TestRefreshRollLargeSample(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	const trials = 10000
	refreshed := 0
	for i := 0; i < trials; i++ {
		if refreshRoll(rng.Float64()) {
			refreshed++
		}
	}
	ratio := float64(refreshed) / trials
	t.Logf("10000 次比例 %.3f", ratio)
	if ratio < 0.37 || ratio > 0.43 {
		t.Fatalf("概率偏离 0.40 太多: %.3f", ratio)
	}
}