package agentgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type QwenConfig struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

type qwenProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

type qwenChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		TotalTokens      int64 `json:"total_tokens"`
	} `json:"usage"`
}

type qwenModelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func QwenConfigFromEnv() QwenConfig {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("DASHSCOPE_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	apiKey := strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(windowsUserEnv("DASHSCOPE_API_KEY"))
	}
	return QwenConfig{BaseURL: baseURL, APIKey: apiKey}
}

func NewQwenProvider(config QwenConfig) Provider {
	client := config.Client
	if client == nil {
		client = &http.Client{}
	}
	return &qwenProvider{
		baseURL: strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"),
		apiKey:  strings.TrimSpace(config.APIKey),
		client:  client,
	}
}

func (p *qwenProvider) Name() string {
	return ProviderQwen
}

func (p *qwenProvider) ListModels(ctx context.Context) ([]ModelDescriptor, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("DASHSCOPE_API_KEY not configured")
	}
	if p.baseURL == "" {
		return nil, fmt.Errorf("DashScope base URL is empty")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qwen provider models http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded qwenModelListResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	items := make([]ModelDescriptor, 0, len(decoded.Data))
	seen := map[string]struct{}{}
	for _, item := range decoded.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		items = append(items, ModelDescriptor{Provider: ProviderQwen, ID: id})
	}
	return items, nil
}

func (p *qwenProvider) Complete(ctx context.Context, request Request) (Response, error) {
	if p.apiKey == "" {
		return Response{}, fmt.Errorf("DASHSCOPE_API_KEY not configured")
	}
	if p.baseURL == "" {
		return Response{}, fmt.Errorf("DashScope base URL is empty")
	}
	if strings.TrimSpace(request.Model) == "" {
		request.Model = DefaultQwenModel
	}
	messages := make([]map[string]any, 0, len(request.Messages))
	for _, item := range request.Messages {
		role := strings.TrimSpace(item.Role)
		content := strings.TrimSpace(item.Content)
		images := make([]string, 0, len(item.ImageURLs))
		for _, imageURL := range item.ImageURLs {
			imageURL = strings.TrimSpace(imageURL)
			if imageURL != "" {
				images = append(images, imageURL)
			}
		}
		if role == "" || (content == "" && len(images) == 0) {
			continue
		}
		if len(images) == 0 {
			messages = append(messages, map[string]any{"role": role, "content": content})
			continue
		}
		parts := make([]map[string]any, 0, len(images)+1)
		if content != "" {
			parts = append(parts, map[string]any{"type": "text", "text": content})
		}
		for _, imageURL := range images {
			parts = append(parts, map[string]any{
				"type":      "image_url",
				"image_url": map[string]string{"url": imageURL},
			})
		}
		messages = append(messages, map[string]any{"role": role, "content": parts})
	}
	if len(messages) == 0 {
		return Response{}, fmt.Errorf("agent messages are empty")
	}
	payload := map[string]any{
		"model":           request.Model,
		"messages":        messages,
		"enable_thinking": request.EnableThinking,
		"stream":          false,
		"max_tokens":      request.MaxTokens,
	}
	if request.ResponseFormat == ResponseJSON {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}
	requestCtx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()

	send := func(currentPayload map[string]any) (int, []byte, int64, error) {
		body, err := json.Marshal(currentPayload)
		if err != nil {
			return 0, nil, 0, err
		}
		httpRequest, err := http.NewRequestWithContext(
			requestCtx,
			http.MethodPost,
			p.baseURL+"/chat/completions",
			bytes.NewReader(body),
		)
		if err != nil {
			return 0, nil, 0, err
		}
		httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
		httpRequest.Header.Set("Content-Type", "application/json")

		started := time.Now()
		resp, err := p.client.Do(httpRequest)
		latencyMS := time.Since(started).Milliseconds()
		if err != nil {
			return 0, nil, latencyMS, err
		}
		defer resp.Body.Close()
		responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return resp.StatusCode, nil, latencyMS, err
		}
		return resp.StatusCode, responseBody, latencyMS, nil
	}

	statusCode, responseBody, latencyMS, err := send(payload)
	if err != nil {
		return Response{}, err
	}
	if statusCode != http.StatusOK &&
		request.ResponseFormat == ResponseJSON &&
		(statusCode == http.StatusBadRequest || statusCode == http.StatusUnprocessableEntity) {
		// Some OpenAI-compatible models do not implement response_format even
		// though they can still follow a strict JSON-only prompt. Retry once
		// without the optional transport hint instead of hard-coding model IDs.
		fallbackPayload := make(map[string]any, len(payload)-1)
		for key, value := range payload {
			if key == "response_format" {
				continue
			}
			fallbackPayload[key] = value
		}
		fallbackStatus, fallbackBody, fallbackLatencyMS, fallbackErr := send(fallbackPayload)
		latencyMS += fallbackLatencyMS
		if fallbackErr != nil {
			return Response{}, fallbackErr
		}
		statusCode = fallbackStatus
		responseBody = fallbackBody
	}
	if statusCode != http.StatusOK {
		return Response{}, fmt.Errorf("qwen provider http %d: %s", statusCode, strings.TrimSpace(string(responseBody)))
	}

	var decoded qwenChatResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return Response{}, err
	}
	if len(decoded.Choices) == 0 {
		return Response{}, fmt.Errorf("qwen provider returned no choices")
	}
	text := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if text == "" {
		return Response{}, fmt.Errorf("qwen provider returned empty reply")
	}
	return Response{
		Text:         text,
		Provider:     ProviderQwen,
		Model:        request.Model,
		LatencyMS:    latencyMS,
		InputTokens:  decoded.Usage.PromptTokens,
		OutputTokens: decoded.Usage.CompletionTokens,
		TotalTokens:  decoded.Usage.TotalTokens,
	}, nil
}
