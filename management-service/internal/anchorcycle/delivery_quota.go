package anchorcycle

import "time"

// A reservation records what this body actually consumed, independently of the
// producer's later task/product. Accepted prose remains immutable audit history.
type SegmentQuota struct {
	Kind     string `json:"kind"`
	Owner    string `json:"owner"`
	Point    string `json:"point"`
	Angle    string `json:"angle"`
	Returned bool   `json:"returned,omitempty"`
}

type SegmentDelivery struct {
	Status string `json:"status"` // failed, or unconfirmed (Core may have accepted).
	Stage  string `json:"stage"`
	Reason string `json:"reason"`
	At     string `json:"at"`
}

func (s *State) segmentQuota(t Task) *SegmentQuota {
	if !s.Config.Rolling {
		return nil
	}
	owner := s.schedulingOwner(t)
	if !t.Common {
		// Use the adopted task's frozen owner, never today's rotation cursor.
		if t.FocusLink != "" {
			owner = t.FocusLink
		} else if t.LinkKey != "" {
			owner = t.LinkKey
		}
	}
	kind := productContentKind
	if t.Common {
		kind, owner = commonContentKind, commonScheduleOwner
	}
	return &SegmentQuota{Kind: kind, Owner: owner, Point: s.schedulingPointKey(t, owner), Angle: s.schedulingAngleKey(t, owner)}
}

func decreaseQuota(counts map[string]int, key string) {
	if counts[key] > 0 {
		counts[key]--
	}
}

// RecordSegmentDeliveryFailure is idempotent and must be saved under the same
// run lease/CAS as adoption. Ambiguous network acknowledgements are not refunds:
// duplicating a possibly queued voice would be worse than preserving its debt.
func (s *State) RecordSegmentDeliveryFailure(id, stage, reason string, definite bool, now time.Time) bool {
	for i := range s.Accepted {
		seg := &s.Accepted[i]
		if seg.Selected.ID != id {
			continue
		}
		if s.Delivery != nil {
			for _, sent := range s.Delivery.DeliveredCandidates {
				if sent == id {
					return false
				}
			}
		}
		if seg.DeliveryStatus != nil {
			return false
		}
		status := "unconfirmed"
		if definite {
			status = "failed"
		}
		seg.DeliveryStatus = &SegmentDelivery{Status: status, Stage: stage, Reason: reason, At: now.UTC().Format(time.RFC3339Nano)}
		if definite && s.ProductFocus != nil && !seg.Task.Common {
			decreaseQuota(s.ProductFocus.PresentedSegments, seg.Task.FocusLink)
		}
		if !definite || s.Surface != "formal_live" || seg.Phase != "body" || seg.Task.ProductMode == "lecture" || seg.Task.ProductMode == "auxiliary" {
			return true
		}
		// Compatibility for already accepted segments from the previous runtime.
		// Promised continuations never consumed a scheduled slot.
		if seg.Quota == nil && seg.Task.SellingPoint != "塑价兑现" {
			seg.Quota = s.segmentQuota(seg.Task)
		}
		q := seg.Quota
		if q == nil || q.Returned {
			return true
		}
		decreaseQuota(s.ContentAdopted, q.Kind)
		decreaseQuota(s.PointUses, q.Point)
		decreaseQuota(s.ScheduledAngleUses, q.Angle)
		if s.ProductFocus != nil && q.Kind == productContentKind {
			decreaseQuota(s.ProductFocus.AdoptedSegments, q.Owner)
		}
		q.Returned = true
		s.DeliveryQuotaRebalance = true
		return true
	}
	return false
}

// Rebuild only future reservations against the effective adopted deficits.
// Never rewind a cursor/history or interrupt an in-flight repair/promise. A
// later body can compensate the missing share across small-cycle boundaries.
func (s *State) rebalanceReturnedDeliveryQuotas() bool {
	if !s.DeliveryQuotaRebalance || !s.Config.Rolling || s.Phase != "body" || len(s.Attempts) > 0 || s.P0PendingRepair != nil || s.PriceHook != nil || s.Lectures != nil && s.Lectures.Active != nil {
		return false
	}
	if s.ContentSchedule != nil {
		s.ContentSchedule.Items, s.ContentSchedule.Cursor = nil, 0
	}
	if s.ProductFocus != nil {
		for _, owner := range s.ProductFocus.Schedule[min(s.ProductFocus.ScheduleCursor, len(s.ProductFocus.Schedule)):] {
			decreaseQuota(s.ProductFocus.AllocatedSegments, owner)
		}
		s.ProductFocus.Schedule, s.ProductFocus.CurrentSlot = nil, -1
		s.ProductFocus.SelectionSet = false
	}
	for _, bag := range s.PointSchedules {
		if bag != nil {
			bag.Items, bag.Cursor = nil, 0
		}
	}
	s.DeliveryQuotaRebalance = false
	return true
}

// Used at the next text boundary if the failure arrived after chooseNext.
func (s *State) PrepareReturnedDeliveryQuotas() {
	if s.rebalanceReturnedDeliveryQuotas() {
		s.chooseNext()
	}
}
