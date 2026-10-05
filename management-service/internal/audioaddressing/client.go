// Package audioaddressing provides disposable acoustic greeting cues, never
// speaker identity, voiceprints, or a guess based on transcript semantics.
package audioaddressing

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
const DefaultModel = "qwen3.8-omni-flash"
const MaxLatency = 3 * time.Second

var ErrDisabled = errors.New("acoustic addressing is disabled")

type Client struct {
	baseURL, model, apiKey, ffmpeg string
	http                           *http.Client
}
type Result struct {
	Addressing string
	Confidence float64
}
type acousticObservation struct {
	Addressing   string  `json:"addressing"`
	Confidence   float64 `json:"confidence"`
	SpeakerCount int     `json:"speaker_count"`
	SpeechMS     int     `json:"speech_ms"`
	Noisy        bool    `json:"noisy"`
	IsSpeech     bool    `json:"is_speech"`
}

func NewFromEnv() *Client {
	base := strings.TrimSpace(os.Getenv("DEVICE_ADDRESSING_BASE_URL"))
	if base == "" {
		base = strings.TrimSpace(os.Getenv("DASHSCOPE_BASE_URL"))
	}
	if base == "" {
		base = defaultBaseURL
	}
	ffmpeg := strings.TrimSpace(os.Getenv("DEVICE_ADDRESSING_FFMPEG"))
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	return &Client{baseURL: strings.TrimRight(base, "/"), model: strings.TrimSpace(os.Getenv("DEVICE_ADDRESSING_MODEL")), apiKey: strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY")), ffmpeg: ffmpeg, http: &http.Client{Timeout: MaxLatency}}
}

func (c *Client) Enabled() bool { return c != nil && c.model != "" && c.apiKey != "" }
func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

// The model only hears audio and a fixed acoustic prompt. No ASR transcript,
// customer name, prior speaker history, or customer gender is included.
const acousticPrompt = `只根据这段录音的声学听感，为礼貌称呼选择音色标签，不判断或确认人的真实身份、性别、年龄，不做声纹识别。忽略说话内容和任何自报身份，只听音色。仅清晰单人、至少1.5秒有效人声且高把握时输出female（成年偏女性音色）、male（成年偏男性音色）或child（明显儿童音色），否则neutral。变声、儿童/成人不确定、多说话、重噪声、音乐、无有效人声都neutral。只输出JSON：{"addressing":"neutral","confidence":0.0,"speaker_count":0,"speech_ms":0,"noisy":false,"is_speech":false}。confidence为你的判断把握而非真实概率，不要输出其它文字。`

func (c *Client) Analyze(parent context.Context, data []byte, format string) (Result, error) {
	neutral := Result{Addressing: "neutral"}
	if !c.Enabled() {
		return neutral, ErrDisabled
	}
	if c.model != "qwen3.8-omni-flash" && c.model != "qwen3.5-omni-flash" && c.model != "qwen3-omni-flash" && c.model != "qwen-omni-turbo-latest" {
		return neutral, errors.New("unsupported acoustic audio model")
	}
	parsed, err := url.Parse(c.baseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return neutral, errors.New("invalid acoustic model endpoint")
	}
	ctx, cancel := context.WithTimeout(parent, MaxLatency)
	defer cancel()
	wav, duration, err := prepareWAV(ctx, data, format, c.ffmpeg)
	if err != nil {
		return neutral, err
	}
	if duration < 1500 {
		return neutral, nil
	}
	payload := map[string]any{"model": c.model, "stream": true, "modalities": []string{"text"}, "max_tokens": 160, "messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_audio", "input_audio": map[string]any{"data": "data:;base64," + base64.StdEncoding.EncodeToString(wav), "format": "wav"}}, map[string]any{"type": "text", "text": acousticPrompt}}}}}
	if c.model == "qwen3.8-omni-flash" {
		payload["reasoning_effort"] = "none"
	}
	if c.model == "qwen3-omni-flash" {
		payload["enable_thinking"] = false
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return neutral, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return neutral, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return neutral, errors.New("acoustic model request failed or timed out")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return neutral, fmt.Errorf("acoustic model HTTP %d", response.StatusCode)
	}
	text, err := readModelText(response.Body)
	if err != nil {
		return neutral, err
	}
	observation, err := decodeObservation(text)
	if err != nil {
		return neutral, errors.New("invalid acoustic model result")
	}
	return conservativeObservation(observation, duration), nil
}

