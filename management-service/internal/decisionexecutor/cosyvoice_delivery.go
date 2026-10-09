package decisionexecutor

import (
	"encoding/json"
	"errors"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/ttsgateway"
	"strings"
)

func (w *Worker) cosyVoiceInstructionForMission(item *decisionItem) (string, error) {
	raw, err := interactionpolicy.Read("cosyvoice-tts-delivery.md")
	if err != nil {
		return "", err
	}
	_, body, ok := strings.Cut(string(raw), "```json")
	if !ok {
		return "", errors.New("CosyVoice表达母版缺少JSON配置")
	}
	body, _, ok = strings.Cut(body, "```")
	if !ok {
		return "", errors.New("CosyVoice表达母版格式错误")
	}
	profiles := map[string]string{}
	if err := json.Unmarshal([]byte(body), &profiles); err != nil {
		return "", err
	}
	base := profiles[strings.TrimSpace(item.SelectedInterrupt)]
	if base == "" {
		base = profiles["default"]
	}
	if base == "" {
		return "", errors.New("CosyVoice表达母版缺少默认指令")
	}
	if mission, ok := w.missionSnapshot(item); ok {
		state := mission.HumanStyle.State.HostState
		for _, spec := range []struct {
			key   string
			words []string
		}{
			{"state_ill", []string{"咳嗽", "感冒", "嗓子", "喉咙", "不舒服", "生病"}},
			{"state_tired", []string{"累", "疲惫", "困", "没精神"}},
			{"state_happy", []string{"兴奋", "开心", "状态好", "精神好"}},
			{"state_anxious", []string{"紧张", "焦虑"}},
		} {
			found := false
			for _, word := range spec.words {
				if strings.Contains(state, word) {
					found = true
					break
				}
			}
			if found {
				if profiles[spec.key] == "" {
					return "", errors.New("CosyVoice表达母版缺少主播状态指令")
				}
				base += "；" + profiles[spec.key]
				break
			}
		}
		if ttsgateway.CharacterCount(base) > 100 {
			return "", errors.New("CosyVoice表达母版超过100字符")
		}
		reaction := mission.HumanStyle.Reaction
		if reaction.Enabled && (strings.EqualFold(reaction.Channel, "TTS_STYLE") || strings.EqualFold(reaction.Channel, "MIXED")) {
			// Only the supplemental delivery instruction is bounded; never trim
			// the spoken answer. Keep the final directive in logs/cache identical
			// to what is actually sent to the provider.
			budget := 99 - ttsgateway.CharacterCount(base)
			brief := ""
			for _, r := range strings.TrimSpace(reaction.Instruction) {
				cost := ttsgateway.CharacterCount(string(r))
				if cost > budget {
					break
				}
				brief += string(r)
				budget -= cost
			}
			if brief != "" {
				base += "；" + brief
			}
		}
	}
	if ttsgateway.CharacterCount(base) > 100 {
		return "", errors.New("CosyVoice表达母版超过100字符")
	}
	return base, nil
}
