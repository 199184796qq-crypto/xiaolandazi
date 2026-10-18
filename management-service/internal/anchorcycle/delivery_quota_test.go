package anchorcycle

import (
	"encoding/json"
	"livecompanion/management/internal/anchorstylescore"
	"reflect"
	"testing"
	"time"
)

func adoptDeliveryTestBody(t *testing.T, s *State, id string) Segment {
	t.Helper()
	s.Phase = "body"
	s.SelectProductTask()
	c := candidate("已核对的商品讲解。", 100)
	c.ID = id
	if ok, err := s.AddCandidate(c, anchorstylescore.Baseline{}); err != nil || !ok {
		t.Fatal(err, s.Status)
	}
	return s.Accepted[len(s.Accepted)-1]
}

func TestDeliveryFailureRefundUsesAdoptedOwnerAndSurvivesReload(t *testing.T) {
	s := mixFixture(t, 2, "delivery-refund")
	s.Surface = "formal_live"
	seg := adoptDeliveryTestBody(t, &s, "failed")
	q := *seg.Quota
	// Production has already prefetched another task; refunds must never use it.
	other := "1"
	if q.Owner == other {
		other = "2"
	}
	s.ProductFocus.CurrentLink = other
	s.ProductFocus.AdoptedSegments[other] = 5
	before := s.TotalAccepted()
	schedule, _ := json.Marshal(s.ContentSchedule)
	if !s.RecordSegmentDeliveryFailure("failed", "voice", "归档失败", true, time.Now()) {
		t.Fatal("failure not recorded")
	}
	afterSchedule, _ := json.Marshal(s.ContentSchedule)
	if s.TotalAccepted() != before || s.Accepted[0].Selected.ID != "failed" || string(schedule) != string(afterSchedule) {
		t.Fatal("refund rewound immutable history/cursor")
	}
	if s.ProductFocus.AdoptedSegments[q.Owner] != 0 || s.ProductFocus.AdoptedSegments[other] != 5 || s.PointUses[q.Point] != 0 || s.ScheduledAngleUses[q.Angle] != 0 || s.ContentAdopted[q.Kind] != 0 {
		t.Fatal("wrong reservation refunded", s.ProductFocus.AdoptedSegments)
	}
	raw, _ := json.Marshal(s)
	var loaded State
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.RecordSegmentDeliveryFailure("failed", "voice", "重复回调", true, time.Now()) || !loaded.Accepted[0].Quota.Returned || loaded.Accepted[0].DeliveryStatus.Status != "failed" {
		t.Fatal("refund was not idempotent after restart")
	}
	if len(loaded.P0SkippedKeys) != 0 || len(loaded.DuplicateSkippedKeys) != 0 {
		t.Fatal("delivery failure blacklisted an angle")
	}
}

func TestDeliveryRefundCompensatesPublicShareInNextWindow(t *testing.T) {
	s := productOnlyFixture(t, 1, true)
	s.Surface = "formal_live"
	for i := 0; i < 5; i++ {
		adoptDeliveryTestBody(t, &s, ID())
	}
	if s.ContentAdopted[commonContentKind] != 1 || s.ContentAdopted[productContentKind] != 4 {
		t.Fatal("fixture did not consume 20/80 window", s.ContentAdopted)
	}
	var failed Segment
	for _, seg := range s.Accepted {
		if seg.Task.Common {
			failed = seg
			break
		}
	}
	if !s.RecordSegmentDeliveryFailure(failed.Selected.ID, "voice", "TTS失败", true, time.Now()) {
		t.Fatal("missing public refund")
	}
	s.Phase = "body"
	s.PrepareReturnedDeliveryQuotas()
	common := 0
	for _, kind := range s.ContentSchedule.Items {
		if kind == commonContentKind {
			common++
		}
	}
	if common != 2 {
		t.Fatal("missing public slot was silently counted as spoken", s.ContentAdopted, s.ContentSchedule.Items)
	}
	if s.DeliveryQuotaRebalance {
		t.Fatal("compensation was not consumed once")
	}
}

func TestDeliveryRefundDoesNotInterruptRepairOrCountUnknownDelivery(t *testing.T) {
	s := mixFixture(t, 2, "repair-refund")
	s.Surface = "formal_live"
	seg := adoptDeliveryTestBody(t, &s, "earlier")
	before, _ := json.Marshal(s.ContentAdopted)
	if !s.RecordSegmentDeliveryFailure("earlier", "delivery_unconfirmed", "网络响应丢失", false, time.Now()) {
		t.Fatal("missing uncertain outcome")
	}
	after, _ := json.Marshal(s.ContentAdopted)
	if string(before) != string(after) || s.DeliveryQuotaRebalance || seg.Quota.Returned {
		t.Fatal("possibly queued voice refunded/replayed")
	}
	seg = adoptDeliveryTestBody(t, &s, "known-failure")
	s.Attempts = []Candidate{{ID: "repair-in-flight"}}
	pending := s.Task()
	s.RecordSegmentDeliveryFailure(seg.Selected.ID, "voice", "声音失败", true, time.Now())
	s.PrepareReturnedDeliveryQuotas()
	if !reflect.DeepEqual(pending, s.Task()) || !s.DeliveryQuotaRebalance {
		t.Fatal("refund changed an in-flight repair")
	}
	s.Attempts = nil
	s.Phase = "body"
	s.PrepareReturnedDeliveryQuotas()
	if s.DeliveryQuotaRebalance {
		t.Fatal("deferred rebalance lost after repair")
	}
}

func TestDeliveryRefundKeepsOpeningClosingAndConfirmedCoreCounts(t *testing.T) {
	for _, phase := range []string{"opening", "closing", "body"} {
		t.Run(phase, func(t *testing.T) {
			s := mixFixture(t, 2, phase)
			s.Surface = "formal_live"
			seg := adoptDeliveryTestBody(t, &s, "sent")
			s.Accepted[0].Phase = phase
			if phase == "body" {
				s.Delivery = &MainlineDelivery{DeliveredCandidates: []string{"sent"}}
			}
			counts, _ := json.Marshal(s.ContentAdopted)
			s.RecordSegmentDeliveryFailure(seg.Selected.ID, "voice", "late callback", true, time.Now())
			after, _ := json.Marshal(s.ContentAdopted)
			if string(counts) != string(after) || s.Accepted[0].Quota.Returned {
				t.Fatal("non-body or confirmed Core submission refunded")
			}
		})
	}
}
