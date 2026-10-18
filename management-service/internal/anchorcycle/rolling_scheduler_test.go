package anchorcycle

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"livecompanion/management/internal/anchorstylescore"
)

func TestRollingProductsCrossPhaseStreakAndBodyTargets(t *testing.T) {
	for _, publicPoints := range []int{0, 3} {
		for seed := 0; seed < 6; seed++ {
			t.Run(fmt.Sprintf("public%d/seed%d", publicPoints, seed), func(t *testing.T) {
				s := productOnlyFixture(t, publicPoints, false)
				s.RunID = fmt.Sprintf("cross-phase-%d", seed)
				s.ProductFocus.ResetRun()
				s.SelectProductTask()
				counts := map[string]int{}
				public, body, streak, longest, last := 0, 0, 0, 0, ""
				for i := 0; i < 1800; i++ {
					phase, task := s.Phase, s.Task()
					if task.Common {
						last, streak = "", 0
					} else {
						if task.FocusLink == last {
							streak++
						} else {
							last, streak = task.FocusLink, 1
						}
						longest = max(longest, streak)
						if streak > 2 {
							t.Fatalf("cross-phase streak %d at %d/%s: %s", streak, i, phase, last)
						}
					}
					if phase == "body" {
						body++
						if task.Common {
							public++
						} else {
							counts[task.FocusLink]++
						}
					}
					ok, err := s.AddCandidate(candidate(fmt.Sprintf("第%d段独立验证文案。", i), 100), anchorstylescore.Baseline{})
					if err != nil || !ok || s.Status != "ready" {
						t.Fatal("real phase transition failed", err, s.Status)
					}
					if i == 127 || i == 900 {
						raw, _ := json.Marshal(s)
						var restored State
						if err := json.Unmarshal(raw, &restored); err != nil {
							t.Fatal(err)
						}
						s = restored
					}
					// Match production history trimming without losing lifetime debt.
					if len(s.Accepted) > 20 {
						s.AcceptedOffset += len(s.Accepted) - 20
						s.Accepted = s.Accepted[len(s.Accepted)-20:]
					}
				}
				if publicPoints > 0 && public*5 != body || publicPoints == 0 && public != 0 {
					t.Fatal("public share changed", public, body)
				}
				if math.Abs(float64(counts["2"])/float64(counts["1"])-1.2) > .015 {
					t.Fatal("cross-phase guard displaced body weight targets", counts)
				}
				if !reflect.DeepEqual(counts, s.ProductFocus.AdoptedSegments) {
					t.Fatal("non-body polluted body counters", counts, s.ProductFocus.AdoptedSegments)
				}
				presented := 0
				for _, count := range s.ProductFocus.PresentedSegments {
					presented += count
				}
				if presented != 1800-public {
					t.Fatal("presentation counters lost during trim/reload", presented, public)
				}
				if len(s.ProductFocus.Schedule) > 1 {
					t.Fatal("product future bag remains", s.ProductFocus.Schedule)
				}
				t.Logf("body=%v public=%d max-streak=%d", counts, public, longest)
			})
		}
	}
}

func TestRollingDeficitsBoundEveryPrefixAndRemainRandom(t *testing.T) {
	keys := []string{"a", "b", "c"}
	shares := boundedShares(keys, map[string]float64{"a": 100, "b": 10, "c": 1}, 1.3)
	firsts := map[string]bool{}
	for seed := 0; seed < 30; seed++ {
		counts := map[string]int{}
		for i := 0; i < 3000; i++ {
			chosen := rollingDeficitChoice(keys, keys, shares, counts, fmt.Sprint(seed))
			if i == 0 {
				firsts[chosen] = true
			}
			counts[chosen]++
			for _, key := range keys {
				if math.Abs(float64(counts[key])-float64(i+1)*shares[key]) > 3 {
					t.Fatal("randomness outweighed cumulative debt", seed, i, counts)
				}
			}
		}
	}
	if len(firsts) != len(keys) {
		t.Fatal("fixed first selection", firsts)
	}
}

func TestRollingGuardUnavailableProductAndOnlyOneProduct(t *testing.T) {
	for _, n := range []int{1, 2} {
		s := mixFixture(t, n, "one-usable-product")
		if n == 1 {
			// Exercise the modern scoped one-product path, not just legacy nil.
			s.ProductFocus = &ProductFocus{Products: []FocusProduct{{LinkKey: "1"}}, Tasks: ProductTaskScopes(s.Queue)}
		} else {
			for _, task := range s.Queue {
				if task.LinkKey == "2" {
					if s.P0SkippedKeys == nil {
						s.P0SkippedKeys = map[string]bool{}
					}
					s.P0SkippedKeys[task.Key] = true
				}
			}
		}
		s.ProductFocus.ResetRun()
		for i := 0; i < 50; i++ {
			task, _ := simulateBody(&s)
			if s.Status != "ready" || task.FocusLink != "1" {
				t.Fatal("streak guard blocked the only usable product", n, s.Status, task)
			}
		}
	}
}

