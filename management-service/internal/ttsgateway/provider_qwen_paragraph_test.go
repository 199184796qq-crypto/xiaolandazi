package ttsgateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQwenParagraphKeepsNonStreamingNoSubtitleContractAndPaidFileAfterStop(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	wav := tencentPCMToWAV(bytes.Repeat([]byte{1, 2}, 24000))
	binary.LittleEndian.PutUint32(wav[24:], 24000)
	binary.LittleEndian.PutUint32(wav[28:], 48000)
	posts, gets := 0, 0
	client := &http.Client{Transport: longTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			gets++
			if r.Header.Get("Authorization") != "" {
				t.Error("key forwarded to media")
			}
			if r.Context().Err() != nil {
				t.Error("paid download cancelled by room stop")
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(wav))}, nil
		}
		posts++
		if r.Header.Get("X-DashScope-SSE") != "" {
			t.Error("unexpected SSE change")
		}
		var payload struct {
			Input map[string]any `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if _, ok := payload.Input["word_timestamp_enabled"]; ok {
			t.Error("subtitle requirement added")
		}
		if payload.Input["text"] != strings.Repeat("原文", 100) || payload.Input["format"] != "wav" {
			t.Error(payload.Input)
		}
		cancel() // Accepted upstream, then the live session is stopped.
		return longTestResponse(`{"request_id":"r1","output":{"audio":{"url":"https://media.invalid/paid.wav"}},"usage":{"input_tokens":200,"output_tokens":300}}`), nil
	})}
	p := NewQwenProvider(QwenConfig{APIKey: "fixture", TTSBaseURL: "https://tts.invalid/api/v1", Client: client})
	path := filepath.Join(t.TempDir(), "paid.wav")
	out, err := p.SynthesizeURL(ctx, SynthesizeRequest{Model: "qwen-audio-3.1-tts-flash", VoiceID: "voice", Text: strings.Repeat("原文", 100), Rate: 1, ParagraphPath: path})
	if err != nil || out.AudioFile != path || len(out.Subtitles) != 0 || posts != 1 || gets != 1 {
		t.Fatal(out, err, posts, gets)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, wav) {
		t.Fatal("paid file missing", err)
	}
}

func TestQwenParagraphCancellationBeforeSubmitDoesNotSend(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	sent := 0
	p := NewQwenProvider(QwenConfig{APIKey: "fixture", TTSBaseURL: "https://tts.invalid", Client: &http.Client{Transport: longTestTransport(func(*http.Request) (*http.Response, error) { sent++; return nil, errors.New("must not send") })}})
	_, err := p.SynthesizeURL(ctx, SynthesizeRequest{Model: "qwen-audio-3.1-tts-flash", VoiceID: "voice", Text: "原文", ParagraphPath: filepath.Join(t.TempDir(), "paid.wav")})
	if !DefinitelyNotSent(err) || sent != 0 {
		t.Fatal(err, sent)
	}
}

func TestQwenParagraphDownloadRetriesDoNotSubmitAgain(t *testing.T) {
	wav := tencentPCMToWAV(bytes.Repeat([]byte{1, 2}, 24000))
	posts, gets := 0, 0
	client := &http.Client{Transport: longTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			posts++
			return longTestResponse(`{"output":{"audio":{"url":"https://media.invalid/paid.wav"}}}`), nil
		}
		gets++
		if gets < 3 {
			return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(wav))}, nil
	})}
	p := NewQwenProvider(QwenConfig{APIKey: "fixture", TTSBaseURL: "https://tts.invalid", Client: client})
	_, err := p.SynthesizeURL(t.Context(), SynthesizeRequest{Model: "qwen-audio-3.1-tts-flash", VoiceID: "voice", Text: "原文。", Rate: 1, ParagraphPath: filepath.Join(t.TempDir(), "paid.wav")})
	if err != nil || posts != 1 || gets != 3 {
		t.Fatal("download retry repurchased audio", err, posts, gets)
	}
}

func TestVolcengineOnlyExplicitConcurrencyRejectionPermitsNewTask(t *testing.T) {
	for _, tc := range []struct {
		body  string
		retry bool
	}{
		{`{"message":"quota exceeded for types: concurrency"}`, true},
		{`{"message":"concurrency limit exceeded"}`, true},
		{`{"message":"insufficient account balance"}`, false},
		{`{"message":"permission denied"}`, false},
		{`quota exceeded for types: concurrency`, false},
	} {
		r := SynthesizeRequest{Model: VolcengineICL2, VoiceID: "S_fixture", Text: "测试。", Rate: 1, LongText: &LongTextJob{StatePath: filepath.Join(t.TempDir(), "job.json")}}
		posts := 0
		p := NewVolcengineProvider(VolcengineConfig{APIKey: "fixture", Client: &http.Client{Transport: longTestTransport(func(*http.Request) (*http.Response, error) {
			posts++
			return &http.Response{StatusCode: 429, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})}})
		_, err := p.SynthesizeURL(t.Context(), r)
		if IsConcurrencyLimit(err) != tc.retry || DefinitelyNotSent(err) != tc.retry || posts != 1 {
			t.Fatal(tc, err, posts)
		}
		_, statErr := os.Stat(r.LongText.StatePath)
		if errors.Is(statErr, os.ErrNotExist) != tc.retry {
			t.Fatal("wrong checkpoint retention", tc, statErr)
		}
	}
}
