package capture

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type recordingStopWriter struct {
	bytes.Buffer
}

func (w *recordingStopWriter) Close() error { return nil }

func TestStopAudioReturnsFinalizingWithoutWaitingForFinalize(t *testing.T) {
	writer := &recordingStopWriter{}
	manager := &Manager{
		now: time.Now,
		rooms: map[int64]*roomState{
			16: {
				recording: &recording{
					status: RecordingStatus{
						Status:    RecordingRecording,
						StartedAt: time.Now(),
					},
					stdin: io.WriteCloser(writer),
					done:  make(chan struct{}),
				},
			},
		},
	}

	type stopResult struct {
		snapshot Snapshot
		err      error
	}
	resultCh := make(chan stopResult, 1)
	go func() {
		snapshot, err := manager.StopAudio(context.Background(), 16)
		resultCh <- stopResult{snapshot: snapshot, err: err}
	}()

	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.snapshot.Mode != ModeFinalizing || result.snapshot.Recording == nil || result.snapshot.Recording.Status != RecordingFinalizing {
			t.Fatalf("snapshot=%#v", result.snapshot)
		}
		if writer.String() != "q\n" {
			t.Fatalf("ffmpeg stop command=%q", writer.String())
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("StopAudio waited for finalization instead of returning immediately")
	}
}

func TestValidateWAVAudioAcceptsNonEmptyDataChunk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recording.wav")
	if err := os.WriteFile(path, testWAVBytes([]byte{1, 2, 3, 4}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateWAVAudio(path); err != nil {
		t.Fatalf("validateWAVAudio() error=%v", err)
	}
}

func TestValidateWAVAudioRejectsEmptyDataChunk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recording.wav")
	if err := os.WriteFile(path, testWAVBytes(nil), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateWAVAudio(path); err == nil {
		t.Fatal("validateWAVAudio() accepted an empty WAV")
	}
}

func testWAVBytes(data []byte) []byte {
	buf := bytes.NewBuffer(nil)
	buf.WriteString("RIFF")
	_ = binary.Write(buf, binary.LittleEndian, uint32(36+len(data)))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(buf, binary.LittleEndian, uint32(44100))
	_ = binary.Write(buf, binary.LittleEndian, uint32(176400))
	_ = binary.Write(buf, binary.LittleEndian, uint16(4))
	_ = binary.Write(buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(data)))
	buf.Write(data)
	return buf.Bytes()
}

func TestResolveFFmpegAcceptsExplicitExistingFile(t *testing.T) {
	name := "ffmpeg-test"
	if filepath.Ext(os.Args[0]) == ".exe" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveFFmpeg(path)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(resolved) != filepath.Clean(path) {
		t.Fatalf("resolved=%q want=%q", resolved, path)
	}
}

func TestResolveFFmpegPrefersBundledFromWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "core-service", "internal", "capture")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	binaryName := "ffmpeg"
	if filepath.Ext(os.Args[0]) == ".exe" {
		binaryName = "ffmpeg.exe"
	}
	bundled := filepath.Join(root, "data", "tools", "ffmpeg", "bin", binaryName)
	if err := os.MkdirAll(filepath.Dir(bundled), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundled, []byte("bundled"), 0o755); err != nil {
		t.Fatal(err)
	}

	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDir) })

	resolved, err := resolveFFmpeg("")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(resolved) != filepath.Clean(bundled) {
		t.Fatalf("resolved=%q want bundled=%q", resolved, bundled)
	}
}
