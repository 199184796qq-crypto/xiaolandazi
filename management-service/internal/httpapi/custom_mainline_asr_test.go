package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"livecompanion/management/internal/speechasr"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type customASRTestStore struct {
	objects map[string][]byte
	deleted []string
}

func (s *customASRTestStore) Driver() string { return "oss" }
func (s *customASRTestStore) Bucket() string { return "fake" }
func (s *customASRTestStore) Put(_ context.Context, key string, reader io.Reader, _ string) error {
	raw, err := io.ReadAll(reader)
	s.objects[key] = raw
	return err
}
func (s *customASRTestStore) PutPrivate(ctx context.Context, key string, reader io.Reader, mime string) error {
	return s.Put(ctx, key, reader, mime)
}
func (s *customASRTestStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.objects[key])), nil
}
func (s *customASRTestStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	s.deleted = append(s.deleted, key)
	return nil
}
func (s *customASRTestStore) SignedURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return key, nil
}

func TestCustomMainlineChunkedASROffsetsAndCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "normalized.wav")
	pcm := make([]byte, 120000) // 2.5 seconds; tiny chunks exercise the large-file path.
	for i := range pcm {
		pcm[i] = byte(i % 251)
	}
	if err := os.WriteFile(path, append(customPCMHeader(int64(len(pcm))), pcm...), 0600); err != nil {
		t.Fatal(err)
	}
	for _, failCall := range []int{0, 2} {
		store := &customASRTestStore{objects: map[string][]byte{}}
		calls := 0
		result, duration, err := transcribeCustomPCM(t.Context(), store, path, "formal.wav", "temporary/", 48000, func(_ context.Context, key string) (speechasr.Result, error) {
			calls++
			part := store.objects[key]
			if len(part) < 44 || !bytes.Equal(part[44:], pcm[(calls-1)*48000:min(calls*48000, len(pcm))]) {
				t.Fatal("PCM bytes changed between parts")
			}
			if calls == failCall {
				return speechasr.Result{}, errors.New("upstream failed")
			}
			return speechasr.Result{Text: "句子", Sentences: []speechasr.Sentence{{BeginTime: 0, EndTime: 100, Text: "句子"}}}, nil
		})
		if len(store.objects) != 0 || len(store.deleted) != calls {
			t.Fatal("ASR temporary audio leaked", calls, store.deleted)
		}
		if failCall > 0 {
			if err == nil || calls != 2 {
				t.Fatal("failed paid chunk was retried", calls, err)
			}
			continue
		}
		if err != nil || duration != 2500 || len(result.Sentences) != 3 || result.Sentences[1].BeginTime != 1000 || result.Sentences[2].BeginTime != 2000 {
			t.Fatal(result, duration, err)
		}
	}
}

func TestCustomPCMDataRejectsTruncatedAudio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "short.wav")
	if err := os.WriteFile(path, append(customPCMHeader(48000), make([]byte, 10)...), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, _, err = customPCMData(file); err == nil {
		t.Fatal("truncated WAV accepted")
	}
}

func TestCustomPCMWithRealFFmpegNormalizationAndMerge(t *testing.T) {
	if _, err := exec.LookPath(ffmpegExecutable()); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	input := filepath.Join(t.TempDir(), "stereo.flac")
	cmd := exec.CommandContext(t.Context(), ffmpegExecutable(), "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.25", "-ac", "2", "-ar", "44100", "-c:a", "flac", input)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(raw))
	}
	normalized, err := normalizeAudioToCoreWAV(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(normalized)
	merged, err := concatenateCoreWAVs(t.Context(), []string{normalized, normalized})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(merged)
	file, err := os.Open(merged)
	if err != nil {
		t.Fatal(err)
	}
	_, size, err := customPCMData(file)
	_ = file.Close()
	if err != nil || size*1000/48000 != 500 {
		t.Fatal("real FFmpeg WAV was not parsed correctly", size, err)
	}
	sample, err := extractCloneSampleFromAudioPath(t.Context(), merged)
	if err != nil || len(sample) < 44 || len(sample) > 5<<20 {
		t.Fatal("server voice sample extraction failed", len(sample), err)
	}
}
