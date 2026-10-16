package anchorcycle

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorstylescore"
)

// Three own points versus one; public weights deliberately dwarf own weights.
func productOnlyFixture(t *testing.T, commonPoints int, equalRoles bool) State {
	t.Helper()
	products := []FocusProduct{{LinkKey: "1", Roles: []string{"ordinary"}}, {LinkKey: "2", Roles: []string{"main"}}}
	if equalRoles {
		products[1].Roles = products[0].Roles
	}
	for i := range products {
		angleCounts := []int{4, 5, 5}
		if i == 1 {
			angleCounts = []int{6}
		}
		for j, count := range angleCounts {
			point := FocusSellingPoint{Name: fmt.Sprintf("point-%d", j), Weight: 1}
			for k := 0; k < count; k++ {
				point.Angles = append(point.Angles, FocusSellingAngle{Angle: fmt.Sprintf("angle-%d", k), Weight: 1})
			}
			products[i].SellingPoints = append(products[i].SellingPoints, point)
		}
		for j := 0; j < commonPoints; j++ {
			products[i].CommonPoints = append(products[i].CommonPoints, FocusSellingPoint{Name: fmt.Sprintf("common-%d", j), Weight: 1000, Angles: []FocusSellingAngle{{Angle: "common-angle", Weight: 1000}}})
		}
	}
	focus := &ProductFocus{MaterialVersion: "product-common-v1", Policy: ProductPolicy{Version: "test"}, Products: products,
		Facts: []ScopedFact{{Text: "共有停车资料", Shared: true}, {Text: "一号专属", Links: []string{"1"}, Applicable: true}, {Text: "二号专属", Links: []string{"2"}, Applicable: true}}}
	tasks := ExpandProductSellingPoints(products, &focus.Policy)
	focus.Tasks = ProductTaskScopes(tasks)
	s, err := New(tasks, Config{Rolling: true, BodyMin: 4, BodyMax: 4, MaxChars: 300, Threshold: 40, CombinationGap: 8, MinReappearCycles: 2})
	if err != nil {
		t.Fatal(err)
	}
	s.RunID, s.ProductFocus = "unequal-product-pools", focus
	focus.ResetRun()
	s.SelectProductTask()
	return s
}

func TestCommonShareIndependentOfPoolSizesAndProductWeights(t *testing.T) {
	for _, equal := range []bool{true, false} {
		t.Run(fmt.Sprint(equal), func(t *testing.T) {
			without, one, many := productOnlyFixture(t, 0, equal), productOnlyFixture(t, 1, equal), productOnlyFixture(t, 30, equal)
			counts, public := map[string]int{}, 0
			for i := 0; i < 3000; i++ {
				a, _ := simulateBody(&one)
				b, _ := simulateBody(&many)
				if a.Common != b.Common {
					t.Fatal("public cardinality changed top-level schedule", i)
				}
				if b.Common {
					public++
				} else {
					original, _ := simulateBody(&without)
					if b.LinkKey != b.FocusLink || a.FocusLink != b.FocusLink || taskPointKey(a) != taskPointKey(b) || original.FocusLink != b.FocusLink || taskPointKey(original) != taskPointKey(b) {
						t.Fatal("public share changed internal product/point sequence", i, original, a, b)
					}
					counts[b.FocusLink]++
				}
				one.CycleNumber, many.CycleNumber, without.CycleNumber = i/4+1, i/4+1, i/4+1
			}
			if public != 600 || counts["1"]+counts["2"] != 2400 || !reflect.DeepEqual(counts, many.ProductFocus.AdoptedSegments) || many.ProductFocus.ScheduleCursor != without.ProductFocus.ScheduleCursor || many.ProductFocus.ScheduleWindow != without.ProductFocus.ScheduleWindow {
				t.Fatal("20/80 share or product counters changed", public, counts)
			}
			if equal && counts["1"] != counts["2"] || !equal && (counts["2"] < 1308 || counts["2"] > 1310) {
				t.Fatal("product weights affected by pool size", counts)
			}
			t.Logf("3000 main-body tasks: public=%d products=%v", public, counts)
		})
	}
}

