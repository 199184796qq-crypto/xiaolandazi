package anchorcycle

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"

	"livecompanion/management/internal/anchorstylescore"
)

func mixFixture(t *testing.T, n int, run string) State {
	t.Helper()
	products := []FocusProduct{}
	for i := 1; i <= n; i++ {
		roles := []string{"ordinary"}
		if i == 1 {
			roles = []string{"main"}
		}
		p := FocusProduct{LinkKey: fmt.Sprint(i), Roles: roles}
		for j, weight := range []int{100, 30, 1} {
			sp := FocusSellingPoint{Name: fmt.Sprintf("point%d", j), Weight: weight}
			angles := 2
			if j == 1 {
				angles = 20
			}
			for k := 0; k < angles; k++ {
				sp.Angles = append(sp.Angles, FocusSellingAngle{Angle: fmt.Sprintf("angle%d", k), Weight: 1})
			}
			p.SellingPoints = append(p.SellingPoints, sp)
		}
		products = append(products, p)
	}
	policy := ProductPolicy{Version: "weighted-mix-v1"}
	tasks := ExpandProductSellingPoints(products, &policy)
	s, err := New(tasks, Config{Rolling: true, BodyMin: 4, BodyMax: 4, MaxChars: 300, Threshold: 40, CombinationGap: 8, PointGap: 2, MinReappearCycles: 2})
	if err != nil {
		t.Fatal(err)
	}
	s.RunID = run
	s.PointSchedules = nil
	if n > 1 {
		s.ProductFocus = &ProductFocus{Policy: policy, Products: products, Tasks: map[string]ScopedTask{}}
		for _, task := range tasks {
			s.ProductFocus.Tasks[task.Key] = ScopedTask{Links: []string{task.LinkKey}}
		}
		s.ProductFocus.ResetRun()
	}
	s.SelectProductTask()
	return s
}

func simulateBody(s *State) (Task, string) {
	s.Phase = "body"
	s.SelectProductTask()
	task := s.Task()
	side := s.CurrentFissionSide()
	s.recordProductAdoption(task, "")
	s.recordScheduledAdoption(task)
	u := s.Used[task.Key]
	u.Count++
	u.LastIndex = s.TotalAccepted() + 1
	u.LastCycle = s.CycleNumber
	s.Used[task.Key] = u
	s.recordFissionSide(task.Key, side)
	s.Accepted = append(s.Accepted, Segment{Phase: "body", Task: task, Selected: Candidate{FissionSide: side}})
	// Exercise bounded recent history: lifetime counters must not depend on it.
	if len(s.Accepted) > 8 {
		s.AcceptedOffset++
		s.Accepted = s.Accepted[1:]
	}
	return task, side
}

func TestWeightedMixLongSessionProductsPointsAndAngles(t *testing.T) {
	for _, n := range []int{1, 2, 3, 10} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s := mixFixture(t, n, "long-session")
			productCounts, pointCounts := map[string]int{}, map[string]int{}
			angleCounts := map[string]int{}
			for i := 0; i < 24000; i++ {
				task, _ := simulateBody(&s)
				productCounts[task.LinkKey]++
				pointCounts[taskPointKey(task)]++
				angleCounts[task.Key]++
				s.CycleNumber = i/4 + 1
			}
			low, high := math.MaxInt, 0
			for _, count := range productCounts {
				low = min(low, count)
				high = max(high, count)
			}
			if float64(high)/float64(low) > 1.205 {
				t.Fatal("product mix outside cap", productCounts)
			}
			for i := 1; i <= n; i++ {
				key := fmt.Sprint(i)
				high, low := pointCounts[key+"×point0"], pointCounts[key+"×point2"]
				if high <= low || float64(high)/float64(low) > 1.34 {
					t.Fatal("point mix outside 30% preference", pointCounts)
				}
				// Twenty angles do not give point1 extra share, and all angles are used.
				mid := pointCounts[key+"×point1"]
				if mid < low-25 || mid > high+25 {
					t.Fatal("angle count changed parent share", pointCounts)
				}
				for k := 0; k < 20; k++ {
					if angleCounts[key+"×point1×"+fmt.Sprintf("angle%d", k)] == 0 {
						t.Fatal("angle starved", key, k)
					}
				}
			}
			t.Logf("products=%v point1=%v", productCounts, map[string]int{"high": pointCounts["1×point0"], "mid": pointCounts["1×point1"], "low": pointCounts["1×point2"]})
		})
	}
}

func TestWeightedMixSmallCyclesAreNotFixedQuotas(t *testing.T) {
	s := mixFixture(t, 2, "random-small-cycles")
	mixes := map[int]bool{}
	pointMixes := map[string]bool{}
	for cycle := 0; cycle < 150; cycle++ {
		ones := 0
		points := map[string]int{}
		for j := 0; j < 4; j++ {
			task, _ := simulateBody(&s)
			if task.LinkKey == "1" {
				ones++
			}
			points[task.SellingPoint]++
		}
		mixes[ones] = true
		pointMixes[fmt.Sprint(points)] = true
		s.CycleNumber++
	}
	if !mixes[4] || !mixes[3] || !mixes[1] || len(pointMixes) < 5 {
		t.Fatal("small cycles became a rigid alternation", mixes, pointMixes)
	}
	t.Logf("four-body cycle product mixes=%v point mixes=%d", mixes, len(pointMixes))
}

