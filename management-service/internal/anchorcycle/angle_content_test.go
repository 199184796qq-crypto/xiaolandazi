package anchorcycle

import (
	"strings"
	"testing"
)

func TestDirectionContentKeepsCallerBindingDespiteModelNumbering(t *testing.T) {
	facts := []HookFact{{ID: "knowledge:16", Products: []string{"2"}, Benefits: []string{"B2"}, Data: "半只450g"}}
	p := ParseDirectionContent("2. 下班省事\n2. 包装保存\n99. 按人数选择", "issued", facts, []string{"2"}, "食用安排")
	if len(p.Cards) != 3 {
		t.Fatal(p)
	}
	facts[0].ID = "changed"
	for i, h := range p.Cards {
		if !strings.HasPrefix(h.ID, "issued/") || h.Facts[0] != "knowledge:16" || h.Products[0] != "2" || h.Benefits[0] != "B2" {
			t.Fatal(i, h)
		}
	}
	plain := ParseDirectionContent("85.5元与日常价的关系", "plain", nil, nil, "")
	if len(plain.Cards) != 1 || plain.Cards[0].Action != "85.5元与日常价的关系" {
		t.Fatal(plain)
	}
	if len(ParseDirectionContent(" \n", "empty", nil, nil, "").Cards) != 0 {
		t.Fatal("empty was fabricated")
	}
}

func TestDirectionDeduplicationPreservesOriginalHistoryAndBinding(t *testing.T) {
	old := []AngleHook{{ID: "old/1", Action: "安排开袋即食", Facts: []string{"knowledge:16"}}}
	added := []AngleHook{{ID: "new/1", Action: "安排开袋即食"}, {ID: "new/2", Action: "安排复热取舍"}}
	usable := DistinctDirectionCards(old, added)
	if len(usable) != 2 || usable[0].ID != "old/1" || usable[0].Facts[0] != "knowledge:16" || usable[1].ID != "new/2" {
		t.Fatal(usable)
	}
	if len(old) != 1 || len(added) != 2 || added[0].ID != "new/1" {
		t.Fatal("history changed")
	}
}

func TestAngleRefreshUsesOnlyRealProgramReceipts(t *testing.T) {
	s := State{Surface: "formal_live", Delivery: &MainlineDelivery{ProgramID: "current"}}
	for i, id := range []string{"a", "b", "c"} {
		s.RegisterAngleTrack(Segment{Selected: Candidate{ID: id, AnglePlan: &AnglePlan{SourceSHA: "pool"}, AngleHook: &AngleHook{ID: "h"}}})
		if s.AngleRefreshDue("pool") {
			t.Fatal("generation counted", i)
		}
	}
	receipts := []AnglePlaybackReceipt{{1, "a", "time"}, {2, "b", "time"}, {3, "c", "time"}}
	if len(s.ApplyAnglePlayback("old", receipts)) != 0 {
		t.Fatal("old session counted")
	}
	if len(s.ApplyAnglePlayback("current", receipts[:2])) != 2 || s.AngleRefreshDue("pool") {
		t.Fatal("wrong threshold")
	}
	if len(s.ApplyAnglePlayback("current", receipts)) != 1 || !s.AngleRefreshDue("pool") {
		t.Fatal("third play did not refresh")
	}
	s.RegisterAngleTrack(Segment{Selected: Candidate{ID: "a", AnglePlan: &AnglePlan{SourceSHA: "pool"}, AngleHook: &AngleHook{ID: "h"}}})
	if len(s.ApplyAnglePlayback("current", receipts)) != 0 || s.AngleUsage("pool").Plays != 3 || len(s.AngleTrackBindings) != 0 {
		t.Fatal("resume/duplicate counted")
	}
	s.AngleUsage("pool").LastAttemptAt = 3
	if s.AngleRefreshDue("pool") {
		t.Fatal("failed refresh retried immediately")
	}
	fresh := State{}
	if fresh.AngleUsage("pool").Plays != 0 || fresh.AngleRefreshDue("pool") {
		t.Fatal("new session inherited progress")
	}
	fresh.Config.AngleRefreshAfterPlays = 5
	fresh.AngleUsage("pool").Plays = 3
	if fresh.AngleRefreshDue("pool") {
		t.Fatal("configured threshold ignored")
	}
}

func TestAngleFingerprintExcludesRoomButChangesWithSource(t *testing.T) {
	s := State{Queue: []Task{{Key: "a", SellingPoint: "包装", Angle: "选择"}}, Facts: "抽真空6天，不抽真空3天", AnglePlanMotherSHA: "current"}
	key := s.AnglePlanKey()
	s.RoomID = 27
	s.RunID = "new"
	if key != s.AnglePlanKey() {
		t.Fatal("pool split by room or session")
	}
	s.Queue[0].ID = "new-program-task-id"
	s.Queue[0].ProductMaxChars = 100
	s.Queue[0].ProductMode = "short"
	if key != s.AnglePlanKey() {
		t.Fatal("pool split by execution metadata")
	}
	s.Config.ComplianceScope = "health_food"
	if key == s.AnglePlanKey() {
		t.Fatal("pool reused across different applicable P0 scopes")
	}
	s.Config.ComplianceScope = ""
	s.Facts = "抽真空6天，不抽真空2天"
	if key == s.AnglePlanKey() {
		t.Fatal("outdated source pool reused")
	}
}

func TestFailedPlanningRetriesAfterActualTaskUses(t *testing.T) {
	s := State{Surface: "formal_live", Delivery: &MainlineDelivery{ProgramID: "p"}}
	s.AngleUsage("key").LastAttemptAt = 0
	for i, id := range []string{"a", "b", "c"} {
		s.RegisterAngleTrack(Segment{Selected: Candidate{ID: id, AnglePlan: &AnglePlan{SourceSHA: "key", PlanError: "timeout"}}})
		s.ApplyAnglePlayback("p", []AnglePlaybackReceipt{{Sequence: uint64(i + 1), TrackID: id, StartedAt: "time"}})
	}
	if !s.AngleRefreshDue("key") {
		t.Fatal("fallback could never retry planning")
	}
}