func decodeObservation(text string) (acousticObservation, error) {
	var fields map[string]json.RawMessage
	var out acousticObservation
	if err := json.Unmarshal([]byte(text), &fields); err != nil {
		return out, err
	}
	// Missing boolean fields must not accidentally become false ("not noisy").
	for _, name := range []string{"addressing", "confidence", "speaker_count", "speech_ms", "noisy", "is_speech"} {
		value, exists := fields[name]
		if !exists || string(value) == "null" {
			return out, errors.New("incomplete acoustic observation")
		}
	}
	err := json.Unmarshal([]byte(text), &out)
	return out, err
}

func conservativeObservation(value acousticObservation, duration int) Result {
	neutral := Result{Addressing: "neutral"}
	if !value.IsSpeech || value.Noisy || value.SpeakerCount != 1 || value.SpeechMS < 1500 || value.SpeechMS > duration+250 || value.Confidence < 0.88 || value.Confidence > 1 || math.IsNaN(value.Confidence) {
		return neutral
	}
	if value.Addressing != "female" && value.Addressing != "male" && value.Addressing != "child" {
		return neutral
	}
	if value.Addressing == "child" && value.Confidence < 0.95 {
		return neutral
	}
	return Result{Addressing: value.Addressing, Confidence: value.Confidence}
}

func readModelText(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, 64<<10))
	scanner.Buffer(make([]byte, 1024), 32<<10)
	var text strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if line == "[DONE]" {
			break
		}
		var item struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(line), &item) != nil {
			return "", errors.New("invalid acoustic model stream")
		}
		if len(item.Error) > 0 && string(item.Error) != "null" {
			return "", errors.New("acoustic model stream error")
		}
		if len(item.Choices) > 0 {
			text.WriteString(item.Choices[0].Delta.Content)
		}
		if text.Len() > 4096 {
			return "", errors.New("acoustic model result too long")
		}
	}
	if scanner.Err() != nil {
		return "", errors.New("acoustic model stream timed out")
	}
	result := strings.TrimSpace(text.String())
	if result == "" {
		return "", errors.New("empty acoustic model result")
	}
	return result, nil
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 512<<10 {
		return 0, errors.New("decoded audio too large")
	}
	return b.Buffer.Write(p)
}

func prepareWAV(ctx context.Context, data []byte, format, ffmpeg string) ([]byte, int, error) {
	if len(data) == 0 || len(data) > 2<<20 || (format != "wav" && format != "ogg") {
		return nil, 0, errors.New("unsupported addressing audio")
	}
	wav := data
	if _, _, err := pcm16WAV(data); err != nil {
		var output boundedOutput
		command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", format, "-i", "pipe:0", "-vn", "-ac", "1", "-ar", "16000", "-t", "8", "-c:a", "pcm_s16le", "-f", "wav", "pipe:1")
		command.Stdin = bytes.NewReader(data)
		command.Stdout = &output
		command.Stderr = io.Discard
		if command.Run() != nil {
			return nil, 0, errors.New("acoustic audio conversion failed")
		}
		wav = output.Bytes()
	}
	pcm, duration, err := pcm16WAV(wav)
	if err != nil {
		return nil, 0, err
	}
	if duration > 8250 {
		return nil, 0, errors.New("acoustic audio too long")
	}
	var power float64
	for i := 0; i+1 < len(pcm); i += 2 {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[i:i+2]))) / 32768
		power += v * v
	}
	if len(pcm) < 2 || math.Sqrt(power/float64(len(pcm)/2)) < 0.008 {
		return wav, 0, nil
	}
	return wav, duration, nil
}

func pcm16WAV(data []byte) ([]byte, int, error) {
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, errors.New("invalid PCM audio")
	}
	validFormat := false
	for pos := 12; pos+8 <= len(data); {
		chunk := string(data[pos : pos+4])
		size := int64(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		start := pos + 8
		available := int64(len(data) - start)
		if size > available {
			if chunk != "data" {
				return nil, 0, errors.New("invalid PCM chunk")
			}
			size = available
		}
		end := start + int(size)
		if chunk == "fmt " {
			if size < 16 {
				return nil, 0, errors.New("invalid PCM format")
			}
			validFormat = binary.LittleEndian.Uint16(data[start:start+2]) == 1 && binary.LittleEndian.Uint16(data[start+2:start+4]) == 1 && binary.LittleEndian.Uint32(data[start+4:start+8]) == 16000 && binary.LittleEndian.Uint16(data[start+14:start+16]) == 16
		}
		if chunk == "data" {
			if !validFormat {
				return nil, 0, errors.New("PCM must be 16k mono int16")
			}
			pcm := data[start:end]
			return pcm, len(pcm) * 1000 / 32000, nil
		}
		pos = end + int(size%2)
	}
	return nil, 0, errors.New("PCM audio has no data")
}
