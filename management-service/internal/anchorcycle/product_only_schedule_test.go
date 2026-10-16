package anchorcycle

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorstylescore"
)

// Match the customer's unequal pools: three points for product 1, one for
// product 2. Common material must not compete with either product's points.
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
	s.RunID = "unequal-product-pools"
	s.ProductFocus = focus
	focus.ResetRun()
	s.SelectProductTask()
	return s
}

func TestProductOnlyScheduleIndependentOfCommonCountAndProductPoolSize(t *testing.T) {
	for _, equalRoles := range []bool{true, false} {
		t.Run(fmt.Sprint(equalRoles), func(t *testing.T) {
			without := productOnlyFixture(t, 0, equalRoles)
			with := productOnlyFixture(t, 30, equalRoles)
			counts, points := map[string]int{}, map[string]int{}
			for i := 0; i < 2400; i++ {
				a, _ := simulateBody(&without)
				b, _ := simulateBody(&with)
				if a != b || b.Common || b.LinkKey != b.FocusLink {
					t.Fatalf("common material changed product/point allocation at %d: %+v / %+v", i, a, b)
				}
				counts[b.FocusLink]++
				points[taskPointKey(b)]++
				without.CycleNumber, with.CycleNumber = i/4+1, i/4+1
			}
			if equalRoles && counts["1"] != counts["2"] {
				t.Fatal("point count changed equal product shares", counts)
			}
			if !equalRoles && (counts["2"] < 1308 || counts["2"] > 1310) {
				t.Fatal("main product share no longer follows the existing 1.2 ratio", counts)
			}
			if !reflect.DeepEqual(counts, with.ProductFocus.AdoptedSegments) {
				t.Fatal("common content polluted product counters")
			}
			t.Logf("2400 body segments, equal roles=%t: products=%v points=%v; common=0", equalRoles, counts, points)
		})
	}
}

func TestProductOnlyScheduleKeepsCommonFactsAndSavedMaterial(t *testing.T) {
	s := productOnlyFixture(t, 3, false)
	before, _ := json.Marshal(s.ProductFocus.Products)
	for i := 0; i < 48; i++ {
		task, _ := simulateBody(&s)
		facts := s.ProductFacts(task)
		if !strings.Contains(facts, "共有停车资料") || !strings.Contains(facts, map[string]string{"1": "一号专属", "2": "二号专属"}[task.FocusLink]) || strings.Contains(facts, map[string]string{"1": "二号专属", "2": "一号专属"}[task.FocusLink]) {
			t.Fatal("fact context or product boundaries changed", task, facts)
		}
	}
	after, _ := json.Marshal(s.ProductFocus.Products)
	if string(before) != string(after) {
		t.Fatal("scheduling rewrote the saved material")
	}
}

func TestProductOnlyScheduleRetriesReloadAndExhaustionDoNotBorrowCommon(t *testing.T) {
	s := productOnlyFixture(t, 3, false)
	s.Phase, s.Surface = "body", "formal_live"
	owner := s.Task().FocusLink
	var common Task
	for _, task := range s.Queue {
		if task.Common {
			common = task
		}
	}
	// A state saved by the old version can still have a common item pending.
	s.PointSchedules[owner] = &WeightedSchedule{Items: []string{s.schedulingPointKey(common, owner)}}
	raw, _ := json.Marshal(s)
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	s.SelectProductTask()
	if s.Task().Common || s.Task().FocusLink != owner {
		t.Fatal("reloaded common slot escaped the product", s.Task())
	}
	if !s.RotateAfterDuplicate() || s.Task().Common || s.Task().FocusLink != owner {
		t.Fatal("duplicate retry borrowed common material", s.Task())
	}
	for _, task := range s.Queue {
		if !task.Common && task.LinkKey == owner {
			if s.P0SkippedKeys == nil {
				s.P0SkippedKeys = map[string]bool{}
			}
			s.P0SkippedKeys[task.Key] = true
		}
	}
	s.SkipGeneration("test", "unavailable direction")
	if s.Status != "ready" || s.Task().Common || s.Task().FocusLink != owner || s.ProductFocus.ScheduleCursor != 0 || len(s.PointUses) != 0 {
		t.Fatal("failure fallback consumed a quota or borrowed common material", s.Task(), s.Status)
	}
}

func TestProductOnlyScheduleRealPhaseTransitions(t *testing.T) {
	s := productOnlyFixture(t, 3, false)
	counts := map[string]int{}
	openings, closings := 0, 0
	for i := 0; i < 120; i++ { // 20 complete opening/body/closing cycles.
		phase, task := s.Phase, s.Task()
		if task.Common {
			t.Fatal("common content became an independent opening/body task", task)
		}
		ok, err := s.AddCandidate(candidate(fmt.Sprintf("第%d段隔离调度样本。", i), 100), anchorstylescore.Baseline{})
		if err != nil || !ok {
			t.Fatal("adoption failed", err, s.Status)
		}
		switch phase {
		case "body":
			counts[task.FocusLink]++
		case "opening":
			openings++
		case "closing":
			closings++
		}
	}
	if openings != 20 || closings != 20 || counts["1"]+counts["2"] != 80 || !reflect.DeepEqual(counts, s.ProductFocus.AdoptedSegments) {
		t.Fatal("existing cycle phases or quota accounting changed", openings, closings, counts)
	}
	t.Logf("20 real state-machine cycles: opening=%d body=%v closing=%d common=0", openings, counts, closings)
}

func TestProductOnlyScheduleCommonCannotConsumeCounters(t *testing.T) {
	s := productOnlyFixture(t, 3, false)
	s.Phase = "body"
	common := Task{Common: true, FocusLink: "1", SellingPoint: "parking", Key: "common:parking"}
	s.recordProductAdoption(common, "共有资料")
	s.recordScheduledAdoption(common)
	if len(s.ProductFocus.AdoptedSegments) != 0 || s.ProductFocus.ScheduleCursor != 0 || len(s.PointUses) != 0 || len(s.ScheduledAngleUses) != 0 {
		t.Fatal("common content counted as a product/point adoption")
	}
}

func TestProductOnlyScheduleNoCommonFallbackWithoutProductPoints(t *testing.T) {
	s, err := New([]Task{{Common: true, SellingPoint: "parking", Angle: "location"}}, Config{Rolling: true, MaxChars: 300})
	if err != nil {
		t.Fatal(err)
	}
	s.SelectProductTask()
	if s.Status != "spacing_blocked" {
		t.Fatal("common-only material masqueraded as a product schedule", s.Status)
	}
}
