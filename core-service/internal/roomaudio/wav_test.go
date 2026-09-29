package roomaudio

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
)

func testPCM24MonoWAV(durationMS int) []byte {
	samples := SampleRate * durationMS / 1000
	dataSize := samples * BytesPerSample
	audio := make([]byte, 44+dataSize)
	copy(audio[0:4], "RIFF")
	binary.LittleEndian.PutUint32(audio[4:8], uint32(len(audio)-8))
	copy(audio[8:12], "WAVE")
	copy(audio[12:16], "fmt ")
	binary.LittleEndian.PutUint32(audio[16:20], 16)
	binary.LittleEndian.PutUint16(audio[20:22], 1)
	binary.LittleEndian.PutUint16(audio[22:24], Channels)
	binary.LittleEndian.PutUint32(audio[24:28], SampleRate)
	binary.LittleEndian.PutUint32(audio[28:32], SampleRate*Channels*BytesPerSample)
	binary.LittleEndian.PutUint16(audio[32:34], Channels*BytesPerSample)
	binary.LittleEndian.PutUint16(audio[34:36], BytesPerSample*8)
	copy(audio[36:40], "data")
	binary.LittleEndian.PutUint32(audio[40:44], uint32(dataSize))
	return audio
}

func TestStreamWAVPCMUsesTwentyMillisecondFrames(t *testing.T) {
	audio := testPCM24MonoWAV(100)
	var cursors []int
	err := StreamWAVPCM(context.Background(), bytes.NewReader(audio), 0, func(frame []byte, cursorMS int) error {
		if len(frame) != PCMBytesPerFrame {
			t.Fatalf("frame bytes=%d want=%d", len(frame), PCMBytesPerFrame)
		}
		cursors = append(cursors, cursorMS)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cursors) != 5 {
		t.Fatalf("frames=%d want=5", len(cursors))
	}
	for index, cursor := range cursors {
		if cursor != index*FrameDurationMS {
			t.Fatalf("cursor[%d]=%d", index, cursor)
		}
	}
}

func TestStreamWAVPCMStartsFromRequestedOffset(t *testing.T) {
	audio := testPCM24MonoWAV(120)
	var cursors []int
	err := StreamWAVPCM(context.Background(), bytes.NewReader(audio), 40, func(_ []byte, cursorMS int) error {
		cursors = append(cursors, cursorMS)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cursors) != 4 || cursors[0] != 40 || cursors[3] != 100 {
		t.Fatalf("cursors=%v", cursors)
	}
}

func TestStreamWAVPCMTreatsTailEOFAsNormalCompletion(t *testing.T) {
	audio := testPCM24MonoWAV(100)
	declaredDataSize := binary.LittleEndian.Uint32(audio[40:44])
	binary.LittleEndian.PutUint32(audio[40:44], declaredDataSize+uint32(PCMBytesPerFrame))
	var frames int
	err := StreamWAVPCM(context.Background(), bytes.NewReader(audio), 0, func(_ []byte, _ int) error {
		frames++
		return nil
	})
	if err != nil {
		t.Fatalf("tail EOF must complete normally after emitted PCM: %v", err)
	}
	if frames != 5 {
		t.Fatalf("frames=%d want=5", frames)
	}
}

func TestStreamWAVPCMRejectsEOFBeforeFirstFrame(t *testing.T) {
	audio := testPCM24MonoWAV(100)
	audio = audio[:44]
	err := StreamWAVPCM(context.Background(), bytes.NewReader(audio), 0, func(_ []byte, _ int) error {
		return nil
	})
	if err == nil {
		t.Fatal("empty data body must still be treated as a broken source")
	}
}
