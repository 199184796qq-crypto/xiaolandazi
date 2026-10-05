package agentgateway

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	ProviderQwen     = "qwen"
	DefaultQwenModel = "qwen3.8-flash"
)

type ResponseFormat string

const (
	ResponseText ResponseFormat = "text"
	ResponseJSON ResponseFormat = "json"
)

type Message struct {
	Role      string
	Content   string
	ImageURLs []string
}

type Request struct {
	Stage          string
	Provider       string
	Model          string
	Messages       []Message
	MaxTokens      int
	EnableThinking bool
	ResponseFormat ResponseFormat
	Timeout        time.Duration
}

type Response struct {
	Text         string
	Provider     string
	Model        string
	LatencyMS    int64
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

type ModelDescriptor struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

type ModelLister interface {
	ListModels(context.Context) ([]ModelDescriptor, error)
}

type Provider interface {
	Name() string
	Complete(context.Context, Request) (Response, error)
}

type Gateway struct {
	speechModels    SpeechModelResolver
	mu              sync.RWMutex
	providers       map[string]Provider
	aliases         map[string]string
	defaultProvider string
	defaultModel    string
}

func New(defaultProvider, defaultModel string) *Gateway {
	return &Gateway{
		providers:       make(map[string]Provider),
		aliases:         make(map[string]string),
		defaultProvider: normalizeName(defaultProvider),
		defaultModel:    strings.TrimSpace(defaultModel),
	}
}

func NewFromEnv() *Gateway {
	providerName := normalizeName(os.Getenv("AGENT_PROVIDER"))
	if providerName == "" {
		providerName = ProviderQwen
	}
	model := strings.TrimSpace(os.Getenv("LIVE_AGENT_MODEL"))
	if model == "" && providerName != "compatible" {
		model = DefaultQwenModel
	}
	gateway := New(providerName, model)
	gateway.Register(NewQwenProvider(QwenConfigFromEnv()), "qwen_dashscope", "dashscope")
	if strings.TrimSpace(os.Getenv("AGENT_COMPATIBLE_BASE_URL")) != "" {
		gateway.Register(NewCompatibleProvider(QwenConfig{BaseURL: os.Getenv("AGENT_COMPATIBLE_BASE_URL"), APIKey: os.Getenv("AGENT_COMPATIBLE_API_KEY")}))
	}
	return gateway
}

func (g *Gateway) Register(provider Provider, aliases ...string) {
	if provider == nil {
		return
	}
	name := normalizeName(provider.Name())
	if name == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.providers[name] = provider
	g.aliases[name] = name
	for _, alias := range aliases {
		alias = normalizeName(alias)
		if alias != "" {
			g.aliases[alias] = name
		}
	}
}

func (g *Gateway) Complete(ctx context.Context, request Request) (Response, error) {
	if g == nil {
		return Response{}, errors.New("agent gateway is nil")
	}
	if response, handled, err := g.completeSpeechModel(ctx, request); handled {
		return response, err
	}
	providerName := normalizeName(request.Provider)
	// Stage overrides are operator-owned. Repairs pin the response's model and
	// provider explicitly, so a retry cannot silently switch models.
	stageKey := ""
	switch request.Stage {
	case "style_analysis":
		stageKey = "ANCHOR_ANALYSIS"
	case "speech_generation":
		stageKey = "ANCHOR_GENERATION"
	}
	if providerName == "" && stageKey != "" {
		providerName = normalizeName(os.Getenv(stageKey + "_PROVIDER"))
	}
	if providerName == "" {
		providerName = g.defaultProvider
	}
	model := strings.TrimSpace(request.Model)
	if model == "" && stageKey != "" {
		model = strings.TrimSpace(os.Getenv(stageKey + "_MODEL"))
	}
	if model == "" && providerName == g.defaultProvider {
		model = g.defaultModel
	}
	if providerName == "compatible" && model == "" {
		return Response{}, errors.New("compatible provider requires an explicit stage model")
	}
	if model == "" {
		model = g.defaultModel
	}
	if request.MaxTokens <= 0 {
		request.MaxTokens = 500
	}
	if request.Timeout <= 0 {
		request.Timeout = 20 * time.Second
	}
	request.Provider = providerName
	request.Model = model

	g.mu.RLock()
	canonical := g.aliases[providerName]
	if canonical == "" {
		canonical = providerName
	}
	provider := g.providers[canonical]
	g.mu.RUnlock()
	if provider == nil {
		return Response{}, errors.New("unknown agent provider: " + providerName)
	}
	return provider.Complete(ctx, request)
}

func (g *Gateway) ListModels(ctx context.Context, providerName string) ([]ModelDescriptor, error) {
	if g == nil {
		return nil, errors.New("agent gateway is nil")
	}
	providerName = normalizeName(providerName)
	if providerName == "" {
		providerName = g.defaultProvider
	}
	g.mu.RLock()
	canonical := g.aliases[providerName]
	if canonical == "" {
		canonical = providerName
	}
	provider := g.providers[canonical]
	g.mu.RUnlock()
	if provider == nil {
		return nil, errors.New("unknown agent provider: " + providerName)
	}
	lister, ok := provider.(ModelLister)
	if !ok {
		return nil, errors.New("agent provider does not expose model catalog: " + providerName)
	}
	return lister.ListModels(ctx)
}

func normalizeName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
