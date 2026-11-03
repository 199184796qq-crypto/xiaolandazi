package httpapi

import (
	"strings"
	"testing"
)

// 杨鸭子真实形态：标签1 有 802/940 字，标签2 有 472/677 字。
func anchorFixture(pinnedFirst bool) recordingScript {
	long := func(head string, n int) string {
		var b strings.Builder
		b.WriteString(head)
		for len([]rune(b.String())) < n {
			b.WriteString("这是补足长度的正文。")
		}
		return b.String()
	}
	return recordingScript{
		Version: 2,
		Tabs: []recordingScriptTab{
			{TabID: "t1", Title: "话术01", Pinned: pinnedFirst, Entries: []recordingScriptEntry{
				{EntryID: "a", Text: long("上班累了一天", 802)},
				{EntryID: "b", Text: long("上班累了一天回到家", 940)},
			}},
			{TabID: "t2", Title: "话术02", Entries: []recordingScriptEntry{
				{EntryID: "c", Text: long("家人们别再犹豫", 472)},
				{EntryID: "d", Text: long("宝宝们先停一下", 677)},
			}},
		},
	}
}

func TestOpeningAnchorPicksGlobalShortest(t *testing.T) {
	script := anchorFixture(false)
	script.normalize()
	if script.OpeningEntryID != "c" || script.OpeningTabID != "t2" {
		t.Fatalf("没有固定标签时应取全方案最短（472 字那条 c），实际 %s / %s",
			script.OpeningTabID, script.OpeningEntryID)
	}
}

func TestOpeningAnchorRespectsPinnedFirstTag(t *testing.T) {
	script := anchorFixture(true)
	script.normalize()
	if script.OpeningTabID != "t1" {
		t.Fatalf("第一个标签固定时应取它，实际 %s", script.OpeningTabID)
	}
	if script.OpeningEntryID != "a" {
		t.Fatalf("固定标签内应取最短（802 字那条 a），实际 %s", script.OpeningEntryID)
	}
}

func TestOpeningAnchorPinnedElsewhereStillGlobalShortest(t *testing.T) {
	// 第二个标签固定，但不在第一位：开场仍然全方案最短。
	script := anchorFixture(false)
	script.Tabs[1].Pinned = true
	script.normalize()
	if script.OpeningEntryID != "c" {
		t.Fatalf("固定标签不在第一位时，开场应取全方案最短，实际 %s", script.OpeningEntryID)
	}
}
