package ttsgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	SynthesisSucceeded    = "success"
	SynthesisNotSent      = "definitely_not_sent"
	SynthesisUnknown      = "unknown"
	UsageSupplierResponse = "supplier_response"
	UsageSentTextEstimate = "sent_text_estimate"
)

// SynthesisUsage contains only supplier-returned measurements. Nil means absent;
// a present zero is still a real measurement. Characters and tokens never share
// a unit. Neither the audio file length nor playback duration is vendor usage.
// Sources (checked 2026-10-25):
// https://www.alibabacloud.com/help/en/model-studio/qwen-audio-tts-http-api
// https://help.aliyun.com/zh/model-studio/cosyvoice-tts-http-api
// https://www.alibabacloud.com/help/en/model-studio/audio-generation-api
// Tencent WSv2 supplies request_id but no billing usage:
// https://cloud.tencent.com/document/api/1073/108595
type SynthesisUsage struct {
	Present         bool     `json:"present"`
	Invalid         bool     `json:"invalid,omitempty"`
	Characters      *int64   `json:"characters,omitempty"`
	CharacterField  string   `json:"character_field,omitempty"`
	InputTokens     *int64   `json:"input_tokens,omitempty"`
	OutputTokens    *int64   `json:"output_tokens,omitempty"`
	TotalTokens     *int64   `json:"total_tokens,omitempty"`
	DurationSeconds *float64 `json:"duration_seconds,omitempty"`
}

// SynthesisAttempt deliberately excludes text, credentials, and audio URLs.
// SentTextCharacters is an estimate in Unicode code points, not a vendor bill.
type SynthesisAttempt struct {
	Provider           string    `json:"provider"`
	Model              string    `json:"model"`
	VoiceID            string    `json:"voice_id"`
	Rate               float64   `json:"rate"`
	SentTextCharacters int64     `json:"sent_text_characters"`
	TextSHA            string    `json:"text_sha"`
	InstructionSHA     string    `json:"instruction_sha"`
	StartedAt          time.Time `json:"started_at"`
}

type SynthesisReceipt struct {
	SynthesisAttempt
	Ticket            string         `json:"ticket"`
	ProviderRequestID string         `json:"provider_request_id,omitempty"`
	Outcome           string         `json:"outcome"`
	Usage             SynthesisUsage `json:"usage"`
	UsageSource       string         `json:"usage_source"`
	FinishedAt        time.Time      `json:"finished_at"`
}

// BeforeSynthesis must persist its reservation before permitting the request.
// AfterSynthesis receives exactly one receipt per permitted provider attempt.
// Returning an error after success cannot discard the generated audio.
type SynthesisObserver interface {
	BeforeSynthesis(context.Context, SynthesisAttempt) (string, error)
	AfterSynthesis(context.Context, SynthesisReceipt) error
}

type synthesisObserverKey struct{}

// WithSynthesisObserver scopes accounting to a call tree, never a global gateway.
// Cloning, voice design, cache reads, and unrelated previews are not observed.
func WithSynthesisObserver(ctx context.Context, observer SynthesisObserver) context.Context {
	if observer == nil {
		// A child operation can explicitly opt out of an inherited observer.
		return context.WithValue(ctx, synthesisObserverKey{}, struct{}{})
	}
	return context.WithValue(ctx, synthesisObserverKey{}, observer)
}

type synthesisNotSentError struct{ cause error }

func (e *synthesisNotSentError) Error() string { return e.cause.Error() }
func (e *synthesisNotSentError) Unwrap() error { return e.cause }
func notSent(err error) error {
	if err == nil {
		return nil
	}
	return &synthesisNotSentError{cause: err}
}

// DefinitelyNotSent is true only for local validation/preparation failures.
// HTTP failures, disconnects and cancellation after a request may have been sent
// remain unknown and must not be automatically refunded.
func DefinitelyNotSent(err error) bool {
	var target *synthesisNotSentError
	return errors.As(err, &target)
}

