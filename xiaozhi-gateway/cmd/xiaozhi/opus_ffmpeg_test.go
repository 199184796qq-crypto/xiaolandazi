package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Validate the upload with a real, independent Opus encoder/decoder when it is
// installed. This is not a physical microphone/ASR integration test.
func TestMicrophoneOggRealOpusDecode(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable: independent Opus decode not exercised")
	}
	source := filepath.Join(t.TempDir(), "encoder.ogg")
	command := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000:duration=0.42", "-ac", "1", "-c:a", "libopus", "-frame_duration", "60", "-f", "ogg", source)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("independent Opus encoder: %v %s", err, out)
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	packets := testExtractOggPackets(t, raw)
	if len(packets) < 3 || !bytes.HasPrefix(packets[0], []byte("OpusHead")) || !bytes.HasPrefix(packets[1], []byte("OpusTags")) {
		t.Fatal("independent encoder did not produce complete Opus packets")
	}
	encoded, err := microphoneOgg(packets[2:], 16000)
	if err != nil {
		t.Fatal(err)
	}
	var samples int
	for _, packet := range packets[2:] {
		n, err := opusPacketSamples(packet)
		if err != nil {
			t.Fatal(err)
		}
		samples += n
	}
	if samples < 48000/3 || samples > 48000 {
		t.Fatalf("unexpected fixture length: %d/48000 seconds", samples)
	}
	upload := filepath.Join(t.TempDir(), "upload.ogg")
	if err := os.WriteFile(upload, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	decoded, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-xerror", "-i", upload, "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1").Output()
	if err != nil {
		if failure, ok := err.(*exec.ExitError); ok {
			t.Fatalf("uploaded Ogg independent decode: %s", failure.Stderr)
		}
		t.Fatal(err)
	}
	if len(decoded)/2 != samples/3 {
		t.Fatalf("granule/PCM length mismatch: decoded=%d expected=%d", len(decoded)/2, samples/3)
	}
	if bytes.Equal(decoded, make([]byte, len(decoded))) {
		t.Fatal("decoded independent fixture is all silent")
	}
	t.Logf("independent real Opus -> upload Ogg -> PCM: packets=%d samples16k=%d", len(packets)-2, len(decoded)/2)
}

func testExtractOggPackets(t *testing.T, raw []byte) [][]byte {
	t.Helper()
	var packets [][]byte
	var pending []byte
	for len(raw) != 0 {
		if len(raw) < 27 || string(raw[:4]) != "OggS" {
			t.Fatal("invalid source Ogg page")
		}
		segments := int(raw[26])
		if len(raw) < 27+segments {
			t.Fatal("truncated source Ogg laces")
		}
		pageLength := 27 + segments
		for _, lace := range raw[27:pageLength] {
			pageLength += int(lace)
		}
		if len(raw) < pageLength {
			t.Fatal("truncated source Ogg data")
		}
		page := append([]byte(nil), raw[:pageLength]...)
		checksum := binary.LittleEndian.Uint32(page[22:26])
		clear(page[22:26])
		if oggCRC(page) != checksum {
			t.Fatal("independent source Ogg checksum mismatch")
		}
		offset := 27 + segments
		for _, lace := range raw[27:offset] {
			end := offset + int(lace)
			pending = append(pending, raw[offset:end]...)
			offset = end
			if lace != 255 {
				packets = append(packets, pending)
				pending = nil
			}
		}
		raw = raw[pageLength:]
	}
	if len(pending) != 0 {
		t.Fatal("incomplete source Ogg packet")
	}
	return packets
}
