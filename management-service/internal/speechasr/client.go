package speechasr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://dashscope.aliyuncs.com/api/v1"
	defaultModel   = "qwen-audio-3.1-asr-flash-filetrans"
)

type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

type Sentence struct {
	BeginTime int64  `json:"begin_time"`
	EndTime   int64  `json:"end_time"`
	Text      string `json:"text"`
	SpeakerID *int   `json:"speaker_id,omitempty"`
}

type Result struct {
	TaskID    string
	RawJSON   []byte
	Text      string
	Sentences []Sentence
}

func NewFromEnv() *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("DASHSCOPE_ASR_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	model := strings.TrimSpace(os.Getenv("DASHSCOPE_ASR_MODEL"))
	if model == "" {
		model = defaultModel
	}
	apiKey := strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(windowsUserEnv("DASHSCOPE_API_KEY"))
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) Configured() error {
	if c == nil {
		return errors.New("ASR 客户端未初始化")
	}
	if strings.TrimSpace(c.apiKey) == "" {
		return errors.New("DASHSCOPE_API_KEY 未配置")
	}
	if strings.TrimSpace(c.baseURL) == "" {
		return errors.New("ASR 接口地址未配置")
	}
	return nil
}

func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

func (c *Client) Transcribe(ctx context.Context, audioURL string) (Result, error) {
	if err := c.Configured(); err != nil {
		return Result{}, err
	}
	audioURL = strings.TrimSpace(audioURL)
	if audioURL == "" {
		return Result{}, errors.New("ASR 音频地址为空")
	}

	payload := map[string]any{
		"model": c.model,
		"input": map[string]any{
			"file_urls": []string{audioURL},
		},
		"parameters": map[string]any{
			"channel_id":     []int{0},
			"language_hints": []string{"zh"},
			"keep_dialect":   false,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/services/audio/asr/transcription", bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DashScope-Async", "enable")
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, err
	}
	submitBody, readErr := readResponse(resp, 2<<20)
	if readErr != nil {
		return Result{}, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("ASR 提交失败 HTTP %d: %s", resp.StatusCode, compactError(submitBody))
	}
	var submitted struct {
		Output struct {
			TaskID     string `json:"task_id"`
			TaskStatus string `json:"task_status"`
		} `json:"output"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(submitBody, &submitted); err != nil {
		return Result{}, fmt.Errorf("解析 ASR 提交结果: %w", err)
	}
	taskID := strings.TrimSpace(submitted.Output.TaskID)
	if taskID == "" {
		if strings.TrimSpace(submitted.Message) != "" {
			return Result{}, errors.New(strings.TrimSpace(submitted.Message))
		}
		return Result{}, errors.New("ASR 未返回 task_id")
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-ticker.C:
			result, done, err := c.poll(ctx, taskID)
			if err != nil {
				return Result{}, err
			}
			if done {
				return result, nil
			}
		}
	}
}

func (c *Client) poll(ctx context.Context, taskID string) (Result, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/tasks/"+taskID, nil)
	if err != nil {
		return Result{}, false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, false, err
	}
	body, readErr := readResponse(resp, 4<<20)
	if readErr != nil {
		return Result{}, false, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, false, fmt.Errorf("查询 ASR 任务失败 HTTP %d: %s", resp.StatusCode, compactError(body))
	}
	var state struct {
		Output struct {
			TaskStatus string `json:"task_status"`
			Results    []struct {
				SubtaskStatus    string `json:"subtask_status"`
				TranscriptionURL string `json:"transcription_url"`
				Code             string `json:"code"`
				Message          string `json:"message"`
			} `json:"results"`
		} `json:"output"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &state); err != nil {
		return Result{}, false, fmt.Errorf("解析 ASR 任务状态: %w", err)
	}
	switch strings.ToUpper(strings.TrimSpace(state.Output.TaskStatus)) {
	case "PENDING", "RUNNING":
		return Result{}, false, nil
	case "SUCCEEDED":
		for _, item := range state.Output.Results {
			if strings.EqualFold(item.SubtaskStatus, "SUCCEEDED") && strings.TrimSpace(item.TranscriptionURL) != "" {
				result, err := c.downloadResult(ctx, taskID, item.TranscriptionURL)
				return result, true, err
			}
		}
		for _, item := range state.Output.Results {
			if strings.TrimSpace(item.Message) != "" {
				return Result{}, true, fmt.Errorf("ASR 子任务失败: %s %s", item.Code, item.Message)
			}
		}
		return Result{}, true, errors.New("ASR 完成但没有可用转写结果")
	case "FAILED", "CANCELED", "UNKNOWN":
		message := strings.TrimSpace(state.Message)
		if message == "" && len(state.Output.Results) > 0 {
			message = strings.TrimSpace(state.Output.Results[0].Message)
		}
		if message == "" {
			message = "ASR 任务失败"
		}
		return Result{}, true, errors.New(message)
	default:
		return Result{}, false, nil
	}
}

func (c *Client) downloadResult(ctx context.Context, taskID, resultURL string) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resultURL, nil)
	if err != nil {
		return Result{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, err
	}
	body, readErr := readResponse(resp, 64<<20)
	if readErr != nil {
		return Result{}, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("下载 ASR 结果失败 HTTP %d", resp.StatusCode)
	}
	var decoded struct {
		Transcripts []struct {
			Text      string     `json:"text"`
			Sentences []Sentence `json:"sentences"`
		} `json:"transcripts"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return Result{}, fmt.Errorf("解析 ASR 转写结果: %w", err)
	}
	var paragraphs []string
	var sentences []Sentence
	for _, transcript := range decoded.Transcripts {
		if text := strings.TrimSpace(transcript.Text); text != "" {
			paragraphs = append(paragraphs, text)
		}
		sentences = append(sentences, transcript.Sentences...)
	}
	text := strings.TrimSpace(strings.Join(paragraphs, "\n"))
	if text == "" {
		return Result{}, errors.New("ASR 转写结果为空")
	}
	return Result{TaskID: taskID, RawJSON: body, Text: text, Sentences: sentences}, nil
}

func readResponse(resp *http.Response, max int64) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, max))
}

func compactError(body []byte) string {
	value := strings.TrimSpace(string(body))
	if len(value) > 800 {
		value = value[:800]
	}
	return value
}