func TestRollingGuardFailedAndUncertainDelivery(t *testing.T) {
	s := mixFixture(t, 2, "receipt-order")
	s.Surface = "formal_live"
	s.Accepted = []Segment{
		{Task: Task{FocusLink: "1"}, Selected: Candidate{ID: "a"}},
		{Task: Task{FocusLink: "2"}, Selected: Candidate{ID: "failed"}},
		{Task: Task{FocusLink: "1"}, Selected: Candidate{ID: "uncertain"}},
	}
	s.ProductFocus.PresentedSegments = map[string]int{"1": 2, "2": 1}
	s.RecordSegmentDeliveryFailure("failed", "tts", "definite failure", true, time.Now())
	s.RecordSegmentDeliveryFailure("uncertain", "core", "timeout", false, time.Now())
	last, streak := s.productStreak()
	if last != "1" || streak != 2 || s.ProductFocus.PresentedSegments["2"] != 0 || s.ProductFocus.PresentedSegments["1"] != 2 {
		t.Fatal("receipt semantics changed", last, streak, s.ProductFocus.PresentedSegments)
	}
	s.ProductFocus.SelectionSet = false
	s.ProductFocus.CurrentLink = ""
	s.SelectProductTask()
	if s.Task().FocusLink != "2" {
		t.Fatal("prefetched/uncertain segments were not included in guard", s.Task())
	}
}

func TestRollingClosingNewProductKeepsItsOwnFactScope(t *testing.T) {
	s := productOnlyFixture(t, 1, false)
	scopes := map[string]*TaskFactScope{}
	for _, key := range []string{"1", "2"} {
		scopes[key] = &TaskFactScope{Facts: []HookFact{{Data: key + "号商品专属事实"}}}
	}
	for i := range s.Queue {
		if !s.Queue[i].Common {
			s.Queue[i].FactScope = scopes[s.Queue[i].LinkKey]
		}
	}
	// A shared task carrying product 2 as its context must never supply the
	// closing's scope. The closing switches to 2 because product 1 ran twice.
	s.Accepted = []Segment{
		{Task: Task{Common: true, FocusLink: "2", FactScope: &TaskFactScope{Facts: []HookFact{{Data: "不应继承的公共整包"}}}}},
		{Task: Task{FocusLink: "1", FactScope: scopes["1"]}},
		{Task: Task{FocusLink: "1", FactScope: scopes["1"]}},
	}
	s.Phase = "closing"
	task := s.Task()
	if task.FocusLink != "2" || task.FactScope != scopes["2"] || !reflect.DeepEqual(task.ProductLinks(), []string{"2"}) {
		t.Fatal("closing borrowed another scope", task)
	}
	facts := s.ProductFacts(task)
	if facts != "共有停车资料\n二号专属" {
		t.Fatal("closing facts changed ownership", facts)
	}
	for i := 0; i < 5; i++ {
		if s.Task() != task {
			t.Fatal("closing retry changed scope")
		}
	}
}

func TestRollingOldBagsRetireWithoutResettingAdoptedDebt(t *testing.T) {
	s := mixFixture(t, 2, "old-runtime")
	p := s.ProductFocus
	p.AdoptedSegments = map[string]int{"1": 6, "2": 10}
	p.Schedule, p.ScheduleCursor = []string{"2", "2", "2", "2", "1"}, 2
	p.CurrentSlot, p.SelectionSet = 2, false
	before := s.Task()
	// The in-flight product is retained even when an old future batch exists.
	if s.Task() != before {
		t.Fatal("legacy retry rerolled")
	}
	s.Phase = "body"
	simulateBody(&s)
	s.SelectProductTask()
	if len(p.Schedule) != 1 || p.AdoptedSegments["1"]+p.AdoptedSegments["2"] != 17 {
		t.Fatal("old batch retained or debt reset", p.Schedule, p.AdoptedSegments)
	}
	for _, bag := range s.PointSchedules {
		if len(bag.Items) > 1 {
			t.Fatal("point future batch retained", bag)
		}
	}
}

func TestRollingResumeCannotRetargetPreviousProductsAngle(t *testing.T) {
	s := productOnlyFixture(t, 1, false)
	for i, task := range s.Queue {
		if task.LinkKey == "2" {
			s.TaskIndex = i
			break
		}
	}
	s.ProductFocus.CurrentLink = "2"
	s.ProductFocus.PresentedSegments = map[string]int{"2": 10}
	s.Accepted = append(s.Accepted, Segment{Task: Task{ProductMode: "lecture"}})
	s.Phase = "opening"
	task := s.Task()
	if task.FocusLink != "1" || task.LinkKey != "1" {
		t.Fatal("resume retargeted another product's angle", task)
	}
}

func TestRollingAllSkippedRecoveryAndMissingMaterial(t *testing.T) {
	s := mixFixture(t, 2, "all-skipped")
	s.Surface = "formal_live"
	s.ProductFocus.ResetRun()
	s.P0SkippedKeys = map[string]bool{}
	for _, task := range s.Queue {
		s.P0SkippedKeys[task.Key] = true
	}
	s.SelectProductTask()
	if s.Status != "ready" || s.Task().FocusLink == "" || s.Task().FocusLink != s.Task().LinkKey {
		t.Fatal("existing formal recovery lost product scope", s.Status, s.Task())
	}
	for i := range s.Queue {
		if s.Queue[i].LinkKey == "2" {
			s.Queue[i].FactScope = &TaskFactScope{}
		}
	}
	s.ProductFocus.ResetRun()
	for i := 0; i < 50; i++ {
		task, _ := simulateBody(&s)
		if s.Status != "ready" || task.FocusLink != "1" {
			t.Fatal("missing material selected or supply stopped", s.Status, task)
		}
	}
}
