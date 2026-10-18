package anchorcycle

import "strconv"

const (
	productContentKind  = "product"
	commonContentKind   = "common"
	commonScheduleOwner = "common"
)

// Public material has one global 20% share, independent of the number of
// products, points or angles. Product weights divide only the other 80%.
// A five-slot shuffled window crosses small-cycle boundaries and persists in
// Redis. Only accepted pool body tasks advance it; retries and continuations do
// not. Opening/closing retain their existing product scope and cycle rules.
func (s *State) prepareContentSelection() {
	s.PendingContentKind = productContentKind
	if s.Phase != "body" || s.PriceHook != nil {
		return
	}
	product, common := false, false
	for _, task := range s.Queue {
		if !s.taskScopeEligible(task) || s.P0SkippedKeys[task.Key] || s.DuplicateSkippedKeys[task.Key] {
			continue
		}
		if task.Common {
			common = true
		} else {
			product = true
		}
	}
	// Public items cannot disguise a product without any own directions.
	// If public directions are absent/rejected, continue product supply.
	if !product || !common {
		// Unavailable public material never incurs a debt to repay later. Start a
		// new balancing period when both pools become available again.
		s.ContentScheduleBase = nil
		return
	}
	if s.ContentScheduleBase == nil {
		s.ContentScheduleBase = map[string]int{
			productContentKind: s.ContentAdopted[productContentKind],
			commonContentKind:  s.ContentAdopted[commonContentKind],
		}
	}
	if s.ContentSchedule == nil {
		s.ContentSchedule = &WeightedSchedule{}
	}
	bag := s.ContentSchedule
	if bag.Cursor >= len(bag.Items) {
		bag.Window++
		counts := map[string]int{}
		for _, kind := range []string{productContentKind, commonContentKind} {
			counts[kind] = max(0, s.ContentAdopted[kind]-s.ContentScheduleBase[kind])
		}
		bag.Items = weightedWindowSize([]string{productContentKind, commonContentKind},
			map[string]float64{productContentKind: .8, commonContentKind: .2}, counts,
			s.RunID+":content:"+strconv.Itoa(bag.Window), 5)
		bag.Cursor = 0
	}
	s.PendingContentKind = bag.Items[bag.Cursor]
}

func (s *State) selectionScheduleOwner() string {
	if s.PendingContentKind == commonContentKind && s.Phase == "body" {
		return commonScheduleOwner
	}
	if s.ProductFocus != nil && s.ProductFocus.CurrentLink != "" {
		return s.ProductFocus.CurrentLink
	}
	for _, task := range s.Queue {
		if !task.Common {
			return s.schedulingOwner(task)
		}
	}
	return "single"
}
