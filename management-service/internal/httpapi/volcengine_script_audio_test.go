package httpapi

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestVolcengineTempoUsesCachedAudioWithMeasuredDuration(t *testing.T) {
	if _, err := exec.LookPath(ffmpegExecutable()); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	raw := make([]byte, 44+48000*4)
	copy(raw, "RIFF")
	binary.LittleEndian.PutUint32(raw[4:], uint32(len(raw)-8))
	copy(raw[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(raw[16:], 16)
	binary.LittleEndian.PutUint16(raw[20:], 1)
	binary.LittleEndian.PutUint16(raw[22:], 1)
	binary.LittleEndian.PutUint32(raw[24:], 24000)
	binary.LittleEndian.PutUint32(raw[28:], 48000)
	binary.LittleEndian.PutUint16(raw[32:], 2)
	binary.LittleEndian.PutUint16(raw[34:], 16)
	copy(raw[36:], "data")
	binary.LittleEndian.PutUint32(raw[40:], uint32(len(raw)-44))
	for i := 0; i < (len(raw)-44)/2; i++ {
		sample := int16(4000 * math.Sin(float64(i)*2*math.Pi*440/24000))
		binary.LittleEndian.PutUint16(raw[44+2*i:], uint16(sample))
	}
	path := filepath.Join(t.TempDir(), "source.wav")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(raw)
	if n, err := volcengineWAVDuration(path); err != nil || n != 4000 {
		t.Fatal(n, err)
	}
	for _, percent := range []int{90, 110} {
		out, err := renderVolcengineTempo(t.Context(), path, percent)
		if err != nil {
			t.Fatal(err)
		}
		n, err := volcengineWAVDuration(out)
		if err != nil {
			t.Fatal(err)
		}
		want := 400000.0 / float64(percent)
		if math.Abs(float64(n)-want) > 75 {
			t.Fatalf("tempo %d duration %d expected %.1f", percent, n, want)
		}
		if err = os.Remove(out); err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || sha256.Sum256(after) != before {
		t.Fatal("paid source changed", err)
	}
	if _, err = renderVolcengineTempo(t.Context(), path, 89); err == nil {
		t.Fatal("bad tempo accepted")
	}
}
