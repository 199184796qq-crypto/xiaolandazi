package ttsgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DeleteOwnedClone is only called with a voice the application has established
// belongs to the customer. The model-prefixed ID provides a second check.
func (g *Gateway) DeleteOwnedClone(ctx context.Context, provider, model, voiceID string) error {
	if !SupportsCloneModel(model) || !strings.HasPrefix(voiceID, model+"-") || ProviderForModel(model) != provider {
		return errors.New("无法验证克隆音色的供应商和模型")
	}
	resolved, _, err := g.provider(provider)
	if err != nil {
		return err
	}
	var transport *qwenProvider
	switch p := resolved.(type) {
	case *cosyVoiceProvider:
		transport = p.transport
	case *qwenProvider:
		transport = p
	default:
		return errors.New("该供应商未接入克隆音色删除")
	}
	if transport.apiKey == "" {
		return errors.New("未配置克隆音色删除服务")
	}
	enrollment, action, key := "voice-enrollment", "delete_voice", "voice_id"
	if !IsCosyVoiceModel(model) && !qwenAudioCloneModel(model) {
		enrollment, action, key = "qwen-voice-enrollment", "delete", "voice"
	}
	raw, err := json.Marshal(map[string]any{"model": enrollment, "input": map[string]string{"action": action, key: voiceID}})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, transport.customizationURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+transport.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := transport.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return fmt.Errorf("读取克隆服务删除回执: %w", err)
	}
	missingVoice := resp.StatusCode == http.StatusBadRequest &&
		(body.Code == "BadRequest.VoiceNotFound" || body.Code == "BadRequest.ResourceNotExist")
	if missingVoice || (resp.StatusCode/100 == 2 && body.Code == "") {
		return nil
	}
	return fmt.Errorf("克隆服务删除失败 HTTP %d (%s)", resp.StatusCode, body.Code)
}
