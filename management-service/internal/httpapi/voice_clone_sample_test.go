package httpapi

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"livecompanion/management/internal/storage"
)

func TestCloneSampleUsesOSSWithLocalUploadStorage(t *testing.T) {
	var uploaded, deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/tenants/14/voice-samples/tmp/7-") {
			t.Errorf("unexpected object key: %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			uploaded = string(data) == "normalized sample" && r.Header.Get("Content-Type") == "audio/wav"
		case http.MethodDelete:
			deleted = true
		default:
			t.Errorf("unexpected request: %s", r.Method)
		}
		w.Header().Set("ETag", "test")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	registry, err := storage.NewRegistry(storage.Config{Driver: "local", LocalRoot: t.TempDir(), OSSEndpoint: server.URL, OSSPublicEndpoint: "https://oss-cn-hangzhou.aliyuncs.com", OSSBucket: "clone-test", OSSAccessKeyID: "test-key", OSSAccessKeySecret: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sampleURL, cleanup, err := uploadCloneSample(ctx, registry, 14, 7, []byte("normalized sample"), "audio/wav")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(sampleURL)
	if !uploaded || parsed.Scheme != "https" || parsed.Host != "clone-test.oss-cn-hangzhou.aliyuncs.com" || parsed.Query().Get("Signature") == "" {
		t.Fatal("server did not upload and sign the OSS sample")
	}
	if registry.Current().Driver() != "local" {
		t.Fatal("ordinary upload storage was changed")
	}
	cancel()
	cleanup()
	if !deleted {
		t.Fatal("temporary OSS sample was not cleaned up after canceled request")
	}
}

func TestCloneSampleRequiresOSS(t *testing.T) {
	registry, err := storage.NewRegistry(storage.Config{LocalRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := uploadCloneSample(context.Background(), registry, 14, 7, []byte("sample"), "audio/wav"); err == nil {
		t.Fatal("local storage cannot create a supplier URL")
	}
}

func TestUploadedVoiceSampleDurationRange(t *testing.T) {
	if _, err := exec.LookPath(ffmpegExecutable()); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	for _, ms := range []int{4900, 5000, 9600, 20000, 60000, 61000} {
		t.Run(strconv.Itoa(ms), func(t *testing.T) {
			dataSize := ms * 32
			raw := make([]byte, 44+dataSize)
			copy(raw, "RIFF")
			binary.LittleEndian.PutUint32(raw[4:], uint32(36+dataSize))
			copy(raw[8:], "WAVEfmt ")
			binary.LittleEndian.PutUint32(raw[16:], 16)
			binary.LittleEndian.PutUint16(raw[20:], 1)
			binary.LittleEndian.PutUint16(raw[22:], 1)
			binary.LittleEndian.PutUint32(raw[24:], 16000)
			binary.LittleEndian.PutUint32(raw[28:], 32000)
			binary.LittleEndian.PutUint16(raw[32:], 2)
			binary.LittleEndian.PutUint16(raw[34:], 16)
			copy(raw[36:], "data")
			binary.LittleEndian.PutUint32(raw[40:], uint32(dataSize))
			path := filepath.Join(t.TempDir(), "sample.wav")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			_, actual, err := normalizeUploadedVoiceSample(context.Background(), path)
			valid := ms >= 5000 && ms <= 60000
			if valid && (err != nil || actual != int64(ms)) {
				t.Fatalf("valid sample %d rejected: actual=%d err=%v", ms, actual, err)
			}
			if !valid && err == nil {
				t.Fatalf("out-of-range sample %d accepted", ms)
			}
		})
	}
}
