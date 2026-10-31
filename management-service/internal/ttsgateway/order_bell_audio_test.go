package ttsgateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOrderBellAudioJoinsPCMOnceAndServesOnePrivateWAV(t *testing.T) {
	bell := bytes.Repeat([]byte{0x11, 0x01}, orderAudioRate/10)
	speech := bytes.Repeat([]byte{0x22, 0x02}, orderAudioRate/5)
	speechWAV := orderPCMToWAV(speech)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected provider call: %s", r.Method)
		}
		_, _ = w.Write(speechWAV)
	}))
	defer server.Close()
	loads := 0
	c := orderBellComposer{load: func() ([]byte, error) { loads++; return orderPCMToWAV(bell), nil }, client: server.Client(), publish: cacheTencentWAV, ffmpeg: "not-needed"}
	for i := 0; i < 3; i++ {
		out, err := c.compose(t.Context(), server.URL+"/existing-speech.wav")
		if err != nil {
			t.Fatal(err)
		}
		defer ReleaseTemporaryAudio(out.AudioURL)
		if out.BellDurationMS != 100 || out.GapDurationMS != 100 || out.DurationMS != 400 {
			t.Fatal(out)
		}
		u, _ := url.Parse(out.AudioURL)
		r := httptest.NewRequest(http.MethodGet, out.AudioURL, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.SetPathValue("audioID", strings.TrimPrefix(u.Path, "/internal/v1/tts-audio/"))
		w := httptest.NewRecorder()
		ServeTencentAudio(w, r)
		pcm, ok := orderPCMFromWAV(w.Body.Bytes())
		gap := make([]byte, orderAudioRate*2/10)
		want := append(append(append([]byte{}, bell...), gap...), speech...)
		if w.Code != 200 || !ok || !bytes.Equal(pcm, want) || !bytes.Equal(speechWAV, orderPCMToWAV(speech)) {
			t.Fatal("one ordered WAV must contain intact bell, silence and speech", w.Code, ok, len(pcm))
		}
		// Existing Core probing/range reads must remain usable.
		r.Header.Set("Range", "bytes=44-47")
		w = httptest.NewRecorder()
		ServeTencentAudio(w, r)
		if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), bell[:4]) {
			t.Fatal("range read lost bell", w.Code)
		}
	}
	if loads != 1 {
		t.Fatal("fixed bell was reloaded for each order", loads)
	}
}

func TestOrderBellAudioMissingAndCancellationNeverPublishes(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		c := orderBellComposer{load: func() ([]byte, error) { return nil, os.ErrNotExist }, publish: func(context.Context, []byte) (string, error) {
			t.Fatal("published without usable speech")
			return "", nil
		}}
		ctx, cancel := context.WithCancel(t.Context())
		if canceled {
			cancel()
		}
		_, err := c.compose(ctx, "http://never-contact.invalid/audio.wav")
		cancel()
		if (!canceled && !errors.Is(err, ErrOrderBellMissing)) || (canceled && !errors.Is(err, context.Canceled)) {
			t.Fatal(err)
		}
	}
}

func TestOrderBellAudioRetriesMissingAssetWithoutPoisoningCache(t *testing.T) {
	loads := 0
	c := orderBellComposer{load: func() ([]byte, error) {
		loads++
		if loads == 1 {
			return nil, os.ErrNotExist
		}
		return orderPCMToWAV([]byte{1, 0, 2, 0}), nil
	}}
	if _, err := c.bell(t.Context()); !errors.Is(err, ErrOrderBellMissing) {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := c.bell(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if loads != 2 {
		t.Fatal(loads)
	}
}

func TestOrderBellAudioRejectsFailedOrCanceledSpeechDownload(t *testing.T) {
	for _, kind := range []string{"http_error", "invalid_audio", "canceled_download", "publish_error"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "http_error":
					w.WriteHeader(http.StatusServiceUnavailable)
				case "invalid_audio":
					_, _ = w.Write([]byte("not audio"))
				case "canceled_download":
					<-r.Context().Done()
				default:
					_, _ = w.Write(orderPCMToWAV([]byte{3, 0, 4, 0}))
				}
			}))
			defer server.Close()
			published := 0
			c := orderBellComposer{load: func() ([]byte, error) { return orderPCMToWAV([]byte{1, 0, 2, 0}), nil }, client: server.Client(), ffmpeg: "fixture-ffmpeg-unavailable", publish: func(context.Context, []byte) (string, error) { published++; return "", errors.New("full") }}
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()
			_, err := c.compose(ctx, server.URL+"/audio?signature=never-print")
			if err == nil || strings.Contains(err.Error(), "signature") || (kind != "publish_error" && published != 0) {
				t.Fatal(err, published)
			}
		})
	}
}

