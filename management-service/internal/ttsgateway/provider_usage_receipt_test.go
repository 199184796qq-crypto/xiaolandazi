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

	"github.com/gorilla/websocket"
)

func TestCosyVoiceReceiptPreservesCanonicalProviderAndSupplierUsage(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload struct {
			Model string `json:"model"`
			Input struct {
				Instruction string  `json:"instruction"`
				Text        string  `json:"text"`
				Rate        float64 `json:"rate"`
			} `json:"input"`
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Model != CosyVoiceFlash || payload.Input.Instruction != "自然一点" || payload.Input.Text != "欢迎。" || payload.Input.Rate != .9 {
			t.Error("synthesis fields changed")
		}
		fmt.Fprint(w, `{"request_id":"cosy-request-1","usage":{"characters":3},"output":{"audio":{"url":"https://audio.invalid/cosy.wav"}}}`)
	}))
	defer server.Close()
	g := New(ProviderCosyVoice, CosyVoiceFlash)
	g.Register(NewCosyVoiceProvider(QwenConfig{APIKey: "fake", TTSBaseURL: server.URL, Client: server.Client()}))
	var receipt SynthesisReceipt
	ctx := WithSynthesisObserver(t.Context(), receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { return "ticket", nil }, after: func(_ context.Context, r SynthesisReceipt) error { receipt = r; return nil }})
	out, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "voice", Text: "欢迎。", Rate: .9, Instruction: "自然一点"})
	if err != nil || out.Provider != ProviderCosyVoice || out.ProviderRequestID != "cosy-request-1" || receipt.Provider != ProviderCosyVoice || receipt.Usage.Characters == nil || *receipt.Usage.Characters != 3 || calls.Load() != 1 {
		t.Fatalf("out=%+v receipt=%+v err=%v calls=%d", out, receipt, err, calls.Load())
	}
}

func TestCosyVoiceLocalInstructionFailureDoesNotSendProviderRequest(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Error("invalid local instruction sent upstream")
	}))
	defer server.Close()
	g := New(ProviderCosyVoice, CosyVoiceFlash)
	g.Register(NewCosyVoiceProvider(QwenConfig{APIKey: "fake", TTSBaseURL: server.URL, Client: server.Client()}))
	var receipt SynthesisReceipt
	ctx := WithSynthesisObserver(t.Context(), receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { return "ticket", nil }, after: func(_ context.Context, r SynthesisReceipt) error { receipt = r; return nil }})
	_, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "voice", Text: "欢迎。", Instruction: strings.Repeat("自然", 30)})
	if err == nil || !DefinitelyNotSent(err) || receipt.Outcome != SynthesisNotSent || calls.Load() != 0 {
		t.Fatalf("err=%v receipt=%+v calls=%d", err, receipt, calls.Load())
	}
}

