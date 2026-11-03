package httpapi

import (
	"math/rand"
	"testing"

	"livecompanion/management/internal/model"
)

func playFixture(version model.LiveAgentPlanVersion) (model.LiveAgentPlanVersion, []volcengineScriptUnit) {
	script := recordingScript{
		Version: 2,
		Tabs: []recordingScriptTab{
			{TabID: "intro", Title: "开场", Pinned: true, Entries: []recordingScriptEntry{{EntryID: "a", Text: longText("开场第一句")}, {EntryID: "b", Text: longText("开场第二句")}}},
			{TabID: "product", Title: "讲解", Entries: []recordingScriptEntry{{EntryID: "c", Text: longText("讲解第一句")}, {EntryID: "d", Text: longText("讲解第二句")}}},
			{TabID: "after", Title: "售后", Pinned: true, Entries: []recordingScriptEntry{{EntryID: "e", Text: longText("售后第一句")}}},
		},
	}
	version.GenerationContext = map[string]any{"recording_script": script}
	var units []volcengineScriptUnit
	for _, tab := range script.Tabs {
		for _, entry := range tab.Entries {
			units = append(units, volcengineScriptUnit{Text: entry.Text, Key: scriptFirstParagraphKey(version, entry.Text)})
		}
	}
	return version, units
}

func TestScriptPlayGroupsFollowTabs(t *testing.T) {
	version, units := playFixture(model.LiveAgentPlanVersion{})
	groups := scriptPlayGroups(version, units)
	if len(groups) != 3 {
		t.Fatalf("应分成 3 组，实际 %d: %v", len(groups), groups)
	}
	if len(groups[0]) != 2 || len(groups[1]) != 2 || len(groups[2]) != 1 {
		t.Fatalf("各组大小不对: %v", groups)
	}
	seen := map[int]bool{}
	for _, group := range groups {
		for _, idx := range group {
			if seen[idx] {
				t.Fatalf("单元 %d 被分到多个组", idx)
			}
			seen[idx] = true
		}
	}
	if len(seen) != len(units) {
		t.Fatalf("有单元没被分组: seen=%d units=%d", len(seen), len(units))
	}
}

func TestScriptPlayGroupsWithoutScriptIsSingleGroup(t *testing.T) {
	_, units := playFixture(model.LiveAgentPlanVersion{})
	groups := scriptPlayGroups(model.LiveAgentPlanVersion{}, units)
	if len(groups) != 1 || len(groups[0]) != len(units) {
		t.Fatalf("无分组信息时整体应为一组: %v", groups)
	}
}

func TestScriptPlayRoundZeroIsIdentity(t *testing.T) {
	version, units := playFixture(model.LiveAgentPlanVersion{})
	order := scriptPlayRoundOrder(version, units, 0, -1, nil, rand.New(rand.NewSource(3)))
	for i, idx := range order {
		if idx != i {
			t.Fatalf("首轮必须是恒等排列（检查点兼容）: %v", order)
		}
	}
}

func TestScriptPlayRoundKeepsFixedTagsInPlace(t *testing.T) {
	version, units := playFixture(model.LiveAgentPlanVersion{})
	// 组内顺序：开场(0,1) 讲解(2,3) 售后(4)
	for seed := int64(0); seed < 20; seed++ {
		order := scriptPlayRoundOrder(version, units, 1, -1, nil, rand.New(rand.NewSource(seed)))
		if len(order) != len(units) {
			t.Fatalf("排列长度不对: %v", order)
		}
		present := map[int]bool{}
		for _, idx := range order {
			if idx < 0 || idx >= len(units) || present[idx] {
				t.Fatalf("排列非法: %v", order)
			}
			present[idx] = true
		}
		// 标签必须连续：开场两句相邻、讲解两句相邻、售后一句独立。
		positions := map[int]int{}
		for pos, idx := range order {
			positions[idx] = pos
		}
		if abs(positions[0]-positions[1]) != 1 {
			t.Fatalf("开场两句不相邻: %v", order)
		}
		if abs(positions[2]-positions[3]) != 1 {
			t.Fatalf("讲解两句不相邻: %v", order)
		}
		// 固定标签锁位：开场组在位置 0-1，售后组在位置 4。
		if positions[0] > 1 || positions[1] > 1 {
			t.Fatalf("开场是固定标签，应在最前: %v", order)
		}
		if positions[4] != 2 {
			t.Fatalf("固定标签整体排在非固定标签之前: %v", order)
		}
	}
}

// longText 把一句话补到 400 字以上，满足话术片的保存校验。
func longText(head string) string {
	tail := []rune("。这是补足长度的正文，用来满足每条话术四百字以上的保存校验。")
	out := []rune(head + "，")
	for len(out) < 420 {
		out = append(out, tail...)
	}
	return string(out)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestScriptPlaySingleModeSkipsExtraPieces(t *testing.T) {
	version, units := playFixture(model.LiveAgentPlanVersion{})
	script := version.GenerationContext["recording_script"].(recordingScript)
	script.PlayMode = "single"
	version.GenerationContext["recording_script"] = script
	rng := rand.New(rand.NewSource(5))
	order := scriptPlayRoundOrder(version, units, 1, -1, nil, rng)
	var played []int
	for pos := range order {
		if scriptPlaySkipInSingleMode(version, units, order, pos) {
			continue
		}
		played = append(played, order[pos])
	}
	// 3 个标签 → 单选一轮只播 3 条，且分别来自不同标签。
	if len(played) != 3 {
		t.Fatalf("单选应播 3 条，实际 %d：%v", len(played), played)
	}
	groupOf := unitGroupIndex(version, units)
	seen := map[int]bool{}
	for _, idx := range played {
		if seen[groupOf[idx]] {
			t.Fatalf("单选出现同标签两条：%v", played)
		}
		seen[groupOf[idx]] = true
	}
}

func TestScriptPlayFullModeNeverSkips(t *testing.T) {
	version, units := playFixture(model.LiveAgentPlanVersion{})
	order := scriptPlayRoundOrder(version, units, 1, -1, nil, rand.New(rand.NewSource(6)))
	for pos := range order {
		if scriptPlaySkipInSingleMode(version, units, order, pos) {
			t.Fatalf("全读模式不应跳过任何片段（位置 %d）", pos)
		}
	}
}
