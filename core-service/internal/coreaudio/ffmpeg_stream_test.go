package coreaudio

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"livecompanion/core/internal/roomaudio"
)

func stereo48kWAV(durationMS int) []byte {
	const sampleRate = 48000
	const channels = 2
	const bytesPerSample = 2
	samples := sampleRate * durationMS / 1000
	dataSize := samples * channels * bytesPerSample
	audio := make([]byte, 44+dataSize)
	copy(audio[0:4], "RIFF")
	binary.LittleEndian.PutUint32(audio[4:8], uint32(len(audio)-8))
	copy(audio[8:12], "WAVE")
	copy(audio[12:16], "fmt ")
	binary.LittleEndian.PutUint32(audio[16:20], 16)
	binary.LittleEndian.PutUint16(audio[20:22], 1)
	binary.LittleEndian.PutUint16(audio[22:24], channels)
	binary.LittleEndian.PutUint32(audio[24:28], sampleRate)
	binary.LittleEndian.PutUint32(audio[28:32], sampleRate*channels*bytesPerSample)
	binary.LittleEndian.PutUint16(audio[32:34], channels*bytesPerSample)
	binary.LittleEndian.PutUint16(audio[34:36], 16)
	copy(audio[36:40], "data")
	binary.LittleEndian.PutUint32(audio[40:44], uint32(dataSize))
	return audio
}

func TestStreamAudioURLAsCorePCMNormalizesStereo48kWAV(t *testing.T) {
	if _, err := exec.LookPath(ffmpegExecutable()); err != nil {
		t.Skip("ffmpeg is not available")
	}
	source := stereo48kWAV(100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(source)
	}))
	defer server.Close()

	var frames int
	var lastCursor int
	err := streamAudioURLAsCorePCM(context.Background(), server.URL, 0, func(frame []byte, cursorMS int) error {
		if len(frame) != roomaudio.PCMBytesPerFrame {
			t.Fatalf("frame bytes=%d want=%d", len(frame), roomaudio.PCMBytesPerFrame)
		}
		frames++
		lastCursor = cursorMS
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if frames != 5 {
		t.Fatalf("frames=%d want=5", frames)
	}
	if lastCursor != 80 {
		t.Fatalf("last cursor=%d want=80", lastCursor)
	}
}
