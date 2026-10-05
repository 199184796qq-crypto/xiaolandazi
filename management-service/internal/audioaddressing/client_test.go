package audioaddressing

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func syntheticTone(milliseconds int) []byte {
	count := milliseconds * 16
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(36+count*2))
	out.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(16000), uint32(32000), uint16(2), uint16(16)} {
		_ = binary.Write(&out, binary.LittleEndian, v)
	}
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, uint32(count*2))
	for i := 0; i < count; i++ {
		_ = binary.Write(&out, binary.LittleEndian, int16(math.Sin(2*math.Pi*440*float64(i)/16000)*7000))
	}
	return out.Bytes()
}

func TestAcousticObservationConservative(t *testing.T) {
	base := acousticObservation{Addressing: "female", Confidence: 0.95, SpeakerCount: 1, SpeechMS: 2000, IsSpeech: true}
	if got := conservativeObservation(base, 2500); got.Addressing != "female" {
		t.Fatal(got)
	}
	for _, change := range []func(*acousticObservation){func(o *acousticObservation) { o.Noisy = true }, func(o *acousticObservation) { o.SpeakerCount = 2 }, func(o *acousticObservation) { o.SpeakerCount = 0 }, func(o *acousticObservation) { o.SpeechMS = 700 }, func(o *acousticObservation) { o.SpeechMS = 3000 }, func(o *acousticObservation) { o.Confidence = .7 }, func(o *acousticObservation) { o.Confidence = 1.1 }, func(o *acousticObservation) { o.IsSpeech = false }, func(o *acousticObservation) { o.Addressing = "identity-001" }, func(o *acousticObservation) { o.Addressing = "child"; o.Confidence = .9 }} {
		o := base
		change(&o)
		if conservativeObservation(o, 2500).Addressing != "neutral" {
			t.Fatal("unsafe inference", o)
		}
	}
}

func TestAudioAddressingUsesAudioNotTranscript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct {
				Content []struct {
					Type       string `json:"type"`
					Text       string `json:"text"`
					InputAudio struct {
						Data   string `json:"data"`
						Format string `json:"format"`
					} `json:"input_audio"`
				} `json:"content"`
			} `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid payload")
		}
		if !request.Stream || request.Model != "qwen3.8-omni-flash" || len(request.Messages) != 1 || len(request.Messages[0].Content) != 2 {
			t.Error("wrong acoustic request")
		}
		if request.Messages[0].Content[1].Text != acousticPrompt || request.Messages[0].Content[0].Type != "input_audio" || request.Messages[0].Content[0].InputAudio.Format != "wav" || !strings.HasPrefix(request.Messages[0].Content[0].InputAudio.Data, "data:;base64,") {
			t.Error("transcript/personal data or audio contract mismatch")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": `{"addressing":"male","confidence":0.95,"speaker_count":1,"speech_ms":2000,"noisy":false,"is_speech":true}`}}}})
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", body)
	}))
	defer server.Close()
	c := &Client{baseURL: server.URL, model: "qwen3.8-omni-flash", apiKey: "test-key", ffmpeg: "does-not-exist", http: server.Client()}
	result, err := c.Analyze(context.Background(), syntheticTone(2500), "wav")
	if err != nil || result.Addressing != "male" {
		t.Fatal(result, err)
	}
	short, err := c.Analyze(context.Background(), syntheticTone(500), "wav")
	if err != nil || short.Addressing != "neutral" {
		t.Fatal("short sound not neutral", short, err)
	}
}

func TestAudioAddressingDisabledMalformedAndTimeout(t *testing.T) {
	if result, err := (&Client{}).Analyze(context.Background(), nil, "wav"); err != ErrDisabled || result.Addressing != "neutral" {
		t.Fatal(result, err)
	}
	for _, raw := range []string{"not-json", `data: {"error":{"message":"provider error"}}`} {
		if _, err := readModelText(strings.NewReader(raw)); err == nil {
			t.Fatal("bad stream accepted")
		}
	}
	for _, raw := range []string{"not-json", `{}`, `{"addressing":"female","confidence":0.99,"speaker_count":1,"speech_ms":2000,"is_speech":true}`, `{"addressing":"female","confidence":0.99,"speaker_count":1,"speech_ms":2000,"noisy":null,"is_speech":true}`} {
		if _, err := decodeObservation(raw); err == nil {
			t.Fatal("incomplete observation accepted")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer server.Close()
	c := &Client{baseURL: server.URL, model: "qwen3.8-omni-flash", apiKey: "test-key", http: server.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	result, err := c.Analyze(ctx, syntheticTone(2500), "wav")
	if err == nil || result.Addressing != "neutral" || time.Since(started) > time.Second {
		t.Fatal("timeout blocked command", result, err, time.Since(started))
	}
}

func TestPrepareWAVIsBounded(t *testing.T) {
	if _, duration, err := prepareWAV(context.Background(), syntheticTone(2500), "wav", ""); err != nil || duration != 2500 {
		t.Fatal(duration, err)
	}
	for _, data := range [][]byte{nil, []byte("wav"), syntheticTone(9000)} {
		if _, _, err := prepareWAV(context.Background(), data, "wav", "not-an-executable"); err == nil {
			t.Fatal("bad or unbounded sound accepted")
		}
	}
}

// Explicit opt-in, exactly one request using an in-memory generated tone. No
// customer recording, ASR, OSS upload, or real device/business row is touched.
func TestLiveSyntheticAcousticModel(t *testing.T) {
	if os.Getenv("DEVICE_ADDRESSING_LIVE_TEST") != "1" {
		t.Skip("single synthetic live check requires explicit opt-in")
	}
	c := NewFromEnv()
	if !c.Enabled() {
		t.Fatal("synthetic live check is not configured")
	}
	started := time.Now()
	result, err := c.Analyze(context.Background(), syntheticTone(2500), "wav")
	if err != nil {
		t.Fatalf("synthetic acoustic model %s failed: %v", c.Model(), err)
	}
	if result.Addressing != "neutral" {
		t.Fatal("non-speech tone must remain neutral")
	}
	t.Logf("synthetic_only=true model=%s addressing=%s elapsed_ms=%d", c.Model(), result.Addressing, time.Since(started).Milliseconds())
}
