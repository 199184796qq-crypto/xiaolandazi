package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorstylescore"
)

// Explicit local archive replay only. Candidates/approval are synthetic. This
// validates actual saved task inputs and production state transitions, not model
// quality, TTS latency or real Core playback. No network or database is used.
func TestArchivedPlanMainlineReuseReplay(t *testing.T) {
	path := os.Getenv("MAINLINE_REUSE_REPLAY_INPUT")
	if path == "" {
		t.Skip("explicit local archived State is required")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var original anchorcycle.State
	if e = json.Unmarshal(raw, &original); e != nil || len(original.Queue) == 0 || original.ProductFocus == nil {
		t.Fatal("invalid saved task state", e)
	}
	saved := anchorcycle.ProductDigest(original.ProductFocus.Products)
	type output struct {
		Mode                                    string `json:"mode"`
		SourceSHA                               string `json:"source_sha"`
		Cycles, Total, Eligible, Reused, Misses int
		Body, Public, LongestProductStreak      int
		Products, Points, Angles                map[string]int
	}
	results := []output{}
	for _, cold := range []bool{false, true} {
		s, err := anchorcycle.New(original.Queue, original.Config)
		if err != nil {
			t.Fatal(err)
		}
		s.Surface, s.RunID = "formal_live", "archived-plan-reuse-replay"
		// New initially uses a random run ID to choose the first body length.
		// Fix only that first target; later cycles use the stable replay ID.
		s.BodyTarget = s.Config.BodyMin
		focus, _ := json.Marshal(original.ProductFocus)
		if err = json.Unmarshal(focus, &s.ProductFocus); err != nil {
			t.Fatal(err)
		}
		s.ProductFocus.ResetRun()
		s.FissionSides, s.FactSideMap = original.FissionSides, original.FactSideMap
		s.SelectProductTask()
		r := output{Mode: "warm matching-cache policy simulation", SourceSHA: anchorcycle.ProductDigest(string(raw)), Products: map[string]int{}, Points: map[string]int{}, Angles: map[string]int{}}
		if cold {
			r.Mode = "cold cache simulation; exact task/phase/side, 3 variants, gap 3"
		}
		// A deliberately small local inventory models misses. A hit must match
		// the exact task, phase and side; no cross-product borrowing is allowed.
		inventory := map[string][]int{}
		last, streak := "", 0
		for s.CycleNumber <= 1000 {
			if s.Status != "ready" {
				t.Fatal(s.Status, s.Error)
			}
			phase, task, first := s.Phase, s.Task(), s.FirstLiveOpening()
			key, side := s.ReuseTaskKey(), s.CurrentFissionSide()
			if task.Common {
				last, streak = "", 0
			} else {
				if task.FocusLink == last {
					streak++
				} else {
					last, streak = task.FocusLink, 1
				}
				r.LongestProductStreak = max(r.LongestProductStreak, streak)
			}
			if phase == "body" {
				r.Body++
				if task.Common {
					r.Public++
				} else {
					r.Products[task.FocusLink]++
					r.Points[task.FocusLink+" / "+task.SellingPoint]++
					r.Angles[task.Key]++
				}
			}
			reuse := s.ShouldReuse()
			if first && reuse {
				t.Fatal("first opening reused")
			}
			if !first {
				r.Eligible++
			}
			if reuse && cold {
				hit := -1
				for i, used := range inventory[key] {
					if r.Total-used > 3 {
						hit = i
						break
					}
				}
				if hit < 0 {
					reuse = false
					r.Misses++
				} else {
					inventory[key][hit] = r.Total
				}
			}
			origin := "fresh"
			if reuse {
				origin = "library"
				r.Reused++
			} else if len(inventory[key]) < 3 {
				inventory[key] = append(inventory[key], r.Total)
			}
			c := anchorcycle.Candidate{ID: fmt.Sprint(r.Total), Text: fmt.Sprintf("第%d段仅供调度验证的占位正文。", r.Total), ReuseOrigin: origin, FissionSide: side, Score: anchorstylescore.Result{OverallScore: 100}}
			ok, err := s.AddCandidate(c, anchorstylescore.Baseline{})
			if err != nil || !ok {
				t.Fatal("real state transition failed", err, s.Status)
			}
			r.Total++
			if phase == "closing" {
				r.Cycles++
			}
			if len(s.Accepted) > 20 {
				s.AcceptedOffset += len(s.Accepted) - 20
				s.Accepted = s.Accepted[len(s.Accepted)-20:]
			}
			if r.Total == 37 {
				blob, _ := json.Marshal(s)
				if err = json.Unmarshal(blob, &s); err != nil {
					t.Fatal(err)
				}
			}
		}
		if r.Cycles != 1000 || r.LongestProductStreak > 2 || saved != anchorcycle.ProductDigest(s.ProductFocus.Products) {
			t.Fatal("saved plan changed or schedule broken", r)
		}
		if math.Abs(float64(r.Public)/float64(r.Body)-.2) > .002 {
			t.Fatal("public quota", r.Public, r.Body)
		}
		if !cold && math.Abs(float64(r.Reused)/float64(r.Eligible)-.7) > .001 {
			t.Fatal("warm mix", r.Reused, r.Eligible)
		}
		for _, task := range original.Queue {
			if !task.Common && r.Angles[task.Key] == 0 {
				t.Fatal("saved angle unused", task.Key)
			}
		}
		t.Logf("%s: cycles=%d total=%d body=%d public=%d products=%v reused=%d/%d misses=%d max-streak=%d", r.Mode, r.Cycles, r.Total, r.Body, r.Public, r.Products, r.Reused, r.Eligible, r.Misses, r.LongestProductStreak)
		results = append(results, r)
	}
	if p := os.Getenv("MAINLINE_REUSE_REPLAY_OUTPUT"); p != "" {
		blob, _ := json.MarshalIndent(results, "", "  ")
		if e = os.WriteFile(p, blob, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
