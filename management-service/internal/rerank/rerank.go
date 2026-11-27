// Package rerank re-orders already recalled candidates with DashScope's
// cross-encoder rerank model (qwen3.7-text-rerank).
//
// It is deliberately a second-stage ranker: the caller keeps its own recall
// gate (cosine threshold, scope filters, active window) and this package only
// answers "given the query and these documents, which order is most relevant".
// Scores are comparable only inside one request, never across requests, so the
// caller must not use them as absolute thresholds.
package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultBaseURL is the public DashScope API host. A workspace-specific
	// Model Studio host is also valid through DASHSCOPE_RERANK_BASE_URL:
	// https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/api/v1
	defaultBaseURL      = "https://dashscope.aliyuncs.com/api/v1"
	defaultModel        = "qwen3.7-text-rerank"
	defaultTimeoutMS    = 1500
	defaultMaxDocuments = 64
	rerankPath          = "/services/rerank/text-rerank/text-rerank"
)

// ErrDisabled means rerank is switched off or the DashScope API key is missing.
// Callers treat it like any other failure: keep the original ordering.
var ErrDisabled = errors.New("rerank is disabled")

// Config is the environment-backed configuration. The API key is shared with
// the existing DashScope integrations (Qwen speech models, ASR, embeddings).
type Config struct {
	Enabled      bool
	BaseURL      string
	APIKey       string
	Model        string
	Timeout      time.Duration
	MaxDocuments int
}

// ConfigFromEnv reads the rerank knobs. Rerank is on by default once a
// DASHSCOPE_API_KEY is available; set DASHSCOPE_RERANK_ENABLED=false to keep
// the pure cosine ordering. `DASHSCOPE_RERANK_BASE_URL` is intentionally
// separate from DASHSCOPE_BASE_URL: the latter points at the OpenAI-compatible
// /compatible-mode/v1 host, which does not serve this endpoint.
func ConfigFromEnv() Config {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("DASHSCOPE_RERANK_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	model := strings.TrimSpace(os.Getenv("DASHSCOPE_RERANK_MODEL"))
	if model == "" {
		model = defaultModel
	}
	timeoutMS := envPositiveInt("DASHSCOPE_RERANK_TIMEOUT_MS", defaultTimeoutMS)
	maxDocuments := envPositiveInt("DASHSCOPE_RERANK_MAX_DOCUMENTS", defaultMaxDocuments)
	apiKey := strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(windowsUserEnv("DASHSCOPE_API_KEY"))
	}
	return Config{
		Enabled:      envBool("DASHSCOPE_RERANK_ENABLED", true),
		BaseURL:      baseURL,
		APIKey:       apiKey,
		Model:        model,
		Timeout:      time.Duration(timeoutMS) * time.Millisecond,
		MaxDocuments: maxDocuments,
	}
}

type Client struct {
	enabled      bool
	baseURL      string
	apiKey       string
	model        string
	maxDocuments int
	http         *http.Client
}

func New(config Config) *Client {
	model := strings.TrimSpace(config.Model)
	if model == "" {
		model = defaultModel
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultTimeoutMS * time.Millisecond
	}
	maxDocuments := config.MaxDocuments
	if maxDocuments <= 0 {
		maxDocuments = defaultMaxDocuments
	}
	return &Client{
		enabled:      config.Enabled,
		baseURL:      baseURL,
		apiKey:       strings.TrimSpace(config.APIKey),
		model:        model,
		maxDocuments: maxDocuments,
		http:         &http.Client{Timeout: timeout},
	}
}

func NewFromEnv() *Client {
	return New(ConfigFromEnv())
}

func (c *Client) Enabled() bool {
	return c != nil && c.enabled && c.apiKey != "" && c.baseURL != "" && c.model != ""
}

func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

// Rerank returns one relevance score per document, aligned with documents by
// position. Order inside the returned slice follows the request order; callers
// sort themselves so the original candidate structs stay attached.
func (c *Client) Rerank(ctx context.Context, query string, documents []string) ([]float64, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	query = strings.TrimSpace(query)
	if query == "" || len(documents) == 0 {
		return nil, nil
	}
	if len(documents) > c.maxDocuments {
		return nil, fmt.Errorf("rerank documents=%d exceeds limit=%d", len(documents), c.maxDocuments)
	}
	normalized := make([]string, len(documents))
	for index, document := range documents {
		document = strings.TrimSpace(document)
		if document == "" {
			// DashScope rejects empty candidates; a space keeps the position so
			// scores stay aligned with the caller's slice.
			document = " "
		}
		normalized[index] = document
	}
	payload, err := json.Marshal(rerankRequest{
		Model: c.model,
		Input: rerankInput{Query: query, Documents: normalized},
		Parameters: rerankParameters{
			ReturnDocuments: false,
		},
	})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+rerankPath, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	var decoded rerankResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode rerank response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := http.StatusText(response.StatusCode)
		if detail := strings.TrimSpace(decoded.Message); detail != "" {
			message = detail
		}
		return nil, fmt.Errorf("rerank http %d: %s", response.StatusCode, message)
	}
	if code := strings.TrimSpace(decoded.Code); code != "" {
		message := strings.TrimSpace(decoded.Message)
		if message == "" {
			message = code
		}
		return nil, fmt.Errorf("rerank error %s: %s", code, message)
	}
	// The request omits top_n, so every submitted document must come back. A
	// short answer would silently drop candidates, so treat it as a failure and
	// let the caller keep its cosine order.
	if len(decoded.Output.Results) != len(normalized) {
		return nil, fmt.Errorf("rerank results=%d want=%d", len(decoded.Output.Results), len(normalized))
	}
	scores := make([]float64, len(normalized))
	seen := make([]bool, len(normalized))
	for _, result := range decoded.Output.Results {
		if result.Index < 0 || result.Index >= len(scores) || seen[result.Index] {
			return nil, fmt.Errorf("rerank result index=%d invalid for documents=%d", result.Index, len(scores))
		}
		seen[result.Index] = true
		scores[result.Index] = clampScore(result.RelevanceScore)
	}
	return scores, nil
}

type rerankRequest struct {
	Model      string           `json:"model"`
	Input      rerankInput      `json:"input"`
	Parameters rerankParameters `json:"parameters"`
}

type rerankInput struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type rerankParameters struct {
	ReturnDocuments bool `json:"return_documents"`
}

type rerankResponse struct {
	Output struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
	} `json:"output"`
	Usage struct {
		TotalTokens int64 `json:"total_tokens"`
	} `json:"usage"`
	RequestID string `json:"request_id"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

func clampScore(score float64) float64 {
	switch {
	case score < 0:
		return 0
	case score > 1:
		return 1
	default:
		return score
	}
}

func envBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func envPositiveInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
