package ttsgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type receiptObserver struct {
	before func(context.Context, SynthesisAttempt) (string, error)
	after  func(context.Context, SynthesisReceipt) error
}

func (o receiptObserver) BeforeSynthesis(ctx context.Context, a SynthesisAttempt) (string, error) {
	return o.before(ctx, a)
}
func (o receiptObserver) AfterSynthesis(ctx context.Context, r SynthesisReceipt) error {
	return o.after(ctx, r)
}

type countedReceiptProvider struct {
	calls atomic.Int64
	run   func(context.Context, SynthesizeRequest) (SynthesizeResponse, error)
}

func (*countedReceiptProvider) Name() string { return "receipt_test" }
func (p *countedReceiptProvider) SynthesizeURL(ctx context.Context, r SynthesizeRequest) (SynthesizeResponse, error) {
	p.calls.Add(1)
	return p.run(ctx, r)
}
func (*countedReceiptProvider) CloneVoice(context.Context, CloneRequest) (CloneResponse, error) {
	return CloneResponse{VoiceID: "clone"}, nil
}

func TestSynthesisObserverReservationDeclinePreventsPaidCall(t *testing.T) {
	provider := &countedReceiptProvider{run: func(context.Context, SynthesizeRequest) (SynthesizeResponse, error) {
		t.Fatal("provider called after declined reservation")
		return SynthesizeResponse{}, nil
	}}
	g := New(provider.Name(), "model")
	g.Register(provider)
	before, after := 0, 0
	ctx := WithSynthesisObserver(t.Context(), receiptObserver{
		before: func(context.Context, SynthesisAttempt) (string, error) {
			before++
			return "", errors.New("可选更新预算暂不足")
		},
		after: func(context.Context, SynthesisReceipt) error { after++; return nil },
	})
	_, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "voice", Text: "你好。"})
	if err == nil || !DefinitelyNotSent(err) || before != 1 || after != 0 || provider.calls.Load() != 0 {
		t.Fatalf("err=%v before=%d after=%d calls=%d", err, before, after, provider.calls.Load())
	}
}

func TestSynthesisSettlementFailureKeepsSuccessfulAudioWithoutRegeneration(t *testing.T) {
	for _, panicAfter := range []bool{false, true} {
		t.Run(fmt.Sprint(panicAfter), func(t *testing.T) {
			provider := &countedReceiptProvider{run: func(context.Context, SynthesizeRequest) (SynthesizeResponse, error) {
				return SynthesizeResponse{AudioURL: "https://audio.invalid/?Signature=secret", ProviderRequestID: "request-1"}, nil
			}}
			g := New(provider.Name(), "model")
			g.Register(provider)
			before, after := 0, 0
			ctx := WithSynthesisObserver(t.Context(), receiptObserver{
				before: func(_ context.Context, a SynthesisAttempt) (string, error) {
					before++
					if a.SentTextCharacters != 4 || a.Rate != 1.2 || a.TextSHA != digestSynthesisText("你好a。") || a.InstructionSHA != digestSynthesisText("慢一点") {
						t.Errorf("attempt metadata=%+v", a)
					}
					return "ticket-1", nil
				},
				after: func(ctx context.Context, r SynthesisReceipt) error {
					after++
					if ctx.Err() != nil || r.Ticket != "ticket-1" || r.Outcome != SynthesisSucceeded || r.UsageSource != UsageSentTextEstimate || r.Usage.Characters != nil || r.ProviderRequestID != "request-1" {
						t.Errorf("receipt=%+v context=%v", r, ctx.Err())
					}
					raw, _ := json.Marshal(r)
					if strings.Contains(string(raw), "Signature") || strings.Contains(string(raw), "你好") || strings.Contains(string(raw), "secret") {
						t.Error("receipt leaked body/audio URL")
					}
					if panicAfter {
						panic("ledger unavailable")
					}
					return errors.New("ledger unavailable")
				},
			})
			out, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "voice", Text: "你好a。", Instruction: "慢一点", Rate: 1.2})
			if err != nil || out.AudioURL == "" || !out.AccountingPending || before != 1 || after != 1 || provider.calls.Load() != 1 {
				t.Fatalf("out=%+v err=%v before=%d after=%d calls=%d", out, err, before, after, provider.calls.Load())
			}
		})
	}
}