func TestCommonSharePreservesFactsAndSavedMaterial(t *testing.T) {
	s := productOnlyFixture(t, 3, false)
	before, _ := json.Marshal(s.ProductFocus.Products)
	for i := 0; i < 50; i++ {
		task, _ := simulateBody(&s)
		facts := s.ProductFacts(task)
		if !strings.Contains(facts, "共有停车资料") || !strings.Contains(facts, map[string]string{"1": "一号专属", "2": "二号专属"}[task.FocusLink]) {
			t.Fatal("source material lost", task, facts)
		}
		if !task.Common && strings.Contains(facts, map[string]string{"1": "二号专属", "2": "一号专属"}[task.FocusLink]) {
			t.Fatal("cross-product facts", task, facts)
		}
		if task.Common && (task.ProductMode != "common" || len(task.ProductLinks()) != 2) {
			t.Fatal("public scope narrowed", task)
		}
	}
	after, _ := json.Marshal(s.ProductFocus.Products)
	if string(before) != string(after) {
		t.Fatal("immutable material rewritten")
	}
}

func TestCommonShareRetriesReloadAndExhaustionPreservePendingQuota(t *testing.T) {
	for _, kind := range []string{productContentKind, commonContentKind} {
		t.Run(kind, func(t *testing.T) {
			s := productOnlyFixture(t, 3, false)
			s.Phase, s.Surface = "body", "formal_live"
			s.ContentSchedule = &WeightedSchedule{Items: []string{kind}}
			s.SelectProductTask()
			before := s.Task()
			raw, _ := json.Marshal(s)
			var restored State
			if err := json.Unmarshal(raw, &restored); err != nil {
				t.Fatal(err)
			}
			s = restored
			s.SelectProductTask()
			if s.Task() != before || !s.RotateAfterDuplicate() || s.Task().Common != before.Common || s.Task().FocusLink != before.FocusLink {
				t.Fatal("retry changed category/product", before, s.Task())
			}
			if s.ContentSchedule.Cursor != 0 || len(s.ContentAdopted) != 0 || s.ProductFocus.ScheduleCursor != 0 || len(s.PointUses) != 0 {
				t.Fatal("retry spent quotas")
			}
			for _, task := range s.Queue {
				if !task.Common && task.LinkKey == before.FocusLink {
					if s.P0SkippedKeys == nil {
						s.P0SkippedKeys = map[string]bool{}
					}
					s.P0SkippedKeys[task.Key] = true
				}
			}
			s.SkipGeneration("test", "unavailable direction")
			if s.Status != "ready" || s.Task().Common || s.Task().FocusLink != before.FocusLink || len(s.ContentAdopted) != 0 || s.ContentSchedule.Cursor != 0 {
				t.Fatal("failure borrowed public or spent quota", s.Status, s.Task())
			}
		})
	}
}

func TestCommonShareRealCyclesAndReload(t *testing.T) {
	s := productOnlyFixture(t, 3, false)
	counts := map[string]int{}
	openings, closings, public := 0, 0, 0
	for i := 0; i < 150; i++ {
		phase, task := s.Phase, s.Task()
		if phase == "opening" && task.Common {
			t.Fatal("public replaced opening")
		}
		ok, err := s.AddCandidate(candidate(fmt.Sprintf("第%d段隔离调度样本。", i), 100), anchorstylescore.Baseline{})
		if err != nil || !ok {
			t.Fatal("adoption failed", err, s.Status)
		}
		switch phase {
		case "body":
			if task.Common {
				public++
			} else {
				counts[task.FocusLink]++
			}
		case "opening":
			openings++
		case "closing":
			closings++
		}
		if i == 37 {
			raw, _ := json.Marshal(s)
			var restored State
			if err := json.Unmarshal(raw, &restored); err != nil {
				t.Fatal(err)
			}
			s = restored
		}
	}
	if openings != 25 || closings != 25 || public != 20 || counts["1"]+counts["2"] != 80 || !reflect.DeepEqual(counts, s.ProductFocus.AdoptedSegments) || s.ContentAdopted[commonContentKind] != 20 || s.ContentAdopted[productContentKind] != 80 {
		t.Fatal("cycle counts changed", openings, closings, public, counts, s.ContentAdopted)
	}
	t.Logf("25 cycles: public=%d products=%v", public, counts)
}