func TestWeightedMixManyShortSessionsHaveNoFixedFirstProduct(t *testing.T) {
	counts := map[string]int{}
	for run := 0; run < 1200; run++ {
		s := mixFixture(t, 2, fmt.Sprintf("short-%d", run))
		task, _ := simulateBody(&s)
		counts[task.LinkKey]++
	}
	share := float64(counts["1"]) / 1200
	if share < 0.50 || share > 0.59 {
		t.Fatal("fixed session prefix biased product share", counts)
	}
	t.Logf("first bodies over 1200 independent sessions=%v", counts)
}

func TestWeightedMixRetriesAndRedisKeepPendingChoice(t *testing.T) {
	s := mixFixture(t, 2, "retry-and-reload")
	for i := 0; i < 19; i++ {
		simulateBody(&s)
	}
	s.SelectProductTask()
	task, side := s.Task(), s.CurrentFissionSide()
	raw, _ := json.Marshal(s)
	var restored State
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		restored.SelectProductTask()
		if restored.Task() != task || restored.CurrentFissionSide() != side {
			t.Fatal("retry/reload rerolled pending selection")
		}
	}
	after, _ := json.Marshal(restored)
	if string(raw) != string(after) {
		t.Fatal("retry consumed a quota or changed a pending window")
	}
}

func TestWeightedMixSideRotationIsIndependentAndStable(t *testing.T) {
	s := mixFixture(t, 1, "sides")
	s.FissionSides = []string{"A：first", "B：second", "C：third"}
	counts := map[string]map[string]int{}
	for i := 0; i < 1200; i++ {
		task, side := simulateBody(&s)
		if counts[task.Key] == nil {
			counts[task.Key] = map[string]int{}
		}
		counts[task.Key][side]++
	}
	for key, c := range counts {
		lo, hi := math.MaxInt, 0
		for _, side := range s.FissionSides {
			lo = min(lo, c[side])
			hi = max(hi, c[side])
		}
		if hi-lo > 1 {
			t.Fatal("side rotation lost per-angle balance", key, c)
		}
	}
}

func TestWeightedMixRealPhaseTransitionsAndNoScopeLeak(t *testing.T) {
	s := mixFixture(t, 2, "phase-transition")
	bodyCounts := map[string]int{}
	closingCount, openingCount := 0, 0
	cycleLinks := map[int]map[string]bool{}
	for i := 0; i < 600; i++ {
		phase, cycle, task := s.Phase, s.CycleNumber, s.Task()
		if task.FocusLink != task.LinkKey && phase != "closing" {
			t.Fatal("selected angle escaped its product", task)
		}
		for retry := 0; retry < 2; retry++ {
			if s.Task() != task {
				t.Fatal("retry changed task")
			}
		}
		text := fmt.Sprintf("这是第%d个已核对的讲解。", i)
		c := candidate(text, 100)
		c.FissionSide = s.CurrentFissionSide()
		adopted, err := s.AddCandidate(c, anchorstylescore.Baseline{})
		if err != nil || !adopted {
			t.Fatal(err, s.Status)
		}
		switch phase {
		case "opening":
			openingCount++
		case "closing":
			closingCount++
		case "body":
			bodyCounts[task.FocusLink]++
			if cycleLinks[cycle] == nil {
				cycleLinks[cycle] = map[string]bool{}
			}
			cycleLinks[cycle][task.FocusLink] = true
		}
	}
	if openingCount != 100 || closingCount != 100 || bodyCounts["1"]+bodyCounts["2"] != 400 {
		t.Fatal("cycle phases changed", openingCount, closingCount, bodyCounts)
	}
	if !reflect.DeepEqual(bodyCounts, s.ProductFocus.AdoptedSegments) {
		t.Fatal("opening/closing polluted body quotas")
	}
	mixed := false
	for _, links := range cycleLinks {
		mixed = mixed || len(links) > 1
	}
	if !mixed {
		t.Fatal("product was incorrectly locked to each small cycle")
	}
}

func TestWeightedMixBoundedSharesAndIndependentInputs(t *testing.T) {
	keys := []string{"a", "b", "c"}
	shares := boundedShares(keys, map[string]float64{"a": 100, "b": 1, "c": 1}, PointFrequencyRatio)
	if math.Abs(shares["a"]/shares["b"]-1.3) > 1e-9 || shares["b"] != shares["c"] {
		t.Fatal(shares)
	}
	for _, ratio := range []float64{ProductFrequencyRatio, PointFrequencyRatio} {
		equal := boundedShares(keys, map[string]float64{"a": 1, "b": 1, "c": 1}, ratio)
		if equal["a"] != equal["b"] || equal["b"] != equal["c"] {
			t.Fatal(equal)
		}
	}
	p := &ProductFocus{Products: []FocusProduct{{LinkKey: "a", Roles: []string{"main"}}, {LinkKey: "b", Roles: []string{"ordinary"}}}}
	first := p.normalizedProductWeights()
	if math.Abs(first["a"]/first["b"]-1.2) > 1e-9 {
		t.Fatal(first)
	}
	p.Products[1].FrequencyWeight = 200
	changed := p.normalizedProductWeights()
	if changed["b"] <= changed["a"] || changed["b"]/changed["a"] > 1.2+1e-9 {
		t.Fatal("manual coefficient did not participate", changed)
	}
}

