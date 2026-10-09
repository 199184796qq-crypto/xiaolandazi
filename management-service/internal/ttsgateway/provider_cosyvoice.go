package ttsgateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type DesignRequest struct {
	Model         string
	PreferredName string
	Description   string
	PreviewText   string
}

type DesignResponse struct {
	VoiceID      string
	Model        string
	Provider     string
	PreviewAudio []byte
}

type voiceDesigner interface {
	DesignVoice(context.Context, DesignRequest) (DesignResponse, error)
}

// Used to clean up voices created by an explicit integration check. User voice
// library deletion continues to use the existing tenant-scoped disable flow.
func (g *Gateway) DeleteCheckVoice(ctx context.Context, model, voiceID string) error {
	p, _, err := g.provider(ProviderForModel(model))
	if err != nil {
		return err
	}
	cosy, ok := p.(*cosyVoiceProvider)
	if !ok || !IsCosyVoiceModel(model) || !strings.HasPrefix(voiceID, model+"-") {
		return errors.New("校验音色不属于指定模型")
	}
	_, err = cosy.customize(ctx, map[string]any{"action": "delete_voice", "voice_id": voiceID})
	return err
}

func (g *Gateway) DesignVoice(ctx context.Context, request DesignRequest) (DesignResponse, error) {
	p, _, err := g.provider(ProviderForModel(request.Model))
	if err != nil {
		return DesignResponse{}, err
	}
	designer, ok := p.(voiceDesigner)
	if !ok {
		return DesignResponse{}, errors.New("当前模型不支持声音设计")
	}
	return designer.DesignVoice(ctx, request)
}

type cosyVoiceProvider struct{ transport *qwenProvider }

func NewCosyVoiceProvider(config QwenConfig) Provider {
	return &cosyVoiceProvider{transport: NewQwenProvider(config).(*qwenProvider)}
}

func (*cosyVoiceProvider) Name() string { return ProviderCosyVoice }

func (p *cosyVoiceProvider) SynthesizeURL(ctx context.Context, request SynthesizeRequest) (SynthesizeResponse, error) {
	if !IsCosyVoiceModel(request.Model) {
		return SynthesizeResponse{}, errors.New("CosyVoice模型不支持")
	}
	if CharacterCount(request.Instruction) > 100 {
		return SynthesizeResponse{}, errors.New("CosyVoice表达指令不能超过100个字符（汉字按2个字符计算）")
	}
	result, err := p.transport.SynthesizeURL(ctx, request)
	result.Provider = ProviderCosyVoice
	return result, err
}

