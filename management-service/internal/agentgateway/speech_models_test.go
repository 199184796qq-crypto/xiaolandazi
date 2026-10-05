package agentgateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func speechFixture() SpeechModel {
	return SpeechModel{ID: "fixture", Name: "测试模型", Protocol: "openai_chat", BaseURL: "https://api.example.com/v1", Model: "fixture-model", Enabled: true, TimeoutMS: 20000, MaxTokens: 16000, APIKey: "fixture-only-secret"}
}

func TestSpeechKeysEncryptedBoundToIdentity(t *testing.T) {
	secret := strings.Repeat("x", 32)
	one, err := SealSpeechKey(secret, "one", "fixture-only-secret")
	if err != nil {
		t.Fatal(err)
	}
	two, _ := SealSpeechKey(secret, "one", "fixture-only-secret")
	if string(one) == string(two) || strings.Contains(string(one), "fixture-only-secret") {
		t.Fatal("nonce reuse or plaintext storage")
	}
	plain, err := OpenSpeechKey(secret, "one", one)
	if err != nil || plain != "fixture-only-secret" {
		t.Fatal("roundtrip failed")
	}
	if _, err := OpenSpeechKey(secret, "two", one); err == nil {
		t.Fatal("key transplanted to another profile")
	}
	if _, err := OpenSpeechKey(strings.Repeat("y", 32), "one", one); err == nil {
		t.Fatal("wrong server key accepted")
	}
	if _, err := SealSpeechKey("", "one", "key"); err == nil {
		t.Fatal("missing master key accepted")
	}
	p := speechFixture()
	raw, _ := (SpeechModels{Profiles: []SpeechModel{p}}).PublicJSON()
	if strings.Contains(string(raw), p.APIKey) || strings.Contains(string(raw), "\"api_key\"") {
		t.Fatal("secret returned to frontend")
	}
}

func TestSpeechModelValidation(t *testing.T) {
	for _, address := range []string{"http://api.example.com", "https://key@api.example.com", "https://api.example.com?key=secret", "https://api.example.com#secret"} {
		p := speechFixture()
		p.BaseURL = address
		if ValidateSpeechModel(p) == nil {
			t.Errorf("accepted invalid address: %s", address)
		}
	}
	p := speechFixture()
	if err := ValidateSpeechModels(SpeechModelsInput{Profiles: []SpeechModelInput{{SpeechModel: p}}, DefaultID: p.ID}); err != nil {
		t.Fatal(err)
	}
	p.Enabled = false
	if ValidateSpeechModels(SpeechModelsInput{Profiles: []SpeechModelInput{{SpeechModel: p}}, DefaultID: p.ID}) == nil {
		t.Fatal("disabled default accepted")
	}
	if ValidateSpeechModels(SpeechModelsInput{DefaultID: "missing"}) == nil {
		t.Fatal("missing default accepted")
	}
}

func TestDashScopeQwenEndpointDetection(t *testing.T) {
	for _, address := range []string{
		"https://dashscope.aliyuncs.com/compatible-mode/v1",
		"https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
		"https://workspace.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
	} {
		if !dashScopeQwenEndpoint(address, "qwen3.8-omni-flash") {
			t.Fatalf("official DashScope Qwen endpoint was not detected: %s", address)
		}
	}
	for _, item := range []struct{ address, model string }{
		{"https://api.example.com/v1", "qwen3.8-omni-flash"},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "deepseek-v4-pro"},
		{"https://dashscope.aliyuncs.com.evil.example/v1", "qwen3.8-omni-flash"},
	} {
		if dashScopeQwenEndpoint(item.address, item.model) {
			t.Fatalf("generic endpoint was misclassified: %+v", item)
		}
	}
}