func TestOrderBellAssetUsesExplicitInstallationAndRejectsOversizedFiles(t *testing.T) {
	root := t.TempDir()
	prompt := filepath.Join(root, "configs", "prompts", "anchor-style")
	audioDir := filepath.Join(root, "configs", "audio")
	if err := os.MkdirAll(audioDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANCHOR_STYLE_PROMPT_DIR", prompt)
	if _, err := readOrderBellAsset(); !errors.Is(err, ErrOrderBellMissing) {
		t.Fatal("explicit missing asset fell back to another installation", err)
	}
	if err := os.WriteFile(filepath.Join(audioDir, "order-bell.wav"), make([]byte, (1<<20)+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readOrderBellAsset(); !errors.Is(err, ErrOrderBellMissing) {
		t.Fatal("oversized bell accepted", err)
	}
}

func TestOrderBellWAVParserHandlesExtraChunksAndStreamingSize(t *testing.T) {
	raw := orderPCMToWAV([]byte{1, 0, 2, 0})
	withJunk := append(append(append([]byte{}, raw[:36]...), []byte{'J', 'U', 'N', 'K', 1, 0, 0, 0, 7, 0}...), raw[36:]...)
	if pcm, ok := orderPCMFromWAV(withJunk); !ok || !bytes.Equal(pcm, raw[44:]) {
		t.Fatal("optional odd-sized chunk lost speech", ok)
	}
	binary.LittleEndian.PutUint32(raw[40:], 0xffffffff)
	if pcm, ok := orderPCMFromWAV(raw); !ok || len(pcm) != 4 {
		t.Fatal("pipe data length rejected", ok)
	}
}

func TestOrderBellRealAssetNormalizesStereo44100AndSpeech16000(t *testing.T) {
	ffmpeg := strings.TrimSpace(os.Getenv("FFMPEG_PATH"))
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	if _, err := exec.LookPath(ffmpeg); err != nil {
		t.Skip("real FFmpeg unavailable")
	}
	asset := filepath.Join("..", "..", "..", "configs", "audio", "order-bell.wav")
	bell, err := os.ReadFile(asset)
	if err != nil {
		t.Fatal(err)
	}
	speech := orderPCMToWAV(bytes.Repeat([]byte{0x20, 0x01}, 1600))
	binary.LittleEndian.PutUint32(speech[24:], 16000)
	binary.LittleEndian.PutUint32(speech[28:], 32000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(speech) }))
	defer server.Close()
	var combined []byte
	c := orderBellComposer{load: func() ([]byte, error) { return bell, nil }, client: server.Client(), ffmpeg: ffmpeg, publish: func(_ context.Context, raw []byte) (string, error) {
		combined = raw
		return "http://fixture.local/combined.wav", nil
	}}
	out, err := c.compose(t.Context(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	pcm, ok := orderPCMFromWAV(combined)
	if !ok || out.BellDurationMS < 1410 || out.BellDurationMS > 1425 || out.DurationMS < 1610 || out.DurationMS > 1625 || !bytes.Equal(pcm[:len(c.bellPCM)], c.bellPCM) {
		t.Fatal("real asset rate, ordering or duration changed", ok, out)
	}
	gap := pcm[len(c.bellPCM) : len(c.bellPCM)+orderAudioRate*2/10]
	if !bytes.Equal(gap, make([]byte, len(gap))) {
		t.Fatal("bell/speech gap contains noise")
	}
	if path := os.Getenv("ORDER_BELL_TEST_ARTIFACT"); path != "" {
		if err := os.WriteFile(path, combined, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
