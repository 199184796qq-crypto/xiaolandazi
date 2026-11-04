package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"livecompanion/management/internal/livecost"
)

func TestLiveCostBackgroundRecoversLateReceiptWithoutAnotherLiveRequest(t *testing.T) {
	for _, outcome := range []string{livecost.ReceiptSuccess, livecost.ReceiptUnknown} {
		t.Run(outcome, func(t *testing.T) {
			store := &adapterCostStore{}
			s := adapterTestServer(t, store)
			r := livecost.Request{Key: "original-late-tts", TenantID: 14, RoomID: 29, PlanID: 4, SessionID: 640, RunID: "original-run", Kind: livecost.KindTTS, Provider: "qwen_audio", Model: "qwen-audio-3.1-tts-flash", Priority: livecost.PriorityNecessary, Reason: "original", Fingerprint: "original", EstimatedMicros: 8238}
			if a, e := s.liveCostRequests.Begin(t.Context(), r); e != nil || !a.Allowed {
				t.Fatal(a, e)
			}
			store.failFinish = true
			receipt := livecost.Receipt{TenantID: 14, RoomID: 29, Status: outcome, UsageSource: livecost.SourceSentTextEstimate, ProviderRequestID: "late-id", InputTokens: 13, OutputTokens: 32}
			if e := s.liveCostRequests.Finish(t.Context(), r.Key, receipt); !errors.Is(e, livecost.ErrAccountingUnavailable) {
				t.Fatal(e)
			}
			store.failFinish = false
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan struct{})
			go func() { defer close(done); s.runLiveCostReconciliation(ctx, time.Millisecond) }()
			deadline := time.Now().Add(time.Second)
			for {
				store.mu.Lock()
				count := len(store.receipts)
				store.mu.Unlock()
				if count > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("background replay required another request")
				}
				time.Sleep(time.Millisecond)
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("shutdown blocked")
			}
			store.mu.Lock()
			entry := store.entries[r.Key]
			requests := len(store.requests)
			store.mu.Unlock()
			if requests != 1 || entry.Receipt.ProviderRequestID != "late-id" {
				t.Fatal("changed original identity", entry)
			}
			if outcome == livecost.ReceiptSuccess && (entry.Status != livecost.StatusSettled || entry.ReservedMicros != 0) {
				t.Fatal("late successful receipt not settled", entry)
			}
			if outcome == livecost.ReceiptUnknown && (entry.Status != livecost.StatusUnknown || entry.ReservedMicros != 8238) {
				t.Fatal("unknown falsely released", entry)
			}
		})
	}
}