func TestCommonShareCountsPublicWithoutSpendingProductQuota(t *testing.T) {
	s := productOnlyFixture(t, 3, false)
	s.Phase = "body"
	common := Task{Common: true, FocusLink: "1", SellingPoint: "parking", Key: "common:parking"}
	s.recordProductAdoption(common, "共有资料")
	s.recordScheduledAdoption(common)
	if len(s.ProductFocus.AdoptedSegments) != 0 || s.ProductFocus.ScheduleCursor != 0 || s.PointUses["common:parking"] != 1 || len(s.PointUses) != 1 || s.ContentAdopted[commonContentKind] != 1 || s.PointUses["1:parking"] != 0 {
		t.Fatal("public polluted product counters")
	}
}

func TestCommonShareRequiresOwnPointsAndHandlesMissingPublic(t *testing.T) {
	s, err := New([]Task{{Common: true, SellingPoint: "parking", Angle: "location"}}, Config{Rolling: true, MaxChars: 300})
	if err != nil {
		t.Fatal(err)
	}
	s.SelectProductTask()
	if s.Status != "spacing_blocked" {
		t.Fatal("common-only material passed as product", s.Status)
	}
	for _, count := range []int{0, 3} {
		s = productOnlyFixture(t, count, false)
		s.Phase, s.Surface = "body", "formal_live"
		if count > 0 {
			s.P0SkippedKeys = map[string]bool{}
			for _, task := range s.Queue {
				if task.Common {
					s.P0SkippedKeys[task.Key] = true
				}
			}
		}
		for i := 0; i < 50; i++ {
			task, _ := simulateBody(&s)
			if s.Status != "ready" || task.Common {
				t.Fatal("missing public stopped product supply", count, s.Status, task)
			}
		}
		s.P0SkippedKeys = nil
		public := 0
		for i := 0; i < 50; i++ {
			task, _ := simulateBody(&s)
			if task.Common {
				public++
			}
		}
		if count > 0 && public != 10 || count == 0 && public != 0 {
			t.Fatal("missing public was fabricated or repaid in a burst", count, public)
		}
	}
}

func TestCommonShareSingleProductLegacyAndFreshRun(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		s := productOnlyFixture(t, 3, false)
		if scoped {
			s.ProductFocus.Products = s.ProductFocus.Products[:1]
			s.Queue = ExpandProductSellingPoints(s.ProductFocus.Products, &s.ProductFocus.Policy)
			s.ProductFocus.Tasks = ProductTaskScopes(s.Queue)
			s.ProductFocus.ResetRun()
		} else {
			s.ProductFocus = nil
		}
		public := 0
		for i := 0; i < 100; i++ {
			task, _ := simulateBody(&s)
			if task.Common {
				public++
			}
		}
		if public != 20 {
			t.Fatal("legacy/single share changed", scoped, public)
		}
	}
	fresh := productOnlyFixture(t, 3, false)
	if len(fresh.ContentAdopted) != 0 || fresh.ContentSchedule != nil {
		t.Fatal("new run inherited old category progress")
	}
}

func TestCommonShareP0SkipAndPromisedContinuationDoNotDoubleSpend(t *testing.T) {
	s := productOnlyFixture(t, 3, false)
	s.Phase, s.Surface = "body", "formal_live"
	s.ContentSchedule = &WeightedSchedule{Items: []string{commonContentKind, productContentKind}}
	s.SelectProductTask()
	initial := s.Task()
	s.SkipGeneration("tts", "one direction unavailable")
	if !s.Task().Common || s.ContentSchedule.Cursor != 0 || len(s.ContentAdopted) != 0 || !s.P0SkippedKeys[initial.Key] {
		t.Fatal("failed public draft consumed a slot or escaped its category")
	}
	task := s.Task()
	// A promised continuation belongs to the original category and scope, but
	// is not a second pool selection. Exercise actual AddCandidate accounting.
	s.PriceHook = &PriceHook{Task: task, Promise: "计算价格", SourceText: "下面算一下"}
	if !s.Task().Common || s.Task().ProductMode != "common" || len(s.Task().ProductLinks()) != 2 {
		t.Fatal("continuation lost public ownership", s.Task())
	}
	ok, err := s.AddCandidate(candidate("两份合计二十元。", 100), anchorstylescore.Baseline{})
	if err != nil || !ok || len(s.ProductFocus.AdoptedSegments) != 0 || s.ContentSchedule.Cursor != 0 || len(s.ContentAdopted) != 0 {
		t.Fatal("continuation spent a public or product pool quota", err, s.Status, s.ContentAdopted)
	}
}
