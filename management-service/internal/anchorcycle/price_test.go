package anchorcycle

import (
	"encoding/json"
	"testing"
)

func TestPriceHooksMustResolveNextAdoptionAndCannotChain(t *testing.T) {
	s := rollingFixture(t, 2)
	add(t, &s, "下一段我来算两桶多少钱，先看这个规格。", 70)
	if s.PriceHook == nil || s.Task().SellingPoint != "塑价兑现" || s.PriceHook.DueIndex != 2 {
		t.Fatal(s)
	}
	before, task := s.BodyCount, s.Task()
	if add(t, &s, "下一轮我再给你算账，先看看油坊。", 99) || s.BodyCount != before || s.Task() != task {
		t.Fatal("unresolved hook advanced")
	}
	if !add(t, &s, "一桶129块9，两桶合计259块8，账就在这里。", 70) || s.PriceHook != nil || s.BodyCount != 1 || len(s.Used) != 1 {
		t.Fatal(s)
	}
	if s.PriceHookAllowed() {
		t.Fatal("price hooks too frequent")
	}
}

func TestUnresolvedPriceCapDoesNotSelectBestInvalidDraft(t *testing.T) {
	s := rollingFixture(t, 2)
	s.Config.MaxRetries = 1
	add(t, &s, "下一段我来算两桶多少钱。", 70)
	add(t, &s, "计算器拿出来，下一轮再算账。", 99)
	add(t, &s, "一桶129块9，另一款119块9，都划算。", 90)
	if s.Status != "content_blocked" || len(s.Accepted) != 1 || len(s.Attempts) != 2 || s.PriceHook == nil {
		t.Fatal(s)
	}
}

func TestVariableBodyTargetFrozenAcrossRetryAndReload(t *testing.T) {
	s, err := New([]Task{{SellingPoint: "甲", Angle: "一"}, {SellingPoint: "乙", Angle: "二"}, {SellingPoint: "丙", Angle: "三"}}, Config{Rolling: true, Threshold: 40, MaxRetries: 1, MaxChars: 300, BodyMin: 2, BodyMax: 6, MaxCycles: 20})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for round := 1; round <= 20; round++ {
		target := s.BodyLimit()
		seen[target] = true
		if target < 2 || target > 6 {
			t.Fatal(target)
		}
		if add(t, &s, "尚未采纳", 20) || s.BodyLimit() != target {
			t.Fatal("retry changed body target")
		}
		raw, _ := json.Marshal(s)
		var restored State
		if json.Unmarshal(raw, &restored) != nil || restored.BodyLimit() != target {
			t.Fatal("reload changed target")
		}
		s = restored
		add(t, &s, "开场采纳", 70)
		for i := 0; i < target; i++ {
			add(t, &s, "中间讲解采纳", 70)
		}
		if s.Phase != "closing" || s.BodyCount != target {
			t.Fatal(s)
		}
		add(t, &s, "收口采纳", 70)
	}
	if s.Phase != "ended" || len(seen) < 2 {
		t.Fatal("rounds are fixed", seen)
	}
}

func TestPriceHookExtendsWithinMaximumOtherwiseClosingResolves(t *testing.T) {
	for _, limit := range []int{2, 6} {
		s := rollingFixture(t, 1)
		s.Config.BodyMin, s.Config.BodyMax, s.BodyTarget = 2, 6, limit
		add(t, &s, "开场", 70)
		for i := 0; i < limit-1; i++ {
			add(t, &s, "讲解", 70)
		}
		add(t, &s, "下一段我来算两桶多少钱。", 70)
		if limit == 2 && (s.Phase != "body" || s.BodyLimit() != 3) {
			t.Fatal("did not extend")
		}
		if limit == 6 && s.Phase != "closing" {
			t.Fatal("exceeded maximum")
		}
		add(t, &s, "一桶129块9，两桶合计259块8。", 70)
		if s.PriceHook != nil || s.BodyCount > 6 {
			t.Fatal(s)
		}
	}
}

func TestPriceShapeNotAnyTwoUnrelatedPricesAndTerminalCannotLeaveHook(t *testing.T) {
	if InspectPrice("1号129块9，2号119块9。", Task{}).Completed {
		t.Fatal("unrelated prices treated as arithmetic")
	}
	if !InspectPrice("119块9买9斤2两，划下来每斤13块。", Task{}).Completed {
		t.Fatal("unit price not recognized")
	}
	s := rollingFixture(t, 1)
	s.Phase = "closing"
	if s.PriceHookAllowed() {
		t.Fatal("terminal hooked an unscheduled next paragraph")
	}
}