func TestSynthesisCancellationAfterAttemptRemainsUnknownAndSettles(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	provider := &countedReceiptProvider{run: func(context.Context, SynthesizeRequest) (SynthesizeResponse, error) {
		cancel()
		return SynthesizeResponse{}, context.Canceled
	}}
	g := New(provider.Name(), "model")
	g.Register(provider)
	var receipt SynthesisReceipt
	ctx = WithSynthesisObserver(ctx, receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { return "ticket", nil }, after: func(ctx context.Context, r SynthesisReceipt) error {
		if ctx.Err() != nil {
			t.Error("settlement inherited cancelled media request")
		}
		receipt = r
		return nil
	}})
	_, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "voice", Text: "测试。"})
	if !errors.Is(err, context.Canceled) || DefinitelyNotSent(err) || receipt.Outcome != SynthesisUnknown || provider.calls.Load() != 1 {
		t.Fatalf("err=%v receipt=%+v calls=%d", err, receipt, provider.calls.Load())
	}
}

func TestSynthesisLocalProviderValidationReleasesReservation(t *testing.T) {
	g := New(ProviderQwen, "model")
	g.Register(NewQwenProvider(QwenConfig{}))
	var receipt SynthesisReceipt
	ctx := WithSynthesisObserver(t.Context(), receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { return "ticket", nil }, after: func(_ context.Context, r SynthesisReceipt) error { receipt = r; return nil }})
	_, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "voice", Text: "测试。"})
	if err == nil || !DefinitelyNotSent(err) || receipt.Outcome != SynthesisNotSent {
		t.Fatalf("err=%v receipt=%+v", err, receipt)
	}
}

func TestSynthesisBeforeCancellationDoesNotInvokeProvider(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	provider := &countedReceiptProvider{run: func(context.Context, SynthesizeRequest) (SynthesizeResponse, error) {
		t.Fatal("provider called after cancellation in reservation")
		return SynthesizeResponse{}, nil
	}}
	g := New(provider.Name(), "model")
	g.Register(provider)
	var receipt SynthesisReceipt
	ctx = WithSynthesisObserver(ctx, receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { cancel(); return "ticket", nil }, after: func(_ context.Context, r SynthesisReceipt) error { receipt = r; return nil }})
	_, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "voice", Text: "测试。"})
	if !errors.Is(err, context.Canceled) || !DefinitelyNotSent(err) || receipt.Outcome != SynthesisNotSent || provider.calls.Load() != 0 {
		t.Fatalf("err=%v receipt=%+v calls=%d", err, receipt, provider.calls.Load())
	}
}

func TestQwenReceiptDistinguishesSupplierZeroMissingInvalidAndTokens(t *testing.T) {
	tests := []struct {
		name, usage      string
		characters       *int64
		field, source    string
		present, invalid bool
	}{
		{name: "missing", source: UsageSentTextEstimate},
		{name: "null", usage: "null", source: UsageSentTextEstimate},
		{name: "empty", usage: "{}", present: true, source: UsageSentTextEstimate},
		{name: "zero", usage: `{"characters":0}`, characters: int64Ptr(0), field: "characters", present: true, source: UsageSupplierResponse},
		{name: "billable", usage: `{"characters":13}`, characters: int64Ptr(13), field: "characters", present: true, source: UsageSupplierResponse},
		{name: "input_alias", usage: `{"input_characters":7}`, characters: int64Ptr(7), field: "input_characters", present: true, source: UsageSupplierResponse},
		{name: "character_precedence", usage: `{"characters":5,"input_characters":7}`, characters: int64Ptr(5), field: "characters", present: true, source: UsageSupplierResponse},
		{name: "invalid", usage: `{"characters":"13","input_tokens":-2}`, present: true, invalid: true, source: UsageSentTextEstimate},
		{name: "tokens", usage: `{"input_tokens":0,"output_tokens":4,"total_tokens":4}`, present: true, source: UsageSupplierResponse},
		{name: "duration", usage: `{"duration":3}`, present: true, source: UsageSupplierResponse},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				usage := ""
				if tc.usage != "" {
					usage = `,"usage":` + tc.usage
				}
				fmt.Fprint(w, `{"request_id":"qwen-request-1","output":{"audio":{"url":"https://audio.invalid/?Signature=private"}}`+usage+`}`)
			}))
			defer server.Close()
			g := New(ProviderQwen, "model")
			g.Register(NewQwenProvider(QwenConfig{APIKey: "fake", TTSBaseURL: server.URL, Client: server.Client()}))
			var receipt SynthesisReceipt
			ctx := WithSynthesisObserver(t.Context(), receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { return "ticket", nil }, after: func(_ context.Context, r SynthesisReceipt) error { receipt = r; return nil }})
			out, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "voice", Text: "这是输入正文。"})
			if err != nil || out.AudioURL == "" || out.ProviderRequestID != "qwen-request-1" || calls.Load() != 1 || receipt.Outcome != SynthesisSucceeded || receipt.UsageSource != tc.source || out.UsageSource != tc.source {
				t.Fatalf("out=%+v receipt=%+v err=%v calls=%d", out, receipt, err, calls.Load())
			}
			usage := receipt.Usage
			if usage.Present != tc.present || usage.Invalid != tc.invalid || usage.CharacterField != tc.field || (usage.Characters == nil) != (tc.characters == nil) || (tc.characters != nil && *usage.Characters != *tc.characters) {
				t.Fatalf("usage=%+v expected=%+v", usage, tc)
			}
			if tc.name == "tokens" && (usage.InputTokens == nil || *usage.InputTokens != 0 || usage.OutputTokens == nil || *usage.OutputTokens != 4 || usage.TotalTokens == nil || *usage.TotalTokens != 4 || usage.Characters != nil) {
				t.Fatalf("tokens mixed with characters: %+v", usage)
			}
			if tc.name == "duration" && (usage.DurationSeconds == nil || *usage.DurationSeconds != 3) {
				t.Fatalf("supplier duration missing: %+v", usage)
			}
		})
	}
}