func CharacterCount(text string) int {
	n := 0
	for _, r := range text {
		if (r >= 0x3400 && r <= 0x4dbf) || (r >= 0x4e00 && r <= 0x9fff) || (r >= 0xf900 && r <= 0xfaff) || (r >= 0x20000 && r <= 0x323af) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

type cosyCustomizationOutput struct {
	VoiceID      string `json:"voice_id"`
	Status       string `json:"status"`
	TargetModel  string `json:"target_model"`
	PreviewAudio struct {
		Data   string `json:"data"`
		Format string `json:"response_format"`
	} `json:"preview_audio"`
}

func (p *cosyVoiceProvider) customize(ctx context.Context, input map[string]any) (cosyCustomizationOutput, error) {
	var output cosyCustomizationOutput
	if p.transport.apiKey == "" {
		return output, errors.New("DASHSCOPE_API_KEY not configured")
	}
	raw, err := json.Marshal(map[string]any{"model": "voice-enrollment", "input": input, "parameters": map[string]any{"sample_rate": 24000, "response_format": "wav"}})
	if err != nil {
		return output, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.transport.customizationURL, bytes.NewReader(raw))
	if err != nil {
		return output, err
	}
	req.Header.Set("Authorization", "Bearer "+p.transport.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.transport.client.Do(req)
	if err != nil {
		return output, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return output, err
	}
	var envelope struct {
		Output  cosyCustomizationOutput `json:"output"`
		Code    string                  `json:"code"`
		Message string                  `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return output, fmt.Errorf("CosyVoice返回格式无效（HTTP %d）", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || envelope.Code != "" {
		return output, fmt.Errorf("CosyVoice音色创建失败（HTTP %d，%s）：%s", resp.StatusCode, envelope.Code, envelope.Message)
	}
	return envelope.Output, nil
}

func (p *cosyVoiceProvider) waitReady(ctx context.Context, voiceID, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for {
		out, err := p.customize(ctx, map[string]any{"action": "query_voice", "voice_id": voiceID})
		if err != nil {
			return err
		}
		if out.TargetModel != "" && out.TargetModel != target {
			return errors.New("音色绑定的模型与所选模型不一致")
		}
		switch out.Status {
		case "OK":
			return nil
		case "UNDEPLOYED":
			return errors.New("音色审核未通过，请换一段清晰录音或调整声音描述")
		case "DEPLOYING":
		default:
			return fmt.Errorf("音色状态无效：%s", out.Status)
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.New("音色仍在处理中，请稍后重试")
		case <-timer.C:
		}
	}
}

func (p *cosyVoiceProvider) CloneVoice(ctx context.Context, request CloneRequest) (CloneResponse, error) {
	if !IsCosyVoiceModel(request.TargetModel) {
		return CloneResponse{}, errors.New("CosyVoice模型不支持")
	}
	if strings.TrimSpace(request.AudioURL) == "" {
		return CloneResponse{}, errors.New("声音复刻需要可访问的录音地址")
	}
	out, err := p.customize(ctx, map[string]any{"action": "create_voice", "target_model": request.TargetModel, "prefix": qwenAudioVoicePrefix(request.PreferredName), "url": request.AudioURL})
	if err != nil {
		return CloneResponse{}, err
	}
	if out.VoiceID == "" {
		return CloneResponse{}, errors.New("CosyVoice未返回音色ID")
	}
	if err := p.waitReady(ctx, out.VoiceID, request.TargetModel); err != nil {
		return CloneResponse{}, err
	}
	return CloneResponse{VoiceID: out.VoiceID, Model: request.TargetModel, Provider: ProviderCosyVoice}, nil
}

func (p *cosyVoiceProvider) DesignVoice(ctx context.Context, request DesignRequest) (DesignResponse, error) {
	if !IsCosyVoiceModel(request.Model) {
		return DesignResponse{}, errors.New("CosyVoice模型不支持")
	}
	if strings.TrimSpace(request.Description) == "" || CharacterCount(request.Description) > 500 {
		return DesignResponse{}, errors.New("声音描述不能为空，且不能超过500个字符（汉字按2个字符计算）")
	}
	if n := CharacterCount(request.PreviewText); n < 15 || n > 200 {
		return DesignResponse{}, errors.New("试听文案需要15～200个字符（汉字按2个字符计算）")
	}
	out, err := p.customize(ctx, map[string]any{"action": "create_voice", "target_model": request.Model, "prefix": qwenAudioVoicePrefix(request.PreferredName), "voice_prompt": request.Description, "preview_text": request.PreviewText, "language_hints": []string{"zh"}})
	if err != nil {
		return DesignResponse{}, err
	}
	if out.VoiceID == "" || out.PreviewAudio.Format != "wav" {
		return DesignResponse{}, errors.New("CosyVoice未返回有效音色或WAV试听声音")
	}
	audio, err := base64.StdEncoding.DecodeString(out.PreviewAudio.Data)
	if err != nil || len(audio) == 0 {
		return DesignResponse{}, errors.New("CosyVoice试听声音数据无效")
	}
	if err := p.waitReady(ctx, out.VoiceID, request.Model); err != nil {
		return DesignResponse{}, err
	}
	return DesignResponse{VoiceID: out.VoiceID, Model: request.Model, Provider: ProviderCosyVoice, PreviewAudio: audio}, nil
}
