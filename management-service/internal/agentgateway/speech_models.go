package agentgateway

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Speech models never replace the agent/analysis provider or its credentials.
type SpeechModel struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Protocol       string `json:"protocol"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	Enabled        bool   `json:"enabled"`
	TimeoutMS      int    `json:"timeout_ms"`
	MaxTokens      int    `json:"max_tokens"`
	ChatTokenField string `json:"chat_token_field,omitempty"`
	SystemPrompt   string `json:"system_prompt"`
	HasAPIKey      bool   `json:"has_api_key"`
	APIKey         string `json:"-"`
}

type SpeechModels struct {
	Profiles  []SpeechModel `json:"profiles"`
	DefaultID string        `json:"default_id"`
	Revision  uint64        `json:"revision"`
}

type SpeechModelInput struct {
	SpeechModel
	NewAPIKey string `json:"api_key"`
}

type SpeechModelsInput struct {
	Profiles  []SpeechModelInput `json:"profiles"`
	DefaultID string             `json:"default_id"`
	Revision  uint64             `json:"revision"`
}

type SpeechModelResolver interface {
	ResolveSpeechModel(context.Context, string) (*SpeechModel, error)
}

func (g *Gateway) WithSpeechModels(resolver SpeechModelResolver) *Gateway {
	g.speechModels = resolver
	return g
}

var speechModelID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func ValidSpeechProfileID(id string) bool { return speechModelID.MatchString(id) }

func ValidateSpeechModel(p SpeechModel) error {
	if !speechModelID.MatchString(p.ID) {
		return errors.New("模型配置 ID 无效")
	}
	if strings.TrimSpace(p.Name) == "" || utf8.RuneCountInString(p.Name) > 80 {
		return errors.New("模型名称须为 1—80 字")
	}
	if err := ValidateSpeechConnection(p); err != nil {
		return err
	}
	if strings.TrimSpace(p.Model) == "" || len(p.Model) > 160 || strings.ContainsAny(p.Model, "?#\\\r\n") {
		return errors.New("请填写有效模型 ID")
	}
	if p.TimeoutMS < 1000 || p.TimeoutMS > 180000 {
		return errors.New("超时须为 1000—180000 毫秒")
	}
	if p.MaxTokens < 64 || p.MaxTokens > 32000 {
		return errors.New("最大输出 Token 须为 64—32000")
	}
	if p.ChatTokenField != "" && p.ChatTokenField != "max_tokens" && p.ChatTokenField != "max_completion_tokens" {
		return errors.New("输出 Token 参数无效")
	}
	if utf8.RuneCountInString(p.SystemPrompt) > 20000 {
		return errors.New("基础提示词不能超过 20000 字")
	}
	return nil
}

// Listing needs no model selected in advance.
func ValidateSpeechConnection(p SpeechModel) error {
	switch p.Protocol {
	case "openai_chat", "openai_responses", "gemini", "anthropic":
	default:
		return errors.New("不支持的 API 协议")
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(p.BaseURL) > 2048 {
		return errors.New("API 地址必须是 HTTPS 地址，不得包含密钥、查询参数或账号")
	}
	return nil
}

// The encryption key is server-owned, never stored in the model configuration.
func speechCipher(secret string) (cipher.AEAD, error) {
	if len(secret) < 32 {
		return nil, errors.New("请先在服务器配置至少 32 字符的 MODEL_CONFIG_ENCRYPTION_KEY")
	}
	key := sha256.Sum256([]byte("anchor-model-credentials/v1:" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func SealSpeechKey(secret, id, key string) ([]byte, error) {
	gcm, err := speechCipher(secret)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(key), []byte(id)), nil
}

func OpenSpeechKey(secret, id string, sealed []byte) (string, error) {
	gcm, err := speechCipher(secret)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("模型密钥损坏")
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(id))
	if err != nil {
		return "", errors.New("模型密钥无法解密，请检查服务器加密密钥")
	}
	return string(plain), nil
}

func (g *Gateway) completeSpeechModel(ctx context.Context, request Request) (Response, bool, error) {
	if g.speechModels == nil {
		return Response{}, false, nil
	}
	id := ""
	if strings.HasPrefix(request.Provider, "anchor:") {
		id = strings.TrimPrefix(request.Provider, "anchor:")
	} else if request.Stage != "speech_generation" || request.Provider != "" || request.Model != "" {
		return Response{}, false, nil
	}
	p, err := g.speechModels.ResolveSpeechModel(ctx, id)
	if err != nil {
		return Response{}, true, errors.New("读取主播模型配置失败")
	}
	if p == nil {
		return Response{}, false, nil
	}
	if !p.Enabled {
		return Response{}, true, errors.New("主播模型已停用")
	}
	request.Model = p.Model
	limit := time.Duration(p.TimeoutMS) * time.Millisecond
	if request.Timeout <= 0 || request.Timeout > limit {
		request.Timeout = limit
	}
	if request.MaxTokens <= 0 || request.MaxTokens > p.MaxTokens {
		request.MaxTokens = p.MaxTokens
	}
	request.Messages = append([]Message(nil), request.Messages...)
	if strings.TrimSpace(p.SystemPrompt) != "" {
		request.Messages = append([]Message{{Role: "system", Content: p.SystemPrompt}}, request.Messages...)
	}
	result, err := CompleteSpeechEndpoint(ctx, *p, request)
	result.Provider = "anchor:" + p.ID
	result.Model = p.Model
	return result, true, err
}

func publicSpeechModels(c SpeechModels) SpeechModels {
	c.Profiles = append([]SpeechModel(nil), c.Profiles...)
	for i := range c.Profiles {
		c.Profiles[i].APIKey = ""
	}
	return c
}

func ValidateSpeechModels(input SpeechModelsInput) error {
	if len(input.Profiles) > 30 {
		return errors.New("最多保存 30 个模型配置")
	}
	seen := map[string]bool{}
	selected := input.DefaultID == ""
	for _, p := range input.Profiles {
		if err := ValidateSpeechModel(p.SpeechModel); err != nil {
			return fmt.Errorf("%s：%w", p.Name, err)
		}
		if seen[p.ID] {
			return errors.New("模型 ID 重复")
		}
		seen[p.ID] = true
		if len(p.NewAPIKey) > 4096 {
			return errors.New("API 密钥过长")
		}
		if p.ID == input.DefaultID {
			selected = p.Enabled
		}
	}
	if !selected {
		return errors.New("默认模型必须存在且启用")
	}
	return nil
}

// Ensure secrets cannot accidentally be returned by marshaling runtime profiles.
func (c SpeechModels) PublicJSON() ([]byte, error) { return json.Marshal(publicSpeechModels(c)) }
