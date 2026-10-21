package anchorcycle

import (
	"math"
	"strconv"
	"strings"
)

// Select only the next slot. Bounded jitter changes close decisions, while a
// growing deficit always outranks randomness. Exclusions do not change targets.
func rollingDeficitChoice(keys, allowed []string, shares map[string]float64, counts map[string]int, seed string) string {
	if len(allowed) == 0 {
		return ""
	}
	total, mass := 0, 0.0
	var history strings.Builder
	for _, key := range keys {
		total += counts[key]
		mass += shares[key]
		history.WriteString(strconv.Quote(key) + ":" + strconv.Itoa(counts[key]) + ";")
	}
	if mass <= 0 {
		return allowed[0]
	}
	seed += ":" + history.String()
	if total == 0 {
		// Independent short sessions must not always start at the largest share.
		availableMass := 0.0
		for _, key := range allowed {
			availableMass += shares[key]
		}
		draw := schedulerSeed(seed, "first") * availableMass
		for _, key := range allowed {
			draw -= shares[key]
			if draw < 0 {
				return key
			}
		}
		return allowed[len(allowed)-1]
	}
	chosen, best := "", math.Inf(-1)
	for _, key := range allowed {
		deficit := float64(total+1)*shares[key]/mass - float64(counts[key])
		score := deficit + 1.25*schedulerSeed(seed, key)
		if score > best {
			chosen, best = key, score
		}
	}
	return chosen
}

func (s *State) availableProducts() []string {
	var keys []string
	for _, product := range s.ProductFocus.Products {
		for _, task := range s.Queue {
			scope, ok := s.ProductFocus.Tasks[task.Key]
			if ok && !task.Common && scopeSchedulable(scope, product.LinkKey, s.ProductFocus.Policy) &&
				!s.P0SkippedKeys[task.Key] && !s.DuplicateSkippedKeys[task.Key] && materialAvailable(task) {
				keys = append(keys, product.LinkKey)
				break
			}
		}
	}
	return keys
}

// Accepted audio reserves playback order, including prefetched audio. Waiting
// for PCM before counting would let the producer queue an unlimited streak.
// A definite delivery failure is excluded; an uncertain receipt remains counted.
func (s *State) productStreak() (string, int) {
	last, count := "", 0
	for i := len(s.Accepted) - 1; i >= 0; i-- {
		seg := s.Accepted[i]
		if seg.DeliveryStatus != nil && seg.DeliveryStatus.Status == "failed" {
			continue
		}
		if seg.Task.Common || seg.Task.FocusLink == "" {
			break
		}
		if last != "" && last != seg.Task.FocusLink {
			break
		}
		last = seg.Task.FocusLink
		count++
	}
	return last, count
}

func (s *State) productPresentationCounts() map[string]int {
	if s.ProductFocus.PresentedSegments != nil {
		return s.ProductFocus.PresentedSegments
	}
	// Compatibility with a Redis state created by the previous scheduler.
	counts := map[string]int{}
	for _, seg := range s.Accepted {
		if !seg.Task.Common && seg.Task.FocusLink != "" && (seg.DeliveryStatus == nil || seg.DeliveryStatus.Status != "failed") {
			counts[seg.Task.FocusLink]++
		}
	}
	return counts
}