func TestTencentReceiptUsesServerRequestIDAndSentTextEstimate(t *testing.T) {
	for _, rejectFinal := range []bool{false, true} {
		t.Run(fmt.Sprint(rejectFinal), func(t *testing.T) {
			var handshakes, syntheses atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handshakes.Add(1)
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				conn.WriteJSON(map[string]any{"code": 0, "request_id": "tencent-server-request", "ready": 1})
				for i := 0; i < 2; i++ {
					var msg map[string]string
					if conn.ReadJSON(&msg) != nil {
						return
					}
					if msg["action"] == "ACTION_SYNTHESIS" {
						syntheses.Add(1)
						if msg["data"] != "欢迎a。" {
							t.Error("Tencent sent text changed")
						}
					}
				}
				if rejectFinal {
					conn.WriteJSON(map[string]any{"code": 123, "request_id": "tencent-server-request", "message": "SecretId=secret Signature=private"})
					return
				}
				conn.WriteMessage(websocket.BinaryMessage, []byte{1, 2, 3, 4})
				conn.WriteJSON(map[string]any{"code": 0, "request_id": "tencent-server-request", "final": 1})
			}))
			defer server.Close()
			g := New(ProviderTencent, TencentPremiumModel)
			g.Register(NewTencentProvider(TencentConfig{AppID: "123", SecretID: "fake-id", SecretKey: "fake-secret", Endpoint: "ws" + strings.TrimPrefix(server.URL, "http") + "/stream_wsv2", Publish: func(context.Context, []byte) (string, error) { return "https://audio.invalid/tencent.wav", nil }}))
			var receipt SynthesisReceipt
			ctx := WithSynthesisObserver(t.Context(), receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { return "ticket", nil }, after: func(_ context.Context, r SynthesisReceipt) error { receipt = r; return nil }})
			out, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "101030", Text: "  欢迎a。 \n", Rate: 1.2})
			if handshakes.Load() != 1 || syntheses.Load() != 1 || receipt.ProviderRequestID != "tencent-server-request" || out.ProviderRequestID != "tencent-server-request" || receipt.SentTextCharacters != 4 || receipt.TextSHA != digestSynthesisText("欢迎a。") || receipt.UsageSource != UsageSentTextEstimate || receipt.Usage.Present || receipt.Usage.Characters != nil || receipt.Usage.DurationSeconds != nil {
				t.Fatalf("out=%+v receipt=%+v err=%v handshakes=%d syntheses=%d", out, receipt, err, handshakes.Load(), syntheses.Load())
			}
			if rejectFinal {
				if err == nil || DefinitelyNotSent(err) || receipt.Outcome != SynthesisUnknown {
					t.Fatalf("provider failure was refunded: %v %+v", err, receipt)
				}
			} else if err != nil || out.AudioURL == "" || receipt.Outcome != SynthesisSucceeded {
				t.Fatalf("successful response lost: %v %+v", err, out)
			}
			raw, _ := json.Marshal(receipt)
			if strings.Contains(string(raw), "Signature") || strings.Contains(string(raw), "SecretId") || strings.Contains(string(raw), "audio.invalid") {
				t.Fatal("cost receipt leaked provider response")
			}
		})
	}
}

func TestSynthesisProviderPanicStillReceiptsUnknownWithoutSwallowingPanic(t *testing.T) {
	provider := &countedReceiptProvider{run: func(context.Context, SynthesizeRequest) (SynthesizeResponse, error) { panic("provider failed") }}
	g := New(provider.Name(), "model")
	g.Register(provider)
	var receipt SynthesisReceipt
	ctx := WithSynthesisObserver(t.Context(), receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { return "ticket", nil }, after: func(_ context.Context, r SynthesisReceipt) error { receipt = r; return nil }})
	func() {
		defer func() {
			if recover() == nil {
				t.Error("provider panic swallowed")
			}
		}()
		g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "voice", Text: "测试。"})
	}()
	if receipt.Outcome != SynthesisUnknown || provider.calls.Load() != 1 {
		t.Fatalf("panic lost its reservation: %+v", receipt)
	}
}

func TestQwenTransportErrorRemainsUnknown(t *testing.T) {
	var calls atomic.Int64
	client := &http.Client{Transport: receiptRoundTripper(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("transport response lost")
	})}
	g := New(ProviderQwen, "model")
	g.Register(NewQwenProvider(QwenConfig{APIKey: "fake", TTSBaseURL: "https://not-called.invalid", Client: client}))
	var receipt SynthesisReceipt
	ctx := WithSynthesisObserver(t.Context(), receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { return "ticket", nil }, after: func(_ context.Context, r SynthesisReceipt) error { receipt = r; return nil }})
	_, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "voice", Text: "测试。"})
	if err == nil || DefinitelyNotSent(err) || receipt.Outcome != SynthesisUnknown || calls.Load() != 1 {
		t.Fatalf("err=%v receipt=%+v calls=%d", err, receipt, calls.Load())
	}
}

type receiptRoundTripper func(*http.Request) (*http.Response, error)

func (fn receiptRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
