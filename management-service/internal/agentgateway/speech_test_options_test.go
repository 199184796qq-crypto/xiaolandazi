package agentgateway

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

type speechTestTransport func(*http.Request) (*http.Response, error)

func (f speechTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSpeechTestControlsWire(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, base, model string
		thinking                    bool
		temperature                 *float64
		check                       func(*testing.T, map[string]any)
	}{
		{"qwen_on", "openai_chat", "https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen3.7-flash", true, new(0.25), func(t *testing.T, p map[string]any) {
			if p["enable_thinking"] != true || p["temperature"] != 0.25 {
				t.Fatal(p)
			}
		}},
		{"qwen38_off", "openai_chat", "https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen3.8-flash", false, new(0.0), func(t *testing.T, p map[string]any) {
			if p["enable_thinking"] != false || p["temperature"] != 0.0 || p["reasoning_effort"] != nil {
				t.Fatal(p)
			}
		}},
		{"omni_off", "openai_chat", "https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen3.8-omni-flash", false, nil, func(t *testing.T, p map[string]any) {
			if p["reasoning_effort"] != "none" || p["enable_thinking"] != nil {
				t.Fatal(p)
			}
		}},
		{"deepseek_on", "openai_chat", "https://api.deepseek.com/v1", "deepseek-v4-pro", true, nil, func(t *testing.T, p map[string]any) {
			if p["thinking"].(map[string]any)["type"] != "enabled" || p["enable_thinking"] != nil || p["temperature"] != nil {
				t.Fatal(p)
			}
		}},
		{"responses_qwen", "openai_responses", "https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen3.8-max", false, new(0.7), func(t *testing.T, p map[string]any) {
			if p["enable_thinking"] != false || p["temperature"] != 0.7 {
				t.Fatal(p)
			}
		}},
		{"gemini_on", "gemini", "https://generativelanguage.googleapis.com/v1beta", "gemini-2.5-flash", true, new(0.3), func(t *testing.T, p map[string]any) {
			g := p["generationConfig"].(map[string]any)
			if g["temperature"] != 0.3 || g["thinkingConfig"].(map[string]any)["thinkingBudget"] != float64(-1) {
				t.Fatal(p)
			}
		}},
		{"gemini_off", "gemini", "https://generativelanguage.googleapis.com/v1beta", "gemini-2.5-flash", false, new(0.3), func(t *testing.T, p map[string]any) {
			if p["generationConfig"].(map[string]any)["thinkingConfig"].(map[string]any)["thinkingBudget"] != float64(0) {
				t.Fatal(p)
			}
		}},
		{"claude_on", "anthropic", "https://api.anthropic.com/v1", "claude-sonnet-4-5", true, nil, func(t *testing.T, p map[string]any) {
			h := p["thinking"].(map[string]any)
			if h["type"] != "enabled" || h["budget_tokens"] != float64(4096) || p["temperature"] != nil {
				t.Fatal(p)
			}
		}},
		{"claude_off", "anthropic", "https://api.anthropic.com/v1", "claude-sonnet-4-5", false, new(0.5), func(t *testing.T, p map[string]any) {
			if p["thinking"].(map[string]any)["type"] != "disabled" || p["temperature"] != 0.5 {
				t.Fatal(p)
			}
		}},
		{"claude_adaptive", "anthropic", "https://api.anthropic.com/v1", "claude-sonnet-4-6", true, nil, func(t *testing.T, p map[string]any) {
			h := p["thinking"].(map[string]any)
			if h["type"] != "adaptive" || h["budget_tokens"] != nil {
				t.Fatal(p)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := speechFixture()
			p.Protocol, p.BaseURL, p.Model = tc.protocol, tc.base, tc.model
			r := Request{Model: p.Model, Messages: []Message{{Role: "user", Content: "测试问题"}}, MaxTokens: 16000, Timeout: time.Second, EnableThinking: tc.thinking, ThinkingOverride: new(tc.thinking), Temperature: tc.temperature}
			if err := ValidateSpeechTestOptions(p, r); err != nil {
				t.Fatal(err)
			}
			calls := 0
			client := &http.Client{Transport: speechTestTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				var payload map[string]any
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				tc.check(t, payload)
				reply := `{"choices":[{"message":{"content":"测试回答"}}]}`
				if tc.protocol == "openai_responses" {
					reply = `{"output":[{"content":[{"type":"output_text","text":"测试回答"}]}]}`
				}
				if tc.protocol == "gemini" {
					reply = `{"candidates":[{"content":{"parts":[{"text":"测试回答"}]}}]}`
				}
				if tc.protocol == "anthropic" {
					reply = `{"content":[{"type":"text","text":"测试回答"}]}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(reply)), Header: make(http.Header)}, nil
			})}
			response, err := completeSpeechEndpoint(context.Background(), p, r, client)
			if err != nil || response.Text != "测试回答" || calls != 1 {
				t.Fatalf("%+v %v calls=%d", response, err, calls)
			}
		})
	}
}

func TestSpeechTestOptionsRejectUnsupportedAndInvalidControls(t *testing.T) {
	p := speechFixture()
	if err := ValidateSpeechTestOptions(p, Request{}); err != nil {
		t.Fatal("existing requests must retain defaults", err)
	}
	if err := ValidateSpeechTestOptions(p, Request{ThinkingOverride: new(true)}); err == nil {
		t.Fatal("unknown endpoint pretended to support a thinking toggle")
	}
	for _, value := range []float64{-0.1, 2.1, math.NaN(), math.Inf(1)} {
		if ValidateSpeechTestOptions(p, Request{Temperature: new(value)}) == nil {
			t.Fatal("invalid temperature accepted")
		}
	}
	p.Protocol, p.Model = "anthropic", "claude-sonnet-4-5"
	if ValidateSpeechTestOptions(p, Request{Temperature: new(1.1)}) == nil {
		t.Fatal("Anthropic temperature exceeds 1")
	}
	if ValidateSpeechTestOptions(p, Request{Temperature: new(0.7), ThinkingOverride: new(true)}) == nil {
		t.Fatal("thinking mode temperature accepted")
	}
	p.MaxTokens = 1024
	if ValidateSpeechTestOptions(p, Request{ThinkingOverride: new(true)}) == nil {
		t.Fatal("invalid thinking budget accepted")
	}
	p.Protocol, p.Model, p.MaxTokens = "gemini", "gemini-3.1-pro", 16000
	if ValidateSpeechTestOptions(p, Request{ThinkingOverride: new(false)}) == nil {
		t.Fatal("non-switchable reasoning was disabled")
	}
}
