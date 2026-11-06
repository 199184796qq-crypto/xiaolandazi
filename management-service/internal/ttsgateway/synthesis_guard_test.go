package ttsgateway

import (
	"context"
	"errors"
	"livecompanion/management/internal/ttsscheduler"
	"testing"
)

type guardedQueueProvider struct{ *countedReceiptProvider }

func (*guardedQueueProvider) Name() string { return ProviderQwen }

func TestSynthesisGuardAfterQueueBeforeFeeAndProvider(t *testing.T) {
	removed := false
	want := errors.New("task removed while queued")
	provider := &guardedQueueProvider{&countedReceiptProvider{run: func(context.Context, SynthesizeRequest) (SynthesizeResponse, error) {
		t.Fatal("removed task reached provider")
		return SynthesizeResponse{}, nil
	}}}
	g := New(provider.Name(), "model")
	g.Register(provider)
	g.scheduler = schedulerFixture(func(context.Context, ttsscheduler.Work) (ttsscheduler.Permit, error) {
		removed = true
		return ttsscheduler.Permit{Credential: ttsscheduler.Credential{AccountID: ttsscheduler.CurrentAccountID}, Finish: func(string) {}}, nil
	})
	before, after, checks := 0, 0, 0
	ctx := WithSynthesisObserver(t.Context(), receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { before++; return "ticket", nil }, after: func(context.Context, SynthesisReceipt) error { after++; return nil }})
	ctx = WithSynthesisGuard(ctx, func(context.Context) error {
		checks++
		if !removed {
			t.Fatal("guard ran before queue admission")
		}
		return want
	})
	_, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "voice", Text: "继续看看商品。"})
	if !errors.Is(err, want) || !DefinitelyNotSent(err) || checks != 1 || provider.calls.Load() != 0 || before != 0 || after != 0 {
		t.Fatalf("guard did not fence purchase: err=%v checks=%d provider=%d ledger=%d/%d", err, checks, provider.calls.Load(), before, after)
	}
}

func TestSynthesisGuardsComposeAndPreserveOnePaidReceipt(t *testing.T) {
	provider := &countedReceiptProvider{run: func(context.Context, SynthesizeRequest) (SynthesizeResponse, error) {
		return SynthesizeResponse{AudioURL: "https://audio.invalid/fixture.wav"}, nil
	}}
	g := New(provider.Name(), "model")
	g.Register(provider)
	first, second, before, after := 0, 0, 0, 0
	ctx := WithSynthesisObserver(t.Context(), receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { before++; return "ticket", nil }, after: func(_ context.Context, r SynthesisReceipt) error {
		after++
		if r.Outcome != SynthesisSucceeded {
			t.Error(r.Outcome)
		}
		return nil
	}})
	ctx = WithSynthesisGuard(ctx, func(context.Context) error { first++; return nil })
	ctx = WithSynthesisGuard(ctx, func(context.Context) error { second++; return nil })
	_, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "voice", Text: "继续看看商品。"})
	if err != nil || first != 1 || second != 1 || before != 1 || after != 1 || provider.calls.Load() != 1 {
		t.Fatal(err, first, second, before, after, provider.calls.Load())
	}
}
