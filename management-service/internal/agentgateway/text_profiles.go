package agentgateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/interactionpolicy"
	"strings"
)

// Only the transport layer knows the actual model. Business code supplies a task
// and an operator-owned role/profile reference, never model-specific instructions.
type TextRoleResolver interface {
	ResolveTextRole(context.Context, string) (string, error)
}
type textProfileRegistry struct {
	Version  int    `json:"version"`
	Default  string `json:"default"`
	Profiles []struct {
		ID          string   `json:"id"`
		Prefixes    []string `json:"model_prefixes"`
		Prompt      string   `json:"prompt"`
		Temperature *float64 `json:"temperature,omitempty"`
	} `json:"profiles"`
}

func textGenerationStage(stage string) bool {
	switch stage {
	case "mainline_text", "speech_generation":
		return true
	}
	return false
}
func canonicalTextModel(model string) string {
	return strings.NewReplacer("-", "", "_", "", " ", "", ".", "").Replace(strings.ToLower(strings.TrimSpace(model)))
}
func TextPromptContract(model string) (string, string, string, error) {
	raw, err := interactionpolicy.Read("text-model-profiles.json")
	if err != nil {
		return "", "", "", err
	}
	var registry textProfileRegistry
	if err = json.Unmarshal(raw, &registry); err != nil || registry.Version != 1 {
		return "", "", "", fmt.Errorf("文字模型配置格式无效")
	}
	selected := registry.Default
	name := canonicalTextModel(model)
	best := 0
	for _, p := range registry.Profiles {
		for _, prefix := range p.Prefixes {
			key := canonicalTextModel(prefix)
			if len(key) > best && strings.HasPrefix(name, key) {
				selected = p.ID
				best = len(key)
			}
		}
	}
	path := ""
	for _, p := range registry.Profiles {
		if p.ID == selected {
			path = p.Prompt
		}
	}
	if path == "" || strings.Contains(path, "..") || strings.ContainsAny(path, "/\\") {
		return "", "", "", fmt.Errorf("文字模型提示词配置无效")
	}
	common, err := interactionpolicy.Read("text-generation-foundation.md")
	if err != nil {
		return "", "", "", err
	}
	adaptation, err := interactionpolicy.Read(path)
	if err != nil {
		return "", "", "", err
	}
	body := string(common) + "\n\n" + string(adaptation)
	nicknames, err := interactionpolicy.Read("interaction-nickname-addressing.md")
	if err != nil {
		return "", "", "", err
	}
	final, err := interactionpolicy.Read("text-generation-final-boundary.md")
	if err != nil {
		return "", "", "", err
	}
	return selected, fmt.Sprintf("%x", sha256.Sum256([]byte(string(raw)+body+string(nicknames)+string(final)))), body, nil
}
func PrepareTextRequest(p SpeechModel, r Request) (Request, error) {
	if !textGenerationStage(r.Stage) || r.TextPromptSHA != "" {
		return r, nil
	}
	id, hash, body, err := TextPromptContract(p.Model)
	if err != nil {
		return r, err
	}
	if r.Temperature == nil {
		raw, err := interactionpolicy.Read("text-model-profiles.json")
		if err != nil {
			return r, err
		}
		var registry textProfileRegistry
		if err = json.Unmarshal(raw, &registry); err != nil {
			return r, err
		}
		for _, profile := range registry.Profiles {
			if profile.ID == id && profile.Temperature != nil {
				value := *profile.Temperature
				r.Temperature = &value
				break
			}
		}
	}
	r.Messages = append([]Message{{Role: "system", Content: body}}, r.Messages...)
	if r.Stage == "speech_generation" {
		nicknames, err := interactionpolicy.Read("interaction-nickname-addressing.md")
		if err != nil {
			return r, err
		}
		// Keep name selection at the end of the actual interaction task. This is
		// guidance in the same request, not a filter or another model/audit call.
		for i := len(r.Messages) - 1; i >= 0; i-- {
			if r.Messages[i].Role == "user" {
				r.Messages[i].Content += "\n\n" + string(nicknames)
				break
			}
		}
	}
	final, err := interactionpolicy.Read("text-generation-final-boundary.md")
	if err != nil {
		return r, err
	}
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "user" {
			r.Messages[i].Content += "\n\n" + string(final)
			break
		}
	}
	r.TextPromptID, r.TextPromptSHA = id, hash
	r.EnableThinking = false
	return r, nil
}

// Include model, operator prompt and current mother revision in disposable text
// cache keys, so switching providers cannot continue serving an old model's prose.
func (g *Gateway) TextGenerationIdentity(ctx context.Context) (string, error) {
	if g.speechModels == nil {
		return g.defaultProvider + ":" + g.defaultModel, nil
	}
	id := ""
	if roles, ok := g.speechModels.(TextRoleResolver); ok {
		var err error
		id, err = roles.ResolveTextRole(ctx, "speech_generation")
		if err != nil {
			return "", err
		}
	}
	p, err := g.speechModels.ResolveSpeechModel(ctx, id)
	if err != nil {
		return "", err
	}
	if p == nil {
		return g.defaultProvider + ":" + g.defaultModel, nil
	}
	_, hash, _, err := TextPromptContract(p.Model)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(p.ID+":"+p.Model+":"+p.BaseURL+":"+p.SystemPrompt+":"+hash))), nil
}
