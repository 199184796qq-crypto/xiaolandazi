package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"livecompanion/management/internal/ttsgateway"
)

func TestQwenFlashScriptSplitsAtPunctuationWithin600(t *testing.T) {
	sentence := strings.Repeat("字", 479) + "。"
	clause := strings.Repeat("字", 519) + "，"
	for _, tc := range []struct {
		name, text string
		want       []int
	}{
		{"at_limit", strings.Repeat("字", 600), []int{600}},
		{"sentence", sentence + clause, []int{480, 520}},
		{"comma", clause + strings.Repeat("尾", 100), []int{520, 100}},
		{"decimal_price", clause + strings.Repeat("尾", 77) + "9.9元" + strings.Repeat("字", 20), []int{520, 101}},
		{"no_punctuation", strings.Repeat("字", 601), []int{600, 1}},
		{"unicode", strings.Repeat("字A🌿 ", 160), []int{600, 40}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parts := splitQwenFlashParagraph(tc.text)
			var lengths []int
			for _, part := range parts {
				lengths = append(lengths, utf8.RuneCountInString(part))
			}
			if !slices.Equal(lengths, tc.want) || strings.Join(parts, "") != tc.text {
				t.Fatalf("parts lengths = %v, want %v with identical text", lengths, tc.want)
			}
		})
	}
}

func TestQwenFlashScriptPaysOnlyForMissingPartsAndProducesOneTrack(t *testing.T) {
	var text strings.Builder
	for i := 1; i <= 8; i++ {
		text.WriteString(strconv.Itoa(i) + strings.Repeat("字", 498) + "。")
	}
	unit := volcengineScriptUnit{Key: "qwen-test-paragraph", Dir: t.TempDir(), Text: text.String()}
	var mu sync.Mutex
	calls := map[int]int{}
	otherPartsReady := make(chan struct{})
	var fake *httptest.Server
	fake = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/audio/") {
			index, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/audio/"))
			w.Write(scriptFirstPCMToWAV([]int16{int16(index), int16(index)}))
			return
		}
		var request struct {
			Model string
			Input struct {
				Text, Voice string
			}
		}
		if r.Method != http.MethodPost || r.URL.Path != "/services/aigc/multimodal-generation/generation" ||
			json.NewDecoder(r.Body).Decode(&request) != nil || request.Model != "qwen3-tts-flash" || request.Input.Voice != "Chelsie" {
			t.Error("unexpected provider request", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if n := utf8.RuneCountInString(request.Input.Text); n != 500 {
			t.Errorf("provider text has %d characters, want 500", n)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		index, _ := strconv.Atoi(request.Input.Text[:1])
		mu.Lock()
		calls[index]++
		attempt := calls[index]
		mu.Unlock()
		if index == 4 && attempt == 1 {
			// Part 4 uses the same worker after part 1 has been saved.
			close(otherPartsReady)
		}
		if index == 2 && attempt == 1 {
			// Other workers may complete and retain paid audio before a
			// failed request cancels the part group.
			select {
			case <-otherPartsReady:
			case <-r.Context().Done():
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintf(w, `{"output":{"audio":{"url":"%s/audio/%d"}}}`, fake.URL, index)
	}))
	defer fake.Close()
	provider := ttsgateway.NewQwenProvider(ttsgateway.QwenConfig{APIKey: "fixture", TTSBaseURL: fake.URL, Client: fake.Client()})
	if _, err := provider.SynthesizeURL(t.Context(), ttsgateway.SynthesizeRequest{
		Model: "qwen3-tts-flash", VoiceID: "Chelsie", Text: strings.Repeat("字", 601),
	}); !ttsgateway.DefinitelyNotSent(err) {
		t.Fatalf("oversized request was not blocked before sending: %v", err)
	}
	run := func() (ttsgateway.SynthesizeResponse, error) {
		return prepareScriptFirstPaid(t.Context(), unit, func(ctx context.Context, path string) (ttsgateway.SynthesizeResponse, error) {
			return synthesizeQwenFlashParagraph(ctx, unit, path, func(c context.Context, part volcengineScriptUnit, file string) (ttsgateway.SynthesizeResponse, error) {
				return provider.SynthesizeURL(c, ttsgateway.SynthesizeRequest{
					Model: "qwen3-tts-flash", VoiceID: "Chelsie", Text: part.Text, ParagraphPath: file,
				})
			})
		})
	}
	if _, err := run(); err == nil || !strings.Contains(err.Error(), "第 2/8 段") {
		t.Fatalf("want the failed part, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(unit.Dir, "paragraph.wav")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial paragraph became available: %v", err)
	}
	out, err := run()
	if err != nil {
		t.Fatal(err)
	}
	samples, rate, err := readWAVPCM16Mono(out.AudioFile)
	if err != nil || rate != 24000 || !slices.Equal(samples, []int16{1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8}) || out.VoiceID != "Chelsie" {
		t.Fatalf("merged audio has %+v, %d Hz, voice %s, error %v", samples, rate, out.VoiceID, err)
	}
	if cached, err := run(); err != nil || cached.AudioFile != out.AudioFile {
		t.Fatalf("complete paragraph was not reused: %+v %v", cached, err)
	}
	mu.Lock()
	defer mu.Unlock()
	for i := 1; i <= 8; i++ {
		want := 1
		if i == 2 {
			want = 2
		}
		if calls[i] != want {
			t.Errorf("part %d was sent %d times, want %d", i, calls[i], want)
		}
	}
}
