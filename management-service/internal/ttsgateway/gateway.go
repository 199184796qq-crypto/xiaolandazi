package ttsgateway

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
)

const (
	ProviderQwen        = "qwen_audio"
	DefaultQwenTTSModel = "qwen-audio-3.0-tts-plus"
)

type SynthesizeRequest struct {
	Provider    string
	Model       string
	VoiceID     string
	Text        string
	Rate        float64
	Instruction string
}

type SynthesizeResponse struct {
	AudioURL string
	Provider string
	Model    string
	VoiceID  string
	Rate     float64
}

type CloneRequest struct {
	Provider      string
	PreferredName string
	AudioData     string
	TargetModel   string
}

type CloneResponse struct {
	VoiceID  string
	Provider string
	Model    string
}

type Provider interface {
	Name() string
	SynthesizeURL(context.Context, SynthesizeRequest) (SynthesizeResponse, error)
	CloneVoice(context.Context, CloneRequest) (CloneResponse, error)
}

type Gateway struct {
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
		defaultProvider: normalize(defaultProvider),
		defaultModel:    strings.TrimSpace(defaultModel),
	}
}

func NewFromEnv() *Gateway {
	name := normalize(os.Getenv("TTS_PROVIDER"))
	if name == "" {
		name = ProviderQwen
	}
	model := strings.TrimSpace(os.Getenv("TTS_MODEL"))
	if model == "" {
		model = DefaultQwenTTSModel
	}
	g := New(name, model)
	g.Register(NewQwenProvider(QwenConfigFromEnv()), "qwen", "dashscope", "qwen_audio_3_0")
	return g
}

func (g *Gateway) Register(provider Provider, aliases ...string) {
	if provider == nil {
		return
	}
	name := normalize(provider.Name())
	if name == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.providers[name] = provider
	g.aliases[name] = name
	for _, alias := range aliases {
		alias = normalize(alias)
		if alias != "" {
			g.aliases[alias] = name
		}
	}
}

func (g *Gateway) provider(name string) (Provider, string, error) {
	name = normalize(name)
	if name == "" {
		name = g.defaultProvider
	}
	g.mu.RLock()
	canonical := g.aliases[name]
	if canonical == "" {
		canonical = name
	}
	p := g.providers[canonical]
	g.mu.RUnlock()
	if p == nil {
		return nil, "", errors.New("unknown tts provider: " + name)
	}
	return p, canonical, nil
}

func (g *Gateway) SynthesizeURL(ctx context.Context, request SynthesizeRequest) (SynthesizeResponse, error) {
	if strings.TrimSpace(request.Model) == "" {
		request.Model = g.defaultModel
	}
	if request.Rate == 0 {
		request.Rate = 1.0
	}
	if request.Rate < 0.5 || request.Rate > 2.0 {
		return SynthesizeResponse{}, errors.New("tts rate must be between 0.5 and 2.0")
	}
	p, canonical, err := g.provider(request.Provider)
	if err != nil {
		return SynthesizeResponse{}, err
	}
	request.Provider = canonical
	return p.SynthesizeURL(ctx, request)
}

func (g *Gateway) CloneVoice(ctx context.Context, request CloneRequest) (CloneResponse, error) {
	p, canonical, err := g.provider(request.Provider)
	if err != nil {
		return CloneResponse{}, err
	}
	request.Provider = canonical
	return p.CloneVoice(ctx, request)
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
