package main

import (
	"bytes"
	"encoding/binary"
	"os/exec"
	"testing"
)

func TestMicrophoneOggHeadersCRCAndGranule(t *testing.T) {
	packets := [][]byte{{0xf8, 0xff, 0xfe}, {0xf8, 0xff, 0xfe}}
	ogg, err := microphoneOgg(packets, 16000)
	if err != nil {
		t.Fatal(err)
	}
	page := 0
	for len(ogg) > 0 {
		if len(ogg) < 27 || string(ogg[:4]) != "OggS" {
			t.Fatal("invalid page")
		}
		segmentCount := int(ogg[26])
		size := 27 + segmentCount
		for _, lace := range ogg[27:size] {
			size += int(lace)
		}
		if size > len(ogg) {
			t.Fatal("truncated page")
		}
		data := append([]byte(nil), ogg[:size]...)
		got := binary.LittleEndian.Uint32(data[22:26])
		clear(data[22:26])
		if got != oggCRC(data) {
			t.Fatal("bad CRC")
		}
		if int(binary.LittleEndian.Uint32(data[18:22])) != page {
			t.Fatal("bad sequence")
		}
		if page == 0 {
			head := data[27+segmentCount:]
			if string(head[:8]) != "OpusHead" || head[9] != 1 || binary.LittleEndian.Uint32(head[12:16]) != 16000 || data[5] != 2 {
				t.Fatal("bad input header")
			}
		}
		if page == 3 && (data[5] != 4 || binary.LittleEndian.Uint64(data[6:14]) != 1920) {
			t.Fatal("bad final granule")
		}
		page++
		ogg = ogg[size:]
	}
	if page != 4 {
		t.Fatal("missing Ogg pages")
	}
	if _, err := microphoneOgg(nil, 16000); err == nil {
		t.Fatal("empty upload accepted")
	}
	if _, err := microphoneOgg(packets, 44100); err == nil {
		t.Fatal("invalid input rate accepted")
	}
	long := make([][]byte, 401)
	for index := range long {
		long[index] = packets[0]
	}
	if _, err := microphoneOgg(long, 16000); err == nil {
		t.Fatal("over 8 second upload accepted")
	}
	for _, invalid := range [][]byte{nil, {0}, {0xff, 0}, {0xff, 63}, make([]byte, 8193)} {
		if _, err := opusPacketSamples(invalid); err == nil {
			t.Errorf("invalid TOC accepted %v", invalid[:min(len(invalid), 4)])
		}
	}
}

func TestMicrophoneOggDecodesWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	pcm := make([]byte, 16000*2/5) // 200 ms of mono 16 kHz audio.
	encoder := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "s16le", "-ar", "16000", "-ac", "1", "-i", "pipe:0", "-c:a", "libopus", "-frame_duration", "20", "-f", "ogg", "pipe:1")
	encoder.Stdin = bytes.NewReader(pcm)
	encoded, err := encoder.Output()
	if err != nil {
		t.Skipf("libopus encoder unavailable: %v", err)
	}
	var packets [][]byte
	if err := readOggPackets(bytes.NewReader(encoded), func(packet []byte, index int) error {
		if index >= 2 {
			packets = append(packets, append([]byte(nil), packet...))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	upload, err := microphoneOgg(packets, 16000)
	if err != nil {
		t.Fatal(err)
	}
	decoder := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-i", "pipe:0", "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1")
	decoder.Stdin = bytes.NewReader(upload)
	decoded, err := decoder.Output()
	if err != nil || len(decoded) < len(pcm) || len(decoded) > len(pcm)+16000*2/10 {
		t.Fatalf("decoded bytes=%d expected~%d error=%v", len(decoded), len(pcm), err)
	}
}
