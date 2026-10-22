package httpapi

import (
	"math/rand"
	"testing"

	"livecompanion/management/internal/model"
)

func TestRecordingStreamOrderReshufflesWithoutAdjacentRepeat(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	previousLast := -1
	seen := make(map[int]bool)
	for round := 0; round < 50; round++ {
		order := recordingStreamOrder(5, previousLast, rng)
		if len(order) != 5 {
			t.Fatalf("order must cover every tab, got %v", order)
		}
		seen = make(map[int]bool, 5)
		for _, tab := range order {
			if tab < 0 || tab >= 5 || seen[tab] {
				t.Fatalf("order must be a permutation, got %v", order)
			}
			seen[tab] = true
		}
		if previousLast >= 0 && order[0] == previousLast {
			t.Fatalf("round %d starts with the tab that ended the previous round: %v", round, order)
		}
		previousLast = order[len(order)-1]
	}
}

func TestRecordingStreamRefreshSetPolishesEverythingOnFirstFourRounds(t *testing.T) {
	tab := &recordingTabRun{segments: make([]recordingStreamSegment, 10), refreshed: make([]int, 10)}
	for index := range tab.refreshed {
		tab.refreshed[index] = -1
	}
	rng := rand.New(rand.NewSource(1))
	for round := 0; round < 4; round++ {
		set := recordingStreamRefreshSet(tab, round, rng)
		for index, refresh := range set {
			if !refresh {
				t.Fatalf("round %d must polish segment %d", round, index)
			}
		}
	}
}

func TestRecordingStreamRefreshSetRotatesThirtyPercent(t *testing.T) {
	tab := &recordingTabRun{segments: make([]recordingStreamSegment, 10), refreshed: make([]int, 10)}
	for index := range tab.refreshed {
		tab.refreshed[index] = 3
	}
	rng := rand.New(rand.NewSource(2))
	set := recordingStreamRefreshSet(tab, 4, rng)
	refreshed := 0
	firstPick := make([]int, 0, 3)
	for index, refresh := range set {
		if refresh {
			refreshed++
			firstPick = append(firstPick, index)
		}
	}
	if refreshed != 3 {
		t.Fatalf("10 segments must refresh 3 (30%%), got %d", refreshed)
	}
	for _, index := range firstPick {
		tab.refreshed[index] = 4
	}
	// 下一轮应优先挑没刷过的段，不能重复刚刷的三段。
	second := recordingStreamRefreshSet(tab, 5, rand.New(rand.NewSource(3)))
	for _, index := range firstPick {
		if second[index] {
			t.Fatalf("segment %d was refreshed twice in a row", index)
		}
	}
}

func TestRecordingStreamRefreshSetAlwaysRefreshesAtLeastOne(t *testing.T) {
	tab := &recordingTabRun{segments: make([]recordingStreamSegment, 2), refreshed: []int{9, 9}}
	set := recordingStreamRefreshSet(tab, 6, rand.New(rand.NewSource(4)))
	refreshed := 0
	for _, refresh := range set {
		if refresh {
			refreshed++
		}
	}
	if refreshed < 1 {
		t.Fatal("at least one segment must be refreshed")
	}
}

func TestRecordingStreamTextsFallsBackToSingleTab(t *testing.T) {
	legacy := model.LiveAgentPlanVersion{Variants: []model.LiveAgentPlanVersionVariant{
		{VariantKey: "A", Text: "旧版本单段话术", IsFormal: true},
	}}
	if texts := recordingStreamTexts(legacy); len(texts) != 1 || texts[0] != "旧版本单段话术" {
		t.Fatalf("legacy plan must play one tab, got %v", texts)
	}

	scripted := model.LiveAgentPlanVersion{GenerationContext: map[string]any{"recording_script": map[string]any{
		"tabs": []any{
			map[string]any{"tab_id": "t1", "title": "开场", "text": "第一段"},
			map[string]any{"tab_id": "t2", "title": "空", "text": "   "},
			map[string]any{"tab_id": "t3", "title": "塑品", "text": "第三段"},
		},
	}}}
	texts := recordingStreamTexts(scripted)
	if len(texts) != 2 || texts[0] != "第一段" || texts[1] != "第三段" {
		t.Fatalf("scripted plan must skip blank tabs, got %v", texts)
	}
}