func TestOfficialDeepSeekThinkingControl(t *testing.T) {
	for _, tc := range []struct {
		name, address, model string
		disabled             bool
	}{
		{"official", "https://api.deepseek.com", "deepseek-v4-pro", true},
		{"official_v1", "https://api.deepseek.com/v1", "deepseek-flash", true},
		{"other_model", "https://api.deepseek.com", "qwen3.8-flash", false},
		{"lookalike_host", "https://api.deepseek.com.evil.example/v1", "deepseek-v4-pro", false},
		{"generic_proxy", "https://api.example.com/v1", "deepseek-v4-pro", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{"enable_thinking": false, "reasoning_effort": "none"}
			applyCompatibleRequestOptions(payload, tc.address, tc.model, false)
			if _, exists := payload["enable_thinking"]; exists {
				t.Fatal("Qwen-specific enable_thinking leaked to compatible endpoint")
			}
			if _, exists := payload["reasoning_effort"]; exists {
				t.Fatal("reasoning_effort leaked to compatible endpoint")
			}
			thinking, exists := payload["thinking"]
			if exists != tc.disabled {
				t.Fatalf("thinking present=%v, want %v", exists, tc.disabled)
			}
			if tc.disabled {
				option, ok := thinking.(map[string]string)
				if !ok || option["type"] != "disabled" {
					t.Fatalf("bad DeepSeek thinking option: %#v", thinking)
				}
			}
		})
	}

	payload := map[string]any{"enable_thinking": true}
	applyCompatibleRequestOptions(payload, "https://api.deepseek.com", "deepseek-v4-pro", true)
	if _, exists := payload["thinking"]; exists {
		t.Fatal("enabled thinking should preserve DeepSeek's model default")
	}
}

func TestSpeechProtocolRequestsAndResponses(t *testing.T) {
	for _, tc := range []struct{ protocol, path, header, value, reply string }{
		{"openai_chat", "/v1/chat/completions", "Authorization", "Bearer fixture-only-secret", `{"choices":[{"message":{"content":"哥哥姐们，盖好啊。"}}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`},
		{"openai_responses", "/v1/responses", "Authorization", "Bearer fixture-only-secret", `{"output":[{"content":[{"type":"output_text","text":"哥哥姐们，盖好啊。"}]}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`},
		{"gemini", "/v1/models/fixture-model:generateContent", "x-goog-api-key", "fixture-only-secret", `{"candidates":[{"content":{"parts":[{"text":"private reasoning","thought":true},{"text":"哥哥姐们，盖好啊。"}]}}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4,"totalTokenCount":7}}`},
		{"anthropic", "/v1/messages", "x-api-key", "fixture-only-secret", `{"content":[{"type":"thinking","text":"private reasoning"},{"type":"text","text":"哥哥姐们，盖好啊。"}],"usage":{"input_tokens":3,"output_tokens":4}}`},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path || r.Header.Get(tc.header) != tc.value {
					t.Errorf("bad protocol transport: %s", r.URL.Path)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				encoded, _ := json.Marshal(payload)
				if !strings.Contains(string(encoded), "规范") || !strings.Contains(string(encoded), "怎么保存") {
					t.Error("system/user prompt lost")
				}
				if _, exists := payload["enable_thinking"]; exists {
					t.Error("vendor-specific option leaked")
				}
				if tc.protocol == "anthropic" && r.Header.Get("anthropic-version") == "" {
					t.Error("missing protocol version")
				}
				_, _ = io.WriteString(w, tc.reply)
			}))
			defer server.Close()
			p := speechFixture()
			p.Protocol = tc.protocol
			p.BaseURL = server.URL + "/v1"
			response, err := completeSpeechEndpoint(context.Background(), p, Request{Model: p.Model, Messages: []Message{{Role: "system", Content: "规范"}, {Role: "user", Content: "怎么保存"}}, MaxTokens: 64, Timeout: time.Second, ResponseFormat: ResponseJSON}, server.Client())
			if err != nil || response.Text != "哥哥姐们，盖好啊。" || response.TotalTokens != 7 {
				t.Fatalf("bad response: %+v %v", response, err)
			}
		})
	}
}

type resolverFixture struct {
	calls   int
	profile *SpeechModel
	err     error
	id      string
}

func (r *resolverFixture) ResolveSpeechModel(_ context.Context, id string) (*SpeechModel, error) {
	r.calls++
	r.id = id
	return r.profile, r.err
}

