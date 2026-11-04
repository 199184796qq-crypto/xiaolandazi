package ttsgateway

import (
	"context"
	"errors"
	"math"
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
	// ParagraphPath is a server-owned durable WAV destination, never sent to
	// the supplier. Paid audio survives downstream archive/live cancellation.
	ParagraphPath string `json:"-"`
	// LongText is internal to the script-first Volcengine producer. Interactive
	// speech and other providers retain the existing short synthesis contract.
	LongText *LongTextJob
	// Once admitted and paid, drain the bounded supplier response even when the
	// event expires. This does not authorize any subsequent request or playback.
	DrainOnCancel bool `json:"-"`
}

type SynthesizeResponse struct {
	AudioURL          string
	Provider          string
	Model             string
	VoiceID           string
	Rate              float64
	ProviderRequestID string
	Usage             SynthesisUsage
	UsageSource       string
	AudioReused       bool
	// A completed synthesis remains usable if accounting could not be persisted.
	// Callers must retain this audio instead of synthesizing it again.
	AccountingPending bool
	AudioFile         string
	Subtitles         []SpeechSubtitle
}

type CloneRequest struct {
	Provider      string
	PreferredName string
	AudioData     string
	AudioURL      string
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
	interactionAudioCache *InteractionAudioCache
	scheduler             synthesisScheduler
	mu                    sync.RWMutex
	providers             map[string]Provider
	aliases               map[string]string
	defaultProvider       string
	defaultModel          string
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
	g.scheduler = currentScheduler()
	g.Register(NewQwenProvider(QwenConfigFromEnv()), "qwen", "dashscope", "qwen_audio_3_0")
	g.Register(NewCosyVoiceProvider(QwenConfigFromEnv()), "aliyun_cosyvoice", "aliyun_cosyvoice_clone")
	g.Register(NewVolcengineProvider(VolcengineConfigFromEnv()), "doubao", "volcano")
	g.Register(NewTencentProvider(TencentConfigFromEnv()), "tencent", "tencent_cloud")
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
	if math.IsNaN(request.Rate) || math.IsInf(request.Rate, 0) || request.Rate < 0.5 || request.Rate > 2.0 {
		return SynthesizeResponse{}, errors.New("tts rate must be between 0.5 and 2.0")
	}
	if request.Provider == "" && (IsCosyVoiceModel(request.Model) || request.Model == TencentPremiumModel || request.Model == VolcengineICL2) {
		request.Provider = ProviderForModel(request.Model)
	}
	if request.Model == TencentPremiumModel && normalize(request.Provider) != ProviderTencent && normalize(request.Provider) != "tencent" && normalize(request.Provider) != "tencent_cloud" {
		return SynthesizeResponse{}, errors.New("腾讯精品音色必须使用腾讯合成服务")
	}
	p, canonical, err := g.provider(request.Provider)
	if err != nil {
		return SynthesizeResponse{}, err
	}
	if (request.Model == VolcengineICL2) != (canonical == ProviderVolcengine) {
		return SynthesizeResponse{}, notSent(errors.New("火山模型与合成服务不匹配"))
	}
	request.Provider = canonical
	if request.LongText != nil && canonical != ProviderVolcengine {
		return SynthesizeResponse{}, notSent(errors.New("长段合成只支持高仿真语音池"))
	}
	return g.scheduledSynthesis(ctx, p, request)
}

func (g *Gateway) CloneVoice(ctx context.Context, request CloneRequest) (CloneResponse, error) {
	if strings.TrimSpace(request.TargetModel) == VolcengineICL2 {
		return CloneResponse{}, errors.New("火山当前仅接入已有音色合成，不执行复刻训练")
	}
	if request.Provider == "" && IsCosyVoiceModel(request.TargetModel) {
		request.Provider = ProviderCosyVoice
	}
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
