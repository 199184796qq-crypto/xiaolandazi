package ttsgateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInteractionPaidResponseDrainsAfterEventDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Millisecond)
	defer cancel()
	var receipt SynthesisReceipt
	provider := &countedReceiptProvider{run: func(c context.Context, r SynthesizeRequest) (SynthesizeResponse, error) {
		if deadline, ok := c.Deadline(); !ok || time.Until(deadline) > 91*time.Second {
			t.Fatal("unbounded supplier work")
		}
		<-ctx.Done()
		if c.Err() != nil {
			return SynthesizeResponse{}, c.Err()
		}
		input, output := int64(13), int64(32)
		return SynthesizeResponse{AudioURL: "https://audio.invalid/retained.wav", ProviderRequestID: "late-request", Usage: SynthesisUsage{Present: true, InputTokens: &input, OutputTokens: &output}}, nil
	}}
	ctx = WithSynthesisObserver(ctx, receiptObserver{
		before: func(context.Context, SynthesisAttempt) (string, error) { return "original-ticket", nil },
		after: func(c context.Context, r SynthesisReceipt) error {
			if c.Err() != nil {
				t.Error("cancelled settlement")
			}
			receipt = r
			return nil
		},
	})
	response, err := observeSynthesis(ctx, provider, SynthesizeRequest{Text: "完整话术", DrainOnCancel: true})
	if err != nil || response.AudioURL == "" || receipt.Outcome != SynthesisSucceeded || receipt.ProviderRequestID != "late-request" || receipt.Usage.InputTokens == nil || *receipt.Usage.InputTokens != 13 || receipt.Usage.OutputTokens == nil || *receipt.Usage.OutputTokens != 32 || receipt.UsageSource != UsageSupplierResponse || provider.calls.Load() != 1 {
		t.Fatalf("late supplier receipt lost: %v %+v", err, receipt)
	}
}

func TestInteractionDrainDoesNotSendCancelledOrDeclinedWork(t *testing.T) {
	for _, cancelled := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancelled", false: "declined"}[cancelled], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if cancelled {
				cancel()
			}
			ctx = WithSynthesisObserver(ctx, receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { return "", errors.New("declined") }, after: func(context.Context, SynthesisReceipt) error { t.Fatal("unexpected receipt"); return nil }})
			provider := &countedReceiptProvider{run: func(context.Context, SynthesizeRequest) (SynthesizeResponse, error) {
				t.Fatal("request was sent")
				return SynthesizeResponse{}, nil
			}}
			_, err := observeSynthesis(ctx, provider, SynthesizeRequest{DrainOnCancel: true})
			if err == nil || !DefinitelyNotSent(err) || provider.calls.Load() != 0 {
				t.Fatal("cancelled work admitted")
			}
		})
	}
}

func TestInteractionActualTransportFailureStaysUnknown(t *testing.T) {
	var receipt SynthesisReceipt
	ctx := WithSynthesisObserver(t.Context(), receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { return "original", nil }, after: func(_ context.Context, r SynthesisReceipt) error { receipt = r; return nil }})
	p := &countedReceiptProvider{run: func(context.Context, SynthesizeRequest) (SynthesizeResponse, error) {
		return SynthesizeResponse{}, errors.New("transport unavailable")
	}}
	_, err := observeSynthesis(ctx, p, SynthesizeRequest{DrainOnCancel: true})
	if err == nil || DefinitelyNotSent(err) || receipt.Outcome != SynthesisUnknown || receipt.Usage.InputTokens != nil || receipt.Usage.OutputTokens != nil {
		t.Fatal("unconfirmed usage was invented", receipt, err)
	}
}

func TestQwenTruncatedResponseRetainsHeaderRequestID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "supplier-header-id")
		w.Header().Set("Content-Length", "1000")
		w.Write([]byte(`{"request_id":`))
	}))
	defer srv.Close()
	provider := NewQwenProvider(QwenConfig{APIKey: "fixture", TTSBaseURL: srv.URL})
	response, err := provider.SynthesizeURL(t.Context(), SynthesizeRequest{Model: "qwen-audio-3.1-tts-flash", VoiceID: "goose", Text: "完整文字"})
	if err == nil || DefinitelyNotSent(err) || response.ProviderRequestID != "supplier-header-id" || response.Usage.InputTokens != nil {
		t.Fatal("lost request ID or invented usage", response, err)
	}
}
