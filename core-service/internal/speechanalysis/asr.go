package speechanalysis

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
	defaultASRBaseURL = "https://dashscope.aliyuncs.com/api/v1"
	defaultASRModel   = "qwen-audio-3.1-asr-flash-filetrans"
)

type asrClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

type asrSentence struct {
	BeginTime int64  `json:"begin_time"`
	EndTime   int64  `json:"end_time"`
	Text      string `json:"text"`
	SpeakerID *int   `json:"speaker_id,omitempty"`
}

type asrResult struct {
	TaskID    string
	Text      string
	Sentences []asrSentence
}

func newASRClient() *asrClient {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("DASHSCOPE_ASR_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = defaultASRBaseURL
	}
	model := strings.TrimSpace(os.Getenv("DASHSCOPE_ASR_MODEL"))
	if model == "" {
		model = defaultASRModel
	}
	return &asrClient{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY")),
		model:   model,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *asrClient) configured() error {
	if c == nil || strings.TrimSpace(c.apiKey) == "" {
		return errors.New("speech recognition credential is not configured")
	}
	return nil
}

func (c *asrClient) submit(ctx context.Context, audioURL string) (string, error) {
	if err := c.configured(); err != nil {
		return "", err
	}
	audioURL = strings.TrimSpace(audioURL)
	if audioURL == "" {
		return "", errors.New("audio url is empty")
	}
	payload := map[string]any{
		"model": c.model,
		"input": map[string]any{"file_urls": []string{audioURL}},
		"parameters": map[string]any{
			"channel_id":     []int{0},
			"language_hints": []string{"zh"},
			"keep_dialect":   false,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/services/audio/asr/transcription", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DashScope-Async", "enable")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	body, err := readHTTPBody(resp, 2<<20)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("speech recognition submit http %d: %s", resp.StatusCode, compactBody(body))
	}
	var submitted struct {
		Output struct {
			TaskID string `json:"task_id"`
		} `json:"output"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &submitted); err != nil {
		return "", err
	}
	taskID := strings.TrimSpace(submitted.Output.TaskID)
	if taskID == "" {
		return "", errors.New(strings.TrimSpace(submitted.Message))
	}
	return taskID, nil
}

func (c *asrClient) wait(ctx context.Context, taskID string) (asrResult, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return asrResult{}, ctx.Err()
		case <-ticker.C:
			result, done, err := c.poll(ctx, taskID)
			if err != nil {
				return asrResult{}, err
			}
			if done {
				return result, nil
			}
		}
	}
}

func (c *asrClient) poll(ctx context.Context, taskID string) (asrResult, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/tasks/"+taskID, nil)
	if err != nil {
		return asrResult{}, false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return asrResult{}, false, err
	}
	body, err := readHTTPBody(resp, 4<<20)
	if err != nil {
		return asrResult{}, false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return asrResult{}, false, fmt.Errorf("speech recognition poll http %d: %s", resp.StatusCode, compactBody(body))
	}
	var state struct {
		Output struct {
			TaskStatus string `json:"task_status"`
			Results    []struct {
				SubtaskStatus    string `json:"subtask_status"`
				TranscriptionURL string `json:"transcription_url"`
				Message          string `json:"message"`
			} `json:"results"`
		} `json:"output"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &state); err != nil {
		return asrResult{}, false, err
	}
	switch strings.ToUpper(strings.TrimSpace(state.Output.TaskStatus)) {
	case "PENDING", "RUNNING":
		return asrResult{}, false, nil
	case "SUCCEEDED":
		for _, item := range state.Output.Results {
			if strings.EqualFold(item.SubtaskStatus, "SUCCEEDED") && strings.TrimSpace(item.TranscriptionURL) != "" {
				result, err := c.download(ctx, taskID, item.TranscriptionURL)
				return result, true, err
			}
		}
		return asrResult{}, true, errors.New("speech recognition finished without transcript")
	case "FAILED", "CANCELED", "UNKNOWN":
		message := strings.TrimSpace(state.Message)
		if message == "" && len(state.Output.Results) > 0 {
			message = strings.TrimSpace(state.Output.Results[0].Message)
		}
		if message == "" {
			message = "speech recognition failed"
		}
		return asrResult{}, true, errors.New(message)
	default:
		return asrResult{}, false, nil
	}
}

func (c *asrClient) download(ctx context.Context, taskID, resultURL string) (asrResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resultURL, nil)
	if err != nil {
		return asrResult{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return asrResult{}, err
	}
	body, err := readHTTPBody(resp, 64<<20)
	if err != nil {
		return asrResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return asrResult{}, fmt.Errorf("speech recognition result http %d", resp.StatusCode)
	}
	var decoded struct {
		Transcripts []struct {
			Text      string        `json:"text"`
			Sentences []asrSentence `json:"sentences"`
		} `json:"transcripts"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return asrResult{}, err
	}
	var paragraphs []string
	var sentences []asrSentence
	for _, transcript := range decoded.Transcripts {
		if text := strings.TrimSpace(transcript.Text); text != "" {
			paragraphs = append(paragraphs, text)
		}
		sentences = append(sentences, transcript.Sentences...)
	}
	text := strings.TrimSpace(strings.Join(paragraphs, "\n"))
	if text == "" {
		return asrResult{}, errors.New("speech recognition result is empty")
	}
	return asrResult{TaskID: taskID, Text: text, Sentences: sentences}, nil
}

func readHTTPBody(resp *http.Response, max int64) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, max))
}

func compactBody(body []byte) string {
	value := strings.TrimSpace(string(body))
	if len(value) > 800 {
		value = value[:800]
	}
	return value
}
