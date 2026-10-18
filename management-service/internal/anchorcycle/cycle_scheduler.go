package anchorcycle

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"math/rand"
	"strconv"
)

type WeightedSchedule struct {
	Items  []string `json:"items,omitempty"`
	Cursor int      `json:"cursor,omitempty"`
	Window int      `json:"window,omitempty"`
}

const ProductFrequencyRatio = 1.2
const PointFrequencyRatio = 1.3

// Map operator preferences to a bounded frequency range, preserving ties and
// ordering. A raw slider value is a preference, never an unbounded multiplier.
func boundedShares(keys []string, preferences map[string]float64, maxRatio float64) map[string]float64 {
	lo, hi := math.MaxFloat64, 0.0
	for _, key := range keys {
		v := preferences[key]
		if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			v = 1
		}
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
	}
	out := map[string]float64{}
	if len(keys) == 0 {
		return out
	}
	exponent := 1.0
	if hi/lo > maxRatio {
		exponent = math.Log(maxRatio) / math.Log(hi/lo)
	}
	total := 0.0
	for _, key := range keys {
		v := preferences[key]
		if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			v = 1
		}
		out[key] = math.Pow(v/lo, exponent)
		total += out[key]
	}
	for _, key := range keys {
		out[key] /= total
	}
	return out
}

func schedulerSeed(run, key string) float64 {
	h := sha256.Sum256([]byte(run + ":" + key))
	return float64(binary.BigEndian.Uint64(h[:8])>>11) / float64(uint64(1)<<53)
}

// The public/product split retains its independent five-slot 20/80 window.
// Product and selling-point selection use rollingDeficitChoice instead.
func weightedWindowSize(keys []string, shares map[string]float64, counts map[string]int, seed string, n int) []string {
	if len(keys) == 0 {
		return nil
	}
	placed, total := map[string]int{}, 0
	for _, key := range keys {
		placed[key] = counts[key]
		total += counts[key]
	}
	bag := make([]string, 0, n)
	for i := 0; i < n; i++ {
		best, tie, chosen := math.Inf(-1), -1.0, ""
		for _, key := range keys {
			score := float64(total+i+1)*shares[key] - float64(placed[key])
			r := schedulerSeed(seed, key+":"+strconv.Itoa(i))
			if score > best+1e-9 || math.Abs(score-best) < 1e-9 && r > tie {
				best, tie, chosen = score, r, key
			}
		}
		placed[chosen]++
		bag = append(bag, chosen)
	}
	h := sha256.Sum256([]byte(seed))
	r := rand.New(rand.NewSource(int64(binary.BigEndian.Uint64(h[:8]))))
	r.Shuffle(len(bag), func(i, j int) { bag[i], bag[j] = bag[j], bag[i] })
	return bag
}

func (s *State) scheduledProduct() string {
	p := s.ProductFocus
	keys := s.availableProducts()
	counts := p.AdoptedSegments
	if s.Phase != "body" {
		counts = s.productPresentationCounts()
	}
	allowed := append([]string(nil), keys...)
	if last, streak := s.productStreak(); streak >= 2 && len(keys) > 1 {
		allowed = nil
		for _, key := range keys {
			if key != last {
				allowed = append(allowed, key)
			}
		}
	}
	chosen := rollingDeficitChoice(keys, allowed, p.normalizedProductWeights(), counts, s.RunID+":products:"+s.Phase)
	// Keep a single pending reservation, never a shuffled future batch. Cursor
	// remains an adoption counter for existing runtime consumers.
	p.Schedule = nil
	if chosen != "" {
		p.Schedule = []string{chosen}
	}
	return chosen
}

type weightedPoint struct {
	weight int
	tasks  []int
}

