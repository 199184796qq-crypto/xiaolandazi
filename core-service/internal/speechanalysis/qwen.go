package speechanalysis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type analysisClient struct {
	http *http.Client
}

type analysisMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type analysisProviderSettings struct {
	BaseURL        string
	APIKey         string
	EnableThinking bool
}

func newAnalysisClient() *analysisClient {
	return &analysisClient{http: &http.Client{Timeout: 4 * time.Minute}}
}

func analysisProviderConfig(provider string) (analysisProviderSettings, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "dashscope", "aliyun", "aliyun-dashscope":
		baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("DASHSCOPE_BASE_URL")), "/")
		if baseURL == "" {
			baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
		}
		return analysisProviderSettings{
			BaseURL:        baseURL,
			APIKey:         strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY")),
			EnableThinking: true,
		}, nil
	case "openai-compatible", "openai_compatible":
		baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("ANALYSIS_OPENAI_BASE_URL")), "/")
		if baseURL == "" {
			return analysisProviderSettings{}, errors.New("analysis OpenAI-compatible base URL is not configured")
		}
		return analysisProviderSettings{
			BaseURL: baseURL,
			APIKey:  strings.TrimSpace(os.Getenv("ANALYSIS_OPENAI_API_KEY")),
		}, nil
	default:
		return analysisProviderSettings{}, fmt.Errorf("unsupported analysis provider: %s", strings.TrimSpace(provider))
	}
}

func (c *analysisClient) configured(provider string) error {
	if c == nil {
		return errors.New("analysis client is not configured")
	}
	settings, err := analysisProviderConfig(provider)
	if err != nil {
		return err
	}
	if strings.TrimSpace(settings.APIKey) == "" {
		return errors.New("analysis credential is not configured")
	}
	return nil
}

func (c *analysisClient) complete(
	ctx context.Context,
	provider string,
	model string,
	messages []analysisMessage,
	maxTokens int,
	timeout time.Duration,
) (string, error) {
	settings, err := analysisProviderConfig(provider)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(settings.APIKey) == "" {
		return "", errors.New("analysis credential is not configured")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return "", errors.New("analysis model is empty")
	}
	if maxTokens <= 0 {
		maxTokens = 2200
	}
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	payload := map[string]any{
		"model":      model,
		"messages":   messages,
		"stream":     false,
		"max_tokens": maxTokens,
	}
	if settings.EnableThinking {
		payload["enable_thinking"] = false
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt*3) * time.Second
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(wait):
			}
		}
		requestCtx, cancel := context.WithTimeout(ctx, timeout)
		text, retry, err := c.completeOnce(requestCtx, settings, payload)
		cancel()
		if err == nil {
			return text, nil
		}
		lastErr = err
		if !retry {
			break
		}
	}
	return "", lastErr
}

func (c *analysisClient) completeOnce(
	ctx context.Context,
	settings analysisProviderSettings,
	payload map[string]any,
) (string, bool, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, settings.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+settings.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", isRetryableTransport(err), err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", true, err
	}
	if resp.StatusCode != http.StatusOK {
		retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return "", retry, fmt.Errorf("analysis provider http %d: %s", resp.StatusCode, compactBody(responseBody))
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return "", false, err
	}
	if len(decoded.Choices) == 0 {
		return "", false, errors.New("analysis provider returned no choices")
	}
	text := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if text == "" {
		return "", false, errors.New("analysis provider returned empty content")
	}
	return text, false, nil
}

func isRetryableTransport(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