func digestSynthesisText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func observeSynthesis(ctx context.Context, provider Provider, request SynthesizeRequest) (result SynthesizeResponse, err error) {
	if err := ctx.Err(); err != nil {
		return result, notSent(err)
	}
	text := request.Text
	if request.Provider == ProviderTencent {
		text = strings.TrimSpace(text)
	}
	attempt := SynthesisAttempt{
		Provider: request.Provider, Model: request.Model, VoiceID: request.VoiceID,
		Rate: request.Rate, SentTextCharacters: int64(utf8.RuneCountInString(text)),
		TextSHA: digestSynthesisText(text), InstructionSHA: digestSynthesisText(request.Instruction),
		StartedAt: time.Now().UTC(),
	}
	observer, _ := ctx.Value(synthesisObserverKey{}).(SynthesisObserver)
	ticket := ""
	if observer != nil {
		ticket, err = observer.BeforeSynthesis(ctx, attempt)
		if err != nil {
			return result, notSent(err)
		}
	}
	finished := false
	defer func() {
		result.UsageSource = synthesisUsageSource(result.Usage)
		if observer == nil {
			return
		}
		outcome := SynthesisUnknown
		if finished && err == nil {
			outcome = SynthesisSucceeded
		}
		if DefinitelyNotSent(err) {
			outcome = SynthesisNotSent
		}
		receipt := SynthesisReceipt{
			SynthesisAttempt: attempt, Ticket: ticket, ProviderRequestID: result.ProviderRequestID,
			Outcome: outcome, Usage: result.Usage, UsageSource: result.UsageSource, FinishedAt: time.Now().UTC(),
		}
		// A cancelled media request must still settle its paid/unknown attempt.
		settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		result.AccountingPending = !settleSynthesis(settleCtx, observer, receipt)
	}()
	if err = ctx.Err(); err != nil {
		return result, notSent(err)
	}
	result, err = provider.SynthesizeURL(ctx, request)
	finished = true
	return result, err
}

func settleSynthesis(ctx context.Context, observer SynthesisObserver, receipt SynthesisReceipt) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return observer.AfterSynthesis(ctx, receipt) == nil
}

func synthesisUsageSource(usage SynthesisUsage) string {
	if usage.Characters != nil || usage.DurationSeconds != nil || usage.InputTokens != nil || usage.OutputTokens != nil || usage.TotalTokens != nil {
		return UsageSupplierResponse
	}
	return UsageSentTextEstimate
}

// Invalid/missing usage is never allowed to destroy an otherwise usable audio
// response: retain the audio and mark the measurement unavailable for billing.
func parseSynthesisUsage(raw json.RawMessage) SynthesisUsage {
	if len(raw) == 0 || string(raw) == "null" {
		return SynthesisUsage{}
	}
	usage := SynthesisUsage{Present: true}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		usage.Invalid = true
		return usage
	}
	integer := func(key string) *int64 {
		value, exists := fields[key]
		if !exists || string(value) == "null" {
			return nil
		}
		var count int64
		if json.Unmarshal(value, &count) != nil || count < 0 {
			usage.Invalid = true
			return nil
		}
		return &count
	}
	usage.Characters = integer("characters")
	if usage.Characters != nil {
		usage.CharacterField = "characters"
	} else {
		usage.Characters = integer("input_characters")
		if usage.Characters != nil {
			usage.CharacterField = "input_characters"
		}
	}
	usage.InputTokens = integer("input_tokens")
	usage.OutputTokens = integer("output_tokens")
	usage.TotalTokens = integer("total_tokens")
	if value, exists := fields["duration"]; exists && string(value) != "null" {
		var duration float64
		if json.Unmarshal(value, &duration) != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration < 0 {
			usage.Invalid = true
		} else {
			usage.DurationSeconds = &duration
		}
	}
	return usage
}

func safeProviderRequestID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 128 {
		return ""
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return ""
	}
	return value
}
