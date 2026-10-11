package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"strings"
	"testing"
)

func TestDirectionReviewReuseAndBoundedFailure(t *testing.T) {
	h := anchorcycle.AngleHook{ID: "original-id", Action: "介绍已知条件"}
	bad := anchorcycle.AngleHook{ID: "new-id", Action: "讲未确认功能"}
	old := anchorcycle.DirectionReviewReceipt{HookID: h.ID, ContentSHA: anchorcycle.DirectionContentSHA(h), ContextSHA: "source", Decision: "通过"}
	p := &anchorcycle.AnglePlan{Cards: []anchorcycle.AngleHook{h, bad}, Raw: "old raw history", DirectionReviews: []anchorcycle.DirectionReviewReceipt{old}}
	calls := 0
	filterAngleDirectionsWithReview(context.Background(), p, "source", nil, func(_ context.Context, card anchorcycle.AngleHook) anchorcycle.DirectionReviewReceipt {
		calls++
		return anchorcycle.DirectionReviewReceipt{Decision: "有问题", Raw: "有问题\n不能编功能"}
	})
	if calls != 1 || len(p.Cards) != 1 || p.Cards[0].ID != h.ID || len(p.ReviewCandidates) != 2 || p.Raw != "old raw history" {
		t.Fatal("cache, history or usable old direction lost", calls, p)
	}
	filterAngleDirectionsWithReview(context.Background(), p, "source", nil, func(context.Context, anchorcycle.AngleHook) anchorcycle.DirectionReviewReceipt {
		t.Fatal("repeated review")
		return anchorcycle.DirectionReviewReceipt{}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	filterAngleDirectionsWithReview(ctx, p, "new-context", nil, func(context.Context, anchorcycle.AngleHook) anchorcycle.DirectionReviewReceipt {
		t.Fatal("cancelled request called")
		return anchorcycle.DirectionReviewReceipt{}
	})
	if len(p.Cards) != 0 || len(p.ReviewCandidates) != 2 {
		t.Fatal("stale approval or lost history")
	}
}

func TestDirectionReviewSourceAndWireAreProgramOwned(t *testing.T) {
	state := &anchorcycle.State{Queue: []anchorcycle.Task{{SellingPoint: "规格", Angle: "选择"}}, Facts: "本次商品有整只和半只两种规格。"}
	p0, _, e := loadP0Mother()
	if e != nil {
		t.Fatal(e)
	}
	state.P0PolicyMD = p0
	h := anchorcycle.AngleHook{ID: "private-hook-id", Action: "从家庭分享说明规格选择", Facts: []string{"private-fact-id"}, Products: []string{"internal-product-id"}}
	msgs, e := angleDirectionReviewMessages(state, h, "content review mother")
	if e != nil {
		t.Fatal(e)
	}
	data, _ := json.Marshal(msgs)
	for _, id := range []string{"private-hook-id", "private-fact-id", "internal-product-id"} {
		if strings.Contains(string(data), id) {
			t.Fatal("model asked to maintain an index")
		}
	}
	if !strings.Contains(string(data), "有效P0") {
		t.Fatal("P0 omitted")
	}
	profile := agentgateway.SpeechModel{ID: "review", Model: "one"}
	a := angleDirectionReviewContext(state, profile, "mother")
	profile.Model = "two"
	if a == angleDirectionReviewContext(state, profile, "mother") {
		t.Fatal("review model change retained approval")
	}
	profile.Model = "one"
	if a == angleDirectionReviewContext(state, profile, "other mother") {
		t.Fatal("mother change retained approval")
	}
}

func TestDirectionReviewRebindsCachedContentWithoutAskingModelForIDs(t *testing.T) {
	h := anchorcycle.AngleHook{ID: "new-generation-id", Action: "按已确认条件介绍规格"}
	cached := anchorcycle.DirectionReviewReceipt{HookID: "old-generation-id", ContentSHA: anchorcycle.DirectionContentSHA(h), ContextSHA: "source", Decision: "通过", ExchangeID: 123}
	p := &anchorcycle.AnglePlan{Cards: []anchorcycle.AngleHook{h}}
	filterAngleDirectionsWithReview(context.Background(), p, "source", []anchorcycle.DirectionReviewReceipt{cached}, func(context.Context, anchorcycle.AngleHook) anchorcycle.DirectionReviewReceipt {
		t.Fatal("unchanged content and context requested review")
		return anchorcycle.DirectionReviewReceipt{}
	})
	if len(p.Cards) != 1 || p.DirectionReviews[0].HookID != h.ID || p.DirectionReviews[0].ExchangeID != 123 {
		t.Fatal("program did not rebind the cached receipt", p)
	}
	p.Cards[0].Action = "新添一个未经核对的结论"
	filterAngleDirectionsWithReview(context.Background(), p, "source", nil, func(context.Context, anchorcycle.AngleHook) anchorcycle.DirectionReviewReceipt {
		t.Fatal("current context must not reinterpret mutated content as reviewed")
		return anchorcycle.DirectionReviewReceipt{}
	})
	if len(p.Cards) != 0 {
		t.Fatal("changed content inherited an old approval")
	}
}
