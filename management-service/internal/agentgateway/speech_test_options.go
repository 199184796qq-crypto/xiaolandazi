package agentgateway

import (
	"errors"
	"math"
	"strings"
)

// Test controls are request-local. Nil controls preserve production transport defaults.
type SpeechTestCapabilities struct {
	Thinking                        string  `json:"thinking"`
	ThinkingHint                    string  `json:"thinking_hint"`
	TemperatureSupported            bool    `json:"temperature_supported"`
	TemperatureMax                  float64 `json:"temperature_max"`
	TemperatureDisabledWhenThinking bool    `json:"temperature_disabled_when_thinking"`
}

func SpeechTestOptions(p SpeechModel) SpeechTestCapabilities {
	c := SpeechTestCapabilities{Thinking: "unavailable", ThinkingHint: "当前接口暂不支持切换思考模式", TemperatureSupported: true, TemperatureMax: 2}
	model := strings.ToLower(strings.TrimSpace(p.Model))
	switch p.Protocol {
	case "openai_chat", "openai_responses":
		if dashScopeQwenEndpoint(p.BaseURL, model) && (strings.HasPrefix(model, "qwen3") || model == "qwen-plus" || model == "qwen-flash" || model == "qwen-turbo" || strings.HasPrefix(model, "qwen-plus-")) {
			c.Thinking = "toggle"
			if strings.Contains(model, "-instruct") {
				c.Thinking = "unavailable"
			}
			if strings.Contains(model, "-thinking") || strings.HasPrefix(model, "qwen3.8-2.4t") || model == "qwen3.7-max-preview" || model == "qwen3.7-max-2026-05-17" {
				c.Thinking = "always"
			}
		} else if p.Protocol == "openai_chat" && officialDeepSeekEndpoint(p.BaseURL, model) {
			c.Thinking = "toggle"
			c.TemperatureDisabledWhenThinking = true
			if strings.HasPrefix(model, "deepseek-reasoner") || strings.HasPrefix(model, "deepseek-r1") {
				c.Thinking = "always"
			}
		}
		if strings.HasPrefix(model, "gpt-5") || strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4") {
			c.TemperatureSupported = false
		}
	case "gemini":
		model = strings.TrimPrefix(model, "models/")
		if strings.HasPrefix(model, "gemini-2.5-flash") {
			c.Thinking = "toggle"
		}
		if strings.HasPrefix(model, "gemini-2.5-pro") || strings.HasPrefix(model, "gemini-3") {
			c.Thinking = "always"
		}
	case "anthropic":
		c.TemperatureMax = 1
		c.TemperatureDisabledWhenThinking = true
		if strings.HasPrefix(model, "claude-") && (strings.Contains(model, "-4") || strings.Contains(model, "-3-7")) {
			c.Thinking = "toggle"
			if !adaptiveSpeechThinking(model) && p.MaxTokens <= 1024 {
				c.Thinking = "unavailable"
				c.ThinkingHint = "开启思考模式需将最大输出 Token 调到 1025 以上"
			}
		}
		// Later Claude models require model-specific adaptive settings and reject temperature.
		if strings.Contains(model, "-4-7") || strings.Contains(model, "-4-8") || strings.HasPrefix(model, "claude-sonnet-5") || strings.HasPrefix(model, "claude-opus-5") || strings.HasPrefix(model, "claude-haiku-5") || strings.Contains(model, "mythos") || strings.Contains(model, "fable") {
			c.Thinking, c.TemperatureSupported = "unavailable", false
			c.ThinkingHint = "当前模型使用服务商默认思考设置"
		}
	default:
		c.TemperatureSupported = false
	}
	if c.Thinking == "toggle" {
		c.ThinkingHint = "只用于本次文字测试"
	}
	if c.Thinking == "always" {
		c.ThinkingHint = "此模型始终开启思考模式"
	}
	return c
}

func adaptiveSpeechThinking(model string) bool {
	return strings.Contains(model, "-4-6") || strings.Contains(model, "-4.6")
}

func ValidateSpeechTestOptions(p SpeechModel, r Request) error {
	c := SpeechTestOptions(p)
	if r.ThinkingOverride != nil {
		if c.Thinking == "unavailable" {
			return errors.New(c.ThinkingHint)
		}
		if c.Thinking == "always" && !*r.ThinkingOverride {
			return errors.New("此模型的思考模式不能关闭")
		}
	}
	if r.Temperature != nil {
		if !c.TemperatureSupported {
			return errors.New("此模型使用默认温度，不能设置温度")
		}
		if math.IsNaN(*r.Temperature) || math.IsInf(*r.Temperature, 0) || *r.Temperature < 0 || *r.Temperature > c.TemperatureMax {
			return errors.New("模型温度超出当前协议允许的范围")
		}
		if r.ThinkingOverride != nil && *r.ThinkingOverride && c.TemperatureDisabledWhenThinking {
			return errors.New("此模型开启思考模式时不能同时设置温度")
		}
	}
	return nil
}

func applySpeechTestOptions(payload map[string]any, p SpeechModel, r Request) {
	if r.Temperature != nil {
		if p.Protocol == "gemini" {
			payload["generationConfig"].(map[string]any)["temperature"] = *r.Temperature
		} else {
			payload["temperature"] = *r.Temperature
		}
	}
	if r.ThinkingOverride == nil {
		return
	}
	enabled := *r.ThinkingOverride
	model := strings.ToLower(strings.TrimSpace(p.Model))
	switch p.Protocol {
	case "openai_chat", "openai_responses":
		if dashScopeQwenEndpoint(p.BaseURL, model) {
			delete(payload, "reasoning_effort")
			payload["enable_thinking"] = enabled
			if p.Protocol == "openai_chat" && strings.HasPrefix(model, "qwen3.8-omni") {
				delete(payload, "enable_thinking")
				effort := "none"
				if enabled {
					effort = "high"
				}
				payload["reasoning_effort"] = effort
			}
		} else if officialDeepSeekEndpoint(p.BaseURL, model) {
			mode := "disabled"
			if enabled {
				mode = "enabled"
			}
			payload["thinking"] = map[string]any{"type": mode}
		}
	case "gemini":
		if SpeechTestOptions(p).Thinking == "toggle" {
			budget := 0
			if enabled {
				budget = -1
			}
			payload["generationConfig"].(map[string]any)["thinkingConfig"] = map[string]any{"thinkingBudget": budget}
		}
	case "anthropic":
		thinking := map[string]any{"type": "disabled"}
		if enabled {
			if adaptiveSpeechThinking(model) {
				thinking["type"] = "adaptive"
			} else {
				thinking["type"], thinking["budget_tokens"] = "enabled", min(4096, r.MaxTokens-1)
			}
		}
		payload["thinking"] = thinking
	}
}
