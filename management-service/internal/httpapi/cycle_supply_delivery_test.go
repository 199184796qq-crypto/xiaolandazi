package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"sync/atomic"
	"testing"
	"time"
)

type deliveryTestRepository struct {
	*supplyRepository
	busy     atomic.Bool
	conflict atomic.Bool
}

func (r *deliveryTestRepository) Acquire(context.Context, int64, int64, int64, string) error {
	if r.busy.Load() {
		return anchorcycle.ErrBusy
	}
	return nil
}
func (r *deliveryTestRepository) Save(ctx context.Context, s *anchorcycle.State) error {
	if r.conflict.CompareAndSwap(true, false) {
		return anchorcycle.ErrStale
	}
	return r.supplyRepository.Save(ctx, s)
}

func deliveryFailureFixture(t *testing.T) (*cycleSupply, *deliveryTestRepository) {
	t.Helper()
	s := cycleTestState(t)
	s.RunID, s.Surface, s.Config.Rolling = "delivery-test", "formal_live", true
	s.ContentAdopted = map[string]int{"product": 1}
	s.PointUses = map[string]int{"point": 1}
	s.ScheduledAngleUses = map[string]int{"angle": 1}
	s.ProductFocus = &anchorcycle.ProductFocus{AdoptedSegments: map[string]int{"1": 1, "2": 4}}
	s.Accepted = []anchorcycle.Segment{{Phase: "body", Task: anchorcycle.Task{FocusLink: "1"}, Quota: &anchorcycle.SegmentQuota{Kind: "product", Owner: "1", Point: "point", Angle: "angle"}, Selected: anchorcycle.Candidate{ID: "failed", Text: "已采纳的原稿"}}}
	r := &deliveryTestRepository{supplyRepository: &supplyRepository{state: &s}}
	p := newCycleSupply(&Server{anchorCycles: r}, context.Background(), &s)
	t.Cleanup(p.stop)
	return p, r
}

func TestSupplyVoiceRefundRetriesLeaseAndCASWithoutLosingNewAdoption(t *testing.T) {
	p, repo := deliveryFailureFixture(t)
	repo.busy.Store(true)
	p.skipVoice(p.initial, "failed", "声音归档失败")
	if len(p.voiceFailures) != 1 || repo.state.ContentAdopted["product"] != 1 {
		t.Fatal("busy lease lost/refunded the failure prematurely")
	}
	// A new text adoption committed while the voice worker was waiting.
	repo.state.Accepted = append(repo.state.Accepted, anchorcycle.Segment{Selected: anchorcycle.Candidate{ID: "newer"}})
	repo.busy.Store(false)
	repo.conflict.Store(true)
	if err := p.flushVoiceFailures(context.Background()); err != nil || len(p.voiceFailures) != 1 {
		t.Fatal("CAS conflict discarded the pending refund", err)
	}
	if err := p.flushVoiceFailures(context.Background()); err != nil {
		t.Fatal(err)
	}
	saved, _ := repo.Get(context.Background(), 0, 0, 0)
	if len(saved.Accepted) != 2 || saved.Accepted[1].Selected.ID != "newer" || saved.ContentAdopted["product"] != 0 || saved.ProductFocus.AdoptedSegments["2"] != 4 || !saved.Accepted[0].Quota.Returned {
		t.Fatal("refund overwrote newer prose or wrong owner")
	}
	p.skipVoice(saved, "failed", "重复失败回执")
	saved, _ = repo.Get(context.Background(), 0, 0, 0)
	if saved.PointUses["point"] != 0 || saved.ScheduledAngleUses["angle"] != 0 || len(p.voiceFailures) != 0 {
		t.Fatal("duplicate refund changed counters twice")
	}
	resumed := newCycleSupply(p.s, context.Background(), saved)
	defer resumed.stop()
	if !resumed.skippedVoices["failed"] {
		t.Fatal("restart would synthesize a refunded segment again")
	}
}

func TestSupplyLateVoiceFailureCannotRefundReplacementRun(t *testing.T) {
	p, repo := deliveryFailureFixture(t)
	p.mu.Lock()
	p.voiceFailures["failed"] = pendingVoiceFailure{Stage: "voice", Reason: "late"}
	p.mu.Unlock()
	newRun, _ := repo.Get(context.Background(), 0, 0, 0)
	newRun.RunID = "replacement-run"
	repo.state = newRun
	if err := p.flushVoiceFailures(context.Background()); !errors.Is(err, anchorcycle.ErrStale) {
		t.Fatal("old run did not stop", err)
	}
	if repo.state.ContentAdopted["product"] != 1 || repo.state.Accepted[0].DeliveryStatus != nil {
		t.Fatal("late callback touched replacement run")
	}
}

func TestSupplyUnconfirmedCoreAppendDoesNotRefundPossiblyPlayedVoice(t *testing.T) {
	p, repo := deliveryFailureFixture(t)
	p.skipVoice(p.initial, "failed", "Core响应丢失", "delivery_unconfirmed")
	saved, _ := repo.Get(context.Background(), 0, 0, 0)
	if saved.ContentAdopted["product"] != 1 || saved.Accepted[0].Quota.Returned || saved.Accepted[0].DeliveryStatus.Status != "unconfirmed" {
		t.Fatal("ambiguous append caused a duplicate quota/replay")
	}
}

func TestSupplyPreSubmitFailureRefundsAndContinuesNextVoice(t *testing.T) {
	p, repo := deliveryFailureFixture(t)
	repo.state.Accepted = append(repo.state.Accepted, anchorcycle.Segment{Selected: anchorcycle.Candidate{ID: "next", Text: "下一段已审核稿"}})
	p.synthesize = func(_ context.Context, _ *anchorcycle.State, id string) (*anchorcycle.VoiceAsset, error) {
		v := &anchorcycle.VoiceAsset{Status: "ready", Bundle: model.LiveAgentPlanVersionVariant{AudioDurationMS: 1000}}
		repo.mu.Lock()
		for i := range repo.state.Accepted {
			if repo.state.Accepted[i].Selected.ID == id {
				repo.state.Accepted[i].Voice = v
			}
		}
		repo.mu.Unlock()
		return v, nil
	}
	delivered := make(chan string, 1)
	p.onReady = func(_ context.Context, _ *anchorcycle.State, seg anchorcycle.Segment) error {
		if seg.Selected.ID == "failed" {
			return beforeMainlineSubmit(errors.New("声音绑定不符，未调用Core"))
		}
		delivered <- seg.Selected.ID
		return nil
	}
	done := make(chan struct{})
	go func() { defer close(done); p.voiceLoop() }()
	select {
	case id := <-delivered:
		if id != "next" {
			t.Fatal(id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("one validation error blocked approved audio supply")
	}
	p.stop()
	<-done
	saved, _ := repo.Get(context.Background(), 0, 0, 0)
	if saved.ContentAdopted["product"] != 0 || !saved.Accepted[0].Quota.Returned || saved.Accepted[0].DeliveryStatus.Stage != "delivery" {
		t.Fatal("definitely unsent voice was not refunded")
	}
}
