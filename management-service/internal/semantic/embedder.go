package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultBaseURL   = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	defaultModel     = "qwen3.7-text-embedding-flash"
	defaultBatchSize = 20
	defaultTimeoutMS = 1200
	defaultDimension = 1024
)

var ErrDisabled = errors.New("semantic embedding is disabled")

type Embedder interface {
	Enabled() bool
	Model() string
	Embed(context.Context, []string) ([][]float32, error)
}

type Config struct {
	Enabled    bool
	BaseURL    string
	APIKey     string
	Model      string
	BatchSize  int
	Timeout    time.Duration
	Dimensions int
}

type Stats struct {
	Requests       uint64
	Texts          uint64
	Failures       uint64
	InputTokens    uint64
	LatencyNanos   uint64
	LatencyBuckets [7]uint64
}

type Client struct {
	enabled        bool
	baseURL        string
	apiKey         string
	model          string
	batchSize      int
	dimensions     int
	http           *http.Client
	requests       atomic.Uint64
	texts          atomic.Uint64
	failures       atomic.Uint64
	inputTokens    atomic.Uint64
	latencyNanos   atomic.Uint64
	latencyBuckets [7]atomic.Uint64
}

func ConfigFromEnv() Config {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("DASHSCOPE_EMBEDDING_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("DASHSCOPE_BASE_URL")), "/")
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	model := strings.TrimSpace(os.Getenv("DASHSCOPE_EMBEDDING_MODEL"))
	if model == "" {
		model = defaultModel
	}
	batchSize := envPositiveInt("SEMANTIC_BATCH_SIZE", defaultBatchSize)
	if batchSize > defaultBatchSize {
		batchSize = defaultBatchSize
	}
	timeoutMS := envPositiveInt("SEMANTIC_TIMEOUT_MS", defaultTimeoutMS)
	return Config{
		Enabled:    envBool("SEMANTIC_ENABLED", false),
		BaseURL:    baseURL,
		APIKey:     strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY")),
		Model:      model,
		BatchSize:  batchSize,
		Timeout:    time.Duration(timeoutMS) * time.Millisecond,
		Dimensions: defaultDimension,
	}
}

func NewFromEnv() *Client {
	return New(ConfigFromEnv())
}

func New(config Config) *Client {
	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	if batchSize > defaultBatchSize {
		batchSize = defaultBatchSize
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultTimeoutMS * time.Millisecond
	}
	dimensions := config.Dimensions
	if dimensions <= 0 {
		dimensions = defaultDimension
	}
	model := strings.TrimSpace(config.Model)
	if model == "" {
		model = defaultModel
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		enabled:    config.Enabled,
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(config.APIKey),
		model:      model,
		batchSize:  batchSize,
		dimensions: dimensions,
		http:       &http.Client{Timeout: timeout},
	}
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

func (c *Client) Stats() Stats {
	if c == nil {
		return Stats{}
	}
	result := Stats{
		Requests:     c.requests.Load(),
		Texts:        c.texts.Load(),
		Failures:     c.failures.Load(),
		InputTokens:  c.inputTokens.Load(),
		LatencyNanos: c.latencyNanos.Load(),
	}
	for index := range result.LatencyBuckets {
		result.LatencyBuckets[index] = c.latencyBuckets[index].Load()
	}
	return result
}

func (c *Client) recordLatency(duration time.Duration) {
	if c == nil {
		return
	}
	ms := duration.Milliseconds()
	index := 6
	switch {
	case ms <= 50:
		index = 0
	case ms <= 100:
		index = 1
	case ms <= 250:
		index = 2
	case ms <= 500:
		index = 3
	case ms <= 1000:
		index = 4
	case ms <= 2000:
		index = 5
	}
	c.latencyBuckets[index].Add(1)
}

func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	if len(texts) == 0 {
		return nil, nil
	}
	result := make([][]float32, len(texts))
	for start := 0; start < len(texts); start += c.batchSize {
		end := start + c.batchSize
		if end > len(texts) {
			end = len(texts)
		}
		vectors, err := c.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		if len(vectors) != end-start {
			return nil, fmt.Errorf("embedding response size=%d want=%d", len(vectors), end-start)
		}
		copy(result[start:end], vectors)
	}
	return result, nil
}

type embeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	Dimensions     int      `json:"dimensions,omitempty"`
	EncodingFormat string   `json:"encoding_format"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
	Usage struct {
		PromptTokens int64 `json:"prompt_tokens"`
		TotalTokens  int64 `json:"total_tokens"`
	} `json:"usage"`
}

func (c *Client) embedBatch(ctx context.Context, texts []string) (vectors [][]float32, err error) {
	started := time.Now()
	c.requests.Add(1)
	c.texts.Add(uint64(len(texts)))
	defer func() {
		duration := time.Since(started)
		c.latencyNanos.Add(uint64(duration))
		c.recordLatency(duration)
		if err != nil {
			c.failures.Add(1)
		}
	}()
	payload, err := json.Marshal(embeddingRequest{Model: c.model, Input: texts, Dimensions: c.dimensions, EncodingFormat: "float"})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var decoded embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if decoded.Usage.PromptTokens > 0 {
		c.inputTokens.Add(uint64(decoded.Usage.PromptTokens))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := http.StatusText(resp.StatusCode)
		if decoded.Error != nil && strings.TrimSpace(decoded.Error.Message) != "" {
			message = strings.TrimSpace(decoded.Error.Message)
		}
		return nil, fmt.Errorf("embedding http %d: %s", resp.StatusCode, message)
	}
	if decoded.Error != nil && strings.TrimSpace(decoded.Error.Message) != "" {
		return nil, fmt.Errorf("embedding error: %s", strings.TrimSpace(decoded.Error.Message))
	}

	if len(decoded.Data) != len(texts) {
		return nil, fmt.Errorf("embedding response data=%d want=%d", len(decoded.Data), len(texts))
	}
	for _, item := range decoded.Data {
		if c.dimensions > 0 && len(item.Embedding) != c.dimensions {
			return nil, fmt.Errorf("embedding dimension=%d want=%d", len(item.Embedding), c.dimensions)
		}
	}

	// Some DashScope OpenAI-compatible responses currently repeat index=0 for
	// every item in a batched request. Prefer valid unique indexes when present;
	// otherwise preserve response order, which matches request order.
	vectors = make([][]float32, len(texts))
	seen := make([]bool, len(texts))
	indexesUsable := true
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(vectors) || seen[item.Index] {
			indexesUsable = false
			break
		}
		seen[item.Index] = true
	}
	if indexesUsable {
		for _, item := range decoded.Data {
			vectors[item.Index] = item.Embedding
		}
		return vectors, nil
	}
	for index, item := range decoded.Data {
		vectors[index] = item.Embedding
	}
	return vectors, nil
}

func CosineSimilarity(left, right []float32) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for index := range left {
		l := float64(left[index])
		r := float64(right[index])
		dot += l * r
		leftNorm += l * l
		rightNorm += r * r
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	score := dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
	if score > 1 {
		return 1
	}
	if score < -1 {
		return -1
	}
	return score
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
