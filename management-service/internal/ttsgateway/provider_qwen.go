package ttsgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type QwenConfig struct {
	APIKey           string
	TTSBaseURL       string
	CustomizationURL string
	Client           *http.Client
}

type qwenProvider struct {
	apiKey           string
	ttsBaseURL       string
	customizationURL string
	client           *http.Client
}

func QwenConfigFromEnv() QwenConfig {
	key := strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY"))
	if key == "" {
		key = strings.TrimSpace(windowsUserEnv("DASHSCOPE_API_KEY"))
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("DASHSCOPE_TTS_BASE_URL")), "/")
	if base == "" {
		base = "https://dashscope.aliyuncs.com/api/v1"
	}
	custom := strings.TrimSpace(os.Getenv("DASHSCOPE_TTS_CUSTOMIZATION_URL"))
	if custom == "" {
		custom = "https://dashscope.aliyuncs.com/api/v1/services/audio/tts/customization"
	}
	return QwenConfig{APIKey: key, TTSBaseURL: base, CustomizationURL: custom}
}

func NewQwenProvider(config QwenConfig) Provider {
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	return &qwenProvider{
		apiKey:           strings.TrimSpace(config.APIKey),
		ttsBaseURL:       strings.TrimRight(strings.TrimSpace(config.TTSBaseURL), "/"),
		customizationURL: strings.TrimSpace(config.CustomizationURL),
		client:           client,
	}
}

func (p *qwenProvider) Name() string { return ProviderQwen }

func (p *qwenProvider) SynthesizeURL(ctx context.Context, request SynthesizeRequest) (SynthesizeResponse, error) {
	if p.apiKey == "" {
		return SynthesizeResponse{}, errors.New("DASHSCOPE_API_KEY not configured")
	}
	if strings.TrimSpace(request.Model) == "" || strings.TrimSpace(request.VoiceID) == "" || strings.TrimSpace(request.Text) == "" {
		return SynthesizeResponse{}, errors.New("model, voice_id and text are required")
	}
	payload := map[string]any{
		"model": request.Model,
		"input": map[string]any{
			"text":        request.Text,
			"voice":       request.VoiceID,
			"format":      "wav",
			"sample_rate": 24000,
			"rate":        request.Rate,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return SynthesizeResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.ttsBaseURL+"/services/audio/tts/SpeechSynthesizer", bytes.NewReader(raw))
	if err != nil {
		return SynthesizeResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return SynthesizeResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return SynthesizeResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SynthesizeResponse{}, fmt.Errorf("qwen tts provider http %d", resp.StatusCode)
	}
	var result struct {
		Output struct {
			Audio struct {
				URL string `json:"url"`
			} `json:"audio"`
		} `json:"output"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return SynthesizeResponse{}, err
	}
	audioURL := strings.TrimSpace(result.Output.Audio.URL)
	if audioURL == "" {
		if strings.TrimSpace(result.Message) != "" {
			return SynthesizeResponse{}, errors.New(result.Message)
		}
		return SynthesizeResponse{}, errors.New("qwen tts provider returned no audio URL")
	}
	return SynthesizeResponse{AudioURL: audioURL, Provider: ProviderQwen, Model: request.Model, VoiceID: request.VoiceID, Rate: request.Rate}, nil
}

func (p *qwenProvider) CloneVoice(ctx context.Context, request CloneRequest) (CloneResponse, error) {
	if p.apiKey == "" {
		return CloneResponse{}, errors.New("DASHSCOPE_API_KEY not configured")
	}
	if strings.TrimSpace(request.PreferredName) == "" || strings.TrimSpace(request.AudioData) == "" || strings.TrimSpace(request.TargetModel) == "" {
		return CloneResponse{}, errors.New("preferred_name, audio and target_model are required")
	}
	payload := map[string]any{
		"model": "qwen-voice-enrollment",
		"input": map[string]any{
			"action":         "create",
			"target_model":   request.TargetModel,
			"preferred_name": request.PreferredName,
			"audio":          map[string]any{"data": request.AudioData},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return CloneResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.customizationURL, bytes.NewReader(raw))
	if err != nil {
		return CloneResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return CloneResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return CloneResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CloneResponse{}, fmt.Errorf("qwen voice clone provider http %d", resp.StatusCode)
	}
	var result struct {
		Output struct {
			Voice   string `json:"voice"`
			VoiceID string `json:"voice_id"`
		} `json:"output"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return CloneResponse{}, err
	}
	voiceID := strings.TrimSpace(result.Output.Voice)
	if voiceID == "" {
		voiceID = strings.TrimSpace(result.Output.VoiceID)
	}
	if voiceID == "" {
		if strings.TrimSpace(result.Message) != "" {
			return CloneResponse{}, errors.New(result.Message)
		}
		return CloneResponse{}, errors.New("qwen voice clone provider returned no voice id")
	}
	return CloneResponse{VoiceID: voiceID, Provider: ProviderQwen, Model: request.TargetModel}, nil
}