func TestSpeechRoutingIsolatedFromAgentAndAnalysis(t *testing.T) {
	r := &resolverFixture{err: errors.New("settings unavailable")}
	g := New("fixture", "original-model").WithSpeechModels(r)
	g.Register(fakeSpeechAgent{})
	for _, request := range []Request{{Stage: "style_analysis"}, {Stage: "intent"}, {Stage: "speech_generation", Provider: "fixture", Model: "pinned-original"}} {
		response, err := g.Complete(context.Background(), request)
		if err != nil || response.Provider != "fixture" {
			t.Fatalf("agent route changed: %+v %v", response, err)
		}
	}
	if r.calls != 0 {
		t.Fatal("agent or analysis consulted speech configuration")
	}
	if _, err := g.Complete(context.Background(), Request{Stage: "speech_generation"}); err == nil {
		t.Fatal("silently fell back after configuration failure")
	}
	r.err = nil
	r.profile = nil
	if _, err := g.Complete(context.Background(), Request{Stage: "speech_generation"}); err != nil {
		t.Fatal("empty configuration did not preserve old route", err)
	}
	p := speechFixture()
	p.Enabled = false
	r.profile = &p
	if _, err := g.Complete(context.Background(), Request{Stage: "speech_generation"}); err == nil {
		t.Fatal("disabled model generated text")
	}
	_, _ = g.Complete(context.Background(), Request{Provider: "anchor:fixture", Model: "fixture-model"})
	if r.id != "fixture" {
		t.Fatal("repair lost pinned endpoint")
	}
}

type fakeSpeechAgent struct{}

func (fakeSpeechAgent) Name() string { return "fixture" }
func (fakeSpeechAgent) Complete(_ context.Context, r Request) (Response, error) {
	return Response{Provider: "fixture", Model: r.Model, Text: "original"}, nil
}

func TestSpeechTransportBlocksPrivateNetworks(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "::1", "fc00::1", "fe80::1", "0.0.0.0"} {
		if publicModelIP(net.ParseIP(ip)) {
			t.Errorf("allowed private/metadata address: %s", ip)
		}
	}
	p := speechFixture()
	p.BaseURL = "https://127.0.0.1/v1"
	_, err := CompleteSpeechEndpoint(context.Background(), p, Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil || strings.Contains(err.Error(), p.APIKey) {
		t.Fatal("private URL accepted or secret leaked")
	}
	if err := speechHTTPClient.CheckRedirect(nil, nil); err == nil {
		t.Fatal("redirect accepted")
	}
}

func TestSpeechErrorsDoNotEchoProviderBodyOrCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, "fixture-only-secret")
	}))
	defer server.Close()
	for _, protocol := range []string{"openai_chat", "openai_responses", "gemini", "anthropic"} {
		p := speechFixture()
		p.Protocol = protocol
		p.BaseURL = server.URL
		_, err := completeSpeechEndpoint(context.Background(), p, Request{Model: p.Model, Messages: []Message{{Role: "user", Content: "hi"}}}, server.Client())
		if err == nil || strings.Contains(err.Error(), p.APIKey) {
			t.Errorf("unsafe error for %s: %v", protocol, err)
		}
	}
}

func TestSelectedSpeechModelKeepsBusinessMessagesAndPinnedRepair(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		raw, _ := json.Marshal(payload)
		for _, text := range []string{"基础规范", "业务事实", "测试问题"} {
			if !strings.Contains(string(raw), text) {
				t.Errorf("lost prompt: %s", text)
			}
		}
		if payload["model"] != "fixture-model" || payload["max_completion_tokens"] != float64(64) {
			t.Errorf("bad model/token routing: %s", raw)
		}
		if _, exists := payload["max_tokens"]; exists {
			t.Error("old token option leaked")
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"哥哥姐们，盖好啊。"}}]}`)
	}))
	defer server.Close()
	previous := speechHTTPClient
	speechHTTPClient = server.Client()
	defer func() { speechHTTPClient = previous }()
	p := speechFixture()
	p.BaseURL = server.URL
	p.ChatTokenField = "max_completion_tokens"
	p.SystemPrompt = "基础规范"
	r := &resolverFixture{profile: &p}
	g := New("fixture", "original-model").WithSpeechModels(r)
	request := Request{Stage: "speech_generation", Messages: []Message{{Role: "system", Content: "业务事实"}, {Role: "user", Content: "测试问题"}}, MaxTokens: 64, Timeout: time.Second}
	result, err := g.Complete(context.Background(), request)
	if err != nil || result.Provider != "anchor:fixture" || result.Model != p.Model {
		t.Fatalf("selected route failed: %+v %v", result, err)
	}
	request.Provider, request.Model = result.Provider, result.Model
	if _, err := g.Complete(context.Background(), request); err != nil || r.id != p.ID {
		t.Fatal("repair did not use pinned model", err)
	}
	if len(request.Messages) != 2 || request.Messages[0].Content != "业务事实" {
		t.Fatal("input messages mutated")
	}
}
