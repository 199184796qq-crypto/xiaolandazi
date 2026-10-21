package anchorcycle

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"livecompanion/management/internal/anchorstylescore"
)

func reusePolicyState() *State {
	return &State{Surface: "formal_live", Phase: "opening", Queue: []Task{{Key: "product:one:point:angle"}}, Config: Config{FreshWindowMinutes: 60}}
}
func acceptReusePolicyOutput(s *State, reused bool, phase string) {
	origin := "fresh"
	if reused {
		origin = "library"
	}
	s.Accepted = append(s.Accepted, Segment{Phase: phase, OpeningVersion: "接新版", Selected: Candidate{ReuseOrigin: origin}})
}
func startReusePolicySession(s *State) {
	s.Accepted = append(s.Accepted, Segment{Phase: "opening", OpeningVersion: "完整版", Selected: Candidate{ReuseOrigin: "fresh"}})
	s.OpeningCount = 1
	s.Phase = "body"
}
func TestFormalFirstOpeningFreshThenSevenReusedThreeNew(t *testing.T) {
	s := reusePolicyState()
	if s.ShouldReuse() {
		t.Fatal("first opening must generate live even with cache enabled")
	}
	startReusePolicySession(s)
	hits := 0
	for i := 0; i < 1000; i++ {
		s.Phase = []string{"body", "closing", "opening"}[i%3]
		want := s.ShouldReuse()
		if want {
			hits++
		}
		// Re-reading or restoring the same pending task must not consume a slot.
		raw, _ := json.Marshal(s)
		var restored State
		if json.Unmarshal(raw, &restored) != nil || restored.ShouldReuse() != want || s.ShouldReuse() != want {
			t.Fatal("retry/reload changed decision", i)
		}
		acceptReusePolicyOutput(s, want, s.Phase)
		if (i+1)%10 == 0 && hits != (i+1)*7/10 {
			t.Fatalf("after %d outputs: reused=%d", i+1, hits)
		}
		if len(s.Accepted) > 20 {
			s.AcceptedOffset += len(s.Accepted) - 20
			s.Accepted = s.Accepted[len(s.Accepted)-20:]
		}
	}
	if hits != 700 {
		t.Fatal(hits)
	}
}
func TestReuseColdMissDoesNotAccumulateUnboundedDebt(t *testing.T) {
	s := reusePolicyState()
	startReusePolicySession(s)
	for i := 0; i < 100; i++ {
		acceptReusePolicyOutput(s, false, "body")
	}
	hits := 0
	for i := 0; i < 100; i++ {
		reuse := s.ShouldReuse()
		if reuse {
			hits++
		}
		acceptReusePolicyOutput(s, reuse, "body")
	}
	if hits != 70 {
		t.Fatalf("cold misses must not cause an all-cache catch-up: %d", hits)
	}
}
func TestReuseCountersSeparateFailuresUnknownAndSpecialTasks(t *testing.T) {
	s := reusePolicyState()
	startReusePolicySession(s)
	acceptReusePolicyOutput(s, false, "body")
	acceptReusePolicyOutput(s, true, "body")
	s.Accepted[len(s.Accepted)-1].DeliveryStatus = &SegmentDelivery{Status: "failed"}
	acceptReusePolicyOutput(s, true, "closing")
	s.Accepted[len(s.Accepted)-1].DeliveryStatus = &SegmentDelivery{Status: "unconfirmed"}
	acceptReusePolicyOutput(s, true, "body")
	s.Accepted[len(s.Accepted)-1].Task.ProductMode = "auxiliary"
	total, reused := s.MainlineReuseWindow()
	if total != 2 || reused != 1 {
		t.Fatal(total, reused)
	}
	s.Config.MaxReuse = -1
	if s.ShouldReuse() {
		t.Fatal("explicit cache disable lost")
	}
}
func TestFormalPoolKeysSeparatePhaseAndOpeningVersion(t *testing.T) {
	s := reusePolicyState()
	initial := s.ReuseTaskKey()
	s.OpeningCount = 1
	later := s.ReuseTaskKey()
	s.Phase = "body"
	body := s.ReuseTaskKey()
	s.Phase = "closing"
	closing := s.ReuseTaskKey()
	keys := map[string]bool{initial: true, later: true, body: true, closing: true}
	if len(keys) != 4 {
		t.Fatal("different roles shared a pool key", keys)
	}
}
func TestReuseMixDoesNotChangeProductPointAngleOrPublicScheduling(t *testing.T) {
	fresh := productOnlyFixture(t, 3, false)
	fresh.Surface = "formal_live"
	fresh.RunID = "same-weight-mix-proof"
	fresh.ProductFocus.ResetRun()
	fresh.SelectProductTask()
	raw, _ := json.Marshal(fresh)
	var mixed State
	if e := json.Unmarshal(raw, &mixed); e != nil {
		t.Fatal(e)
	}
	body, public := 0, 0
	products := map[string]int{}
	eligible, reuses := 0, 0
	for i := 0; i < 1800; i++ {
		a, b := fresh.Task(), mixed.Task()
		if a.Key != b.Key || a.FocusLink != b.FocusLink || fresh.Phase != mixed.Phase || a.Common != b.Common {
			t.Fatal("cache mix changed selected task", i, a, b)
		}
		if mixed.Phase == "body" {
			body++
			if b.Common {
				public++
			} else {
				products[b.FocusLink]++
			}
		}
		reuse := mixed.ShouldReuse()
		if !mixed.FirstLiveOpening() {
			eligible++
			if reuse {
				reuses++
			}
		}
		one := candidate(fmt.Sprintf("第%d段不同的方案任务验证文字。", i), 100)
		one.ReuseOrigin = "fresh"
		two := one
		if reuse {
			two.ReuseOrigin = "library"
		}
		for _, item := range []struct {
			s *State
			c Candidate
		}{{&fresh, one}, {&mixed, two}} {
			ok, e := item.s.AddCandidate(item.c, anchorstylescore.Baseline{})
			if e != nil || !ok {
				t.Fatal("adoption failed", i, e, item.s.Status)
			}
		}
	}
	if math.Abs(float64(public)/float64(body)-.2) > .002 {
		t.Fatal("public allocation", public, body)
	}
	if math.Abs(float64(products["2"])/float64(products["1"])-1.2) > .03 {
		t.Fatal("product weights", products)
	}
	if math.Abs(float64(reuses)/float64(eligible)-.7) > .002 {
		t.Fatal("mix", reuses, eligible)
	}
	t.Logf("same tasks across fresh/mixed: public=%d/%d products=%v reuse=%d/%d", public, body, products, reuses, eligible)
}