func int64Ptr(value int64) *int64 { return &value }

func TestQwenFailedHTTPAndTruncatedResponseAreUnknownNotRefundable(t *testing.T) {
	for _, scenario := range []string{"http400", "truncated"} {
		t.Run(scenario, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if scenario == "http400" {
					w.WriteHeader(400)
					fmt.Fprint(w, `{"request_id":"failed-request-1","usage":{"characters":2},"message":"secret https://audio.invalid/?Signature=private"}`)
				} else {
					fmt.Fprint(w, `{"request_id":"partial-request-1","output":{"audio":`)
				}
			}))
			defer server.Close()
			g := New(ProviderQwen, "model")
			g.Register(NewQwenProvider(QwenConfig{APIKey: "fake", TTSBaseURL: server.URL, Client: server.Client()}))
			var receipt SynthesisReceipt
			ctx := WithSynthesisObserver(t.Context(), receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { return "ticket", nil }, after: func(_ context.Context, r SynthesisReceipt) error { receipt = r; return nil }})
			_, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "voice", Text: "测试。"})
			if err == nil || DefinitelyNotSent(err) || receipt.Outcome != SynthesisUnknown || calls.Load() != 1 {
				t.Fatalf("err=%v receipt=%+v calls=%d", err, receipt, calls.Load())
			}
			if scenario == "http400" && (receipt.ProviderRequestID != "failed-request-1" || receipt.Usage.Characters == nil || *receipt.Usage.Characters != 2) {
				t.Fatalf("failed provider receipt lost: %+v", receipt)
			}
			raw, _ := json.Marshal(receipt)
			if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "Signature") || strings.Contains(string(raw), "audio.invalid") {
				t.Error("accounting receipt leaked vendor error body or URL")
			}
		})
	}
}

func TestSynthesisObserverDoesNotApplyToCloneOrExplicitOptOut(t *testing.T) {
	provider := &countedReceiptProvider{run: func(context.Context, SynthesizeRequest) (SynthesizeResponse, error) {
		return SynthesizeResponse{AudioURL: "https://audio.invalid"}, nil
	}}
	g := New(provider.Name(), "model")
	g.Register(provider)
	ctx := WithSynthesisObserver(t.Context(), receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) {
		t.Fatal("unexpected accounting for unrelated clone/opt out")
		return "", nil
	}, after: func(context.Context, SynthesisReceipt) error { t.Fatal("unexpected receipt"); return nil }})
	if _, err := g.CloneVoice(ctx, CloneRequest{TargetModel: "model"}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.SynthesizeURL(WithSynthesisObserver(ctx, nil), SynthesizeRequest{VoiceID: "voice", Text: "测试。"}); err != nil {
		t.Fatal(err)
	}
}

func TestSafeProviderRequestIDDoesNotAcceptURLOrSecretFields(t *testing.T) {
	for _, value := range []string{"https://audio.invalid/?Signature=secret", "Bearer secret", "request\nAuthorization=secret", strings.Repeat("a", 129)} {
		if safeProviderRequestID(value) != "" {
			t.Fatal("unsafe request ID accepted")
		}
	}
	if safeProviderRequestID(" req-123_ab ") != "req-123_ab" {
		t.Fatal("valid request ID rejected")
	}
}