// Point deficits are independent of angle count and of other products.
// Retries keep the same pending point and never consume a quota.
func (s *State) chooseProductWeightedTask() bool {
	if !s.Config.Rolling || s.PriceHook != nil {
		return false
	}
	owner := s.selectionScheduleOwner()
	groups, order := map[string]*weightedPoint{}, []string{}
	for i, t := range s.Queue {
		if !s.productTaskEligible(t) || s.P0SkippedKeys[t.Key] || s.DuplicateSkippedKeys[t.Key] || !materialAvailable(t) {
			continue
		}
		key := s.schedulingPointKey(t, owner)
		if groups[key] == nil {
			groups[key] = &weightedPoint{weight: s.schedulingPointWeight(t)}
			order = append(order, key)
		}
		groups[key].tasks = append(groups[key].tasks, i)
	}
	if len(order) == 0 {
		return false
	}
	if s.PointSchedules == nil {
		s.PointSchedules = map[string]*WeightedSchedule{}
	}
	bag := s.PointSchedules[owner]
	if bag == nil {
		bag = &WeightedSchedule{}
		s.PointSchedules[owner] = bag
	}
	if len(bag.Items) > 1 || bag.Cursor >= len(bag.Items) || groups[bag.Items[bag.Cursor]] == nil {
		preferences := map[string]float64{}
		for _, key := range order {
			preferences[key] = float64(groups[key].weight)
		}
		shares := boundedShares(order, preferences, PointFrequencyRatio)
		bag.Window++
		bag.Items = []string{rollingDeficitChoice(order, order, shares, s.PointUses, s.RunID+":points:"+owner)}
		bag.Cursor = 0
	}
	g := groups[bag.Items[bag.Cursor]]
	candidates := []int{}
	for _, i := range g.tasks {
		u := s.Used[s.Queue[i].Key]
		if u.Count == 0 || ((s.Config.MinReappearCycles <= 0 || s.CycleNumber-u.LastCycle >= s.Config.MinReappearCycles) && s.TotalAccepted()-u.LastIndex >= s.Config.CombinationGap) {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		candidates = g.tasks
	}
	// Angle targets use the whole point, even while some angles cool down.
	angleWeight, angleCount := 0, 0
	for _, i := range g.tasks {
		angleWeight += s.schedulingAngleWeight(s.Queue[i])
		angleCount += s.ScheduledAngleUses[s.schedulingAngleKey(s.Queue[i], owner)]
	}
	chosen, best, tie := -1, math.Inf(-1), -1.0
	for _, i := range candidates {
		t := s.Queue[i]
		score := float64(angleCount+1)*float64(s.schedulingAngleWeight(t))/float64(angleWeight) - float64(s.ScheduledAngleUses[s.schedulingAngleKey(t, owner)])
		r := schedulerSeed(s.RunID, t.Key+":"+strconv.Itoa(angleCount))
		if score > best+1e-9 || math.Abs(score-best) < 1e-9 && r > tie {
			best, tie, chosen = score, r, i
		}
	}
	if chosen < 0 {
		return false
	}
	s.TaskIndex = chosen
	s.Status, s.Error = "ready", ""
	return true
}

func (s *State) schedulingOwner(t Task) string {
	if t.Common {
		return commonScheduleOwner
	}
	if s.ProductFocus != nil && s.ProductFocus.CurrentLink != "" {
		return s.ProductFocus.CurrentLink
	}
	if t.LinkKey != "" {
		return t.LinkKey
	}
	return "single"
}
func (s *State) schedulingPointKey(t Task, owner string) string {
	if t.Common {
		return "common:" + t.SellingPoint
	}
	return owner + ":" + t.SellingPoint
}
func (s *State) schedulingAngleKey(t Task, owner string) string {
	if t.Common {
		return "common:" + t.Key
	}
	return owner + ":" + t.Key
}
func (s *State) schedulingPointWeight(t Task) int {
	if s.ProductFocus != nil && !t.Common {
		for _, key := range []string{productSellingPointKey(s.ProductFocus.CurrentLink, t.SellingPoint), taskPointKey(t), t.SellingPoint} {
			if w := s.ProductFocus.Policy.PointWeights[key]; w > 0 {
				return w
			}
		}
	}
	if t.PointWeight > 0 {
		return t.PointWeight
	}
	return 1
}
func (s *State) schedulingAngleWeight(t Task) int {
	if s.ProductFocus != nil {
		if w := s.ProductFocus.Policy.AngleWeights[t.Key]; w > 0 {
			return w
		}
	}
	if t.AngleWeight > 0 {
		return t.AngleWeight
	}
	return 1
}

// Only adopted body content consumes selection quotas. Opening, closing,
// failed drafts and retries keep the pending slot; promised continuations are
// accounted separately by the caller's existing phase/obligation handling.
func (s *State) recordScheduledAdoption(t Task) {
	if s.Config.Rolling {
		if s.ContentAdopted == nil {
			s.ContentAdopted = map[string]int{}
		}
		kind := productContentKind
		if t.Common {
			kind = commonContentKind
		}
		s.ContentAdopted[kind]++
		if s.ContentSchedule != nil && s.ContentSchedule.Cursor < len(s.ContentSchedule.Items) {
			s.ContentSchedule.Cursor++
		}
	}
	owner := s.schedulingOwner(t)
	if s.ProductFocus != nil && !t.Common {
		s.ProductFocus.ScheduleCursor++
	}
	if s.PointUses == nil {
		s.PointUses = map[string]int{}
	}
	s.PointUses[s.schedulingPointKey(t, owner)]++
	if s.ScheduledAngleUses == nil {
		s.ScheduledAngleUses = map[string]int{}
	}
	s.ScheduledAngleUses[s.schedulingAngleKey(t, owner)]++
	if bag := s.PointSchedules[owner]; bag != nil {
		bag.Cursor++
	}
}