func TestWeightedMixFailedDraftAndUnavailableAngleDoNotEscapeProduct(t *testing.T) {
	s := mixFixture(t, 2, "failure")
	s.Config.MaxRetries = 3
	s.Phase = "body"
	task, side := s.Task(), s.CurrentFissionSide()
	if adopted, err := s.AddCandidate(candidate("未达到采纳条件的稿件", 1), anchorstylescore.Baseline{}); err != nil || adopted {
		t.Fatal("failed draft unexpectedly adopted", err)
	}
	if s.Task() != task || s.CurrentFissionSide() != side || len(s.PointUses) != 0 || s.ProductFocus.ScheduleCursor != 0 {
		t.Fatal("failed draft advanced the pending scheduling slot")
	}
	// Exhaust a whole point. Selection may move within this product, never
	// borrow another product's angle to satisfy a cooldown or missing quota.
	s.P0SkippedKeys = map[string]bool{}
	for _, item := range s.Queue {
		if item.SellingPoint == task.SellingPoint && item.LinkKey == task.LinkKey {
			s.P0SkippedKeys[item.Key] = true
		}
	}
	s.SelectProductTask()
	if s.Status != "ready" || s.Task().LinkKey != task.LinkKey || s.Task().SellingPoint == task.SellingPoint {
		t.Fatal("unavailable point caused cross-product fallback", s.Task())
	}
	if len(s.PointUses) != 0 || s.ProductFocus.ScheduleCursor != 0 {
		t.Fatal("skipping an unavailable direction consumed an adopted quota")
	}
}

func TestWeightedMixFreshSessionDoesNotInheritProgress(t *testing.T) {
	s := mixFixture(t, 2, "first-run")
	for i := 0; i < 80; i++ {
		simulateBody(&s)
	}
	fresh := mixFixture(t, 2, "next-run")
	if len(fresh.PointUses) != 0 || len(fresh.ScheduledAngleUses) != 0 || len(fresh.SideUses) != 0 || len(fresh.ProductFocus.AdoptedSegments) != 0 || fresh.ProductFocus.ScheduleCursor != 0 {
		t.Fatal("new live session inherited old scheduling debt")
	}
}

func TestWeightedMixDuplicateRotationKeepsPendingProductAndPoint(t *testing.T) {
	s := mixFixture(t, 2, "duplicate-rotation")
	s.Phase = "body"
	before := s.Task()
	if !s.RotateAfterDuplicate() {
		t.Fatal("point has another angle")
	}
	after := s.Task()
	if after.Key == before.Key || after.SellingPoint != before.SellingPoint || after.LinkKey != before.LinkKey || after.FocusLink != after.LinkKey {
		t.Fatal("duplicate rotation escaped pending product/point", before, after)
	}
	if s.ProductFocus.ScheduleCursor != 0 || len(s.PointUses) != 0 || len(s.ScheduledAngleUses) != 0 || s.PointSchedules[before.FocusLink].Cursor != 0 {
		t.Fatal("duplicate draft consumed an adopted quota")
	}
	// Exhaust this point; fallback may change point but must retain ownership.
	for _, task := range s.Queue {
		if task.LinkKey == before.LinkKey && task.SellingPoint == before.SellingPoint {
			s.DuplicateSkippedKeys[task.Key] = true
		}
	}
	if !s.RotateAfterDuplicate() || s.Task().SellingPoint == before.SellingPoint || s.Task().LinkKey != before.LinkKey || s.Task().FocusLink != before.LinkKey {
		t.Fatal("exhausted point borrowed another product", s.Task())
	}
	if s.ProductFocus.ScheduleCursor != 0 || len(s.PointUses) != 0 {
		t.Fatal("point fallback consumed a product quota")
	}
	// Restore through the real Redis representation before exhausting a product.
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	otherKey := ""
	for _, task := range s.Queue {
		if task.LinkKey == before.LinkKey {
			s.DuplicateSkippedKeys[task.Key] = true
		} else {
			otherKey = task.Key
		}
	}
	s.DuplicateSkippedKeys[otherKey] = true
	last := s.Task()
	if s.RotateAfterDuplicate() || s.Task() != last {
		t.Fatal("exhausted product must retain its pending task", s.Task())
	}
	if !s.DuplicateSkippedKeys[otherKey] || len(s.DuplicateSkippedKeys) != 1 {
		t.Fatal("exhaustion reset another product's duplicate history", s.DuplicateSkippedKeys)
	}
}
