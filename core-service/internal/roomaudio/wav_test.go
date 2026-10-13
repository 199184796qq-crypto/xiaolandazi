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

func TestLeadingSilenceMS(t *testing.T) {
	// 构造：前导静音 120ms + 非静音。data 全零即静音，之后写入大振幅样本。
	audio := testPCM24MonoWAV(500)
	data := audio[44:]
	leadingSamples := SampleRate * 120 / 1000
	for i := 0; i < leadingSamples; i++ {
		// 保持静音（全零，16bit 小端）
		binary.LittleEndian.PutUint16(data[i*2:i*2+2], 0)
	}
	for i := leadingSamples; i < SampleRate*500/1000; i++ {
		binary.LittleEndian.PutUint16(data[i*2:i*2+2], 1000)
	}
	if got := LeadingSilenceMS(audio); got != 120 {
		t.Fatalf("LeadingSilenceMS=%dms want=120ms", got)
	}

	// 非 WAV 返回 0
	if got := LeadingSilenceMS([]byte("not a wav")); got != 0 {
		t.Fatalf("LeadingSilenceMS(non-wav)=%d want=0", got)
	}

	// 全静音（无语音）不应整体跳过，返回 0
	allSilent := testPCM24MonoWAV(500)
	if got := LeadingSilenceMS(allSilent); got != 0 {
		t.Fatalf("LeadingSilenceMS(all-silent)=%dms want=0ms", got)
	}
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

func TestStreamWAVPCMEmitsAndSilencePadsFinalPartialFrame(t *testing.T) {
	audio := testPCM24MonoWAV(45)
	for i := 44 + 2*PCMBytesPerFrame; i < len(audio); i++ {
		audio[i] = 0x5a
	}
	var frames [][]byte
	err := StreamWAVPCM(context.Background(), bytes.NewReader(audio), 0, func(frame []byte, _ int) error {
		frames = append(frames, append([]byte(nil), frame...))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 {
		t.Fatalf("frames=%d want=3", len(frames))
	}
	spokenBytes := SampleRate * 5 / 1000 * BytesPerSample
	if !bytes.Equal(frames[2][:spokenBytes], bytes.Repeat([]byte{0x5a}, spokenBytes)) {
		t.Fatal("final spoken PCM was discarded")
	}
	if !bytes.Equal(frames[2][spokenBytes:], make([]byte, PCMBytesPerFrame-spokenBytes)) {
		t.Fatal("final partial frame was not silence padded")
	}
}

func TestStreamWAVPCMEmitsPhysicalPartialFrameWithStreamingPlaceholder(t *testing.T) {
	audio := testPCM24MonoWAV(45)
	binary.LittleEndian.PutUint32(audio[40:44], uint32(len(audio))*100)
	for i := 44 + 2*PCMBytesPerFrame; i < len(audio); i++ {
		audio[i] = 0x5a
	}
	var frames [][]byte
	err := StreamWAVPCM(context.Background(), bytes.NewReader(audio), 0, func(frame []byte, _ int) error {
		frames = append(frames, append([]byte(nil), frame...))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 {
		t.Fatalf("frames=%d want=3", len(frames))
	}
	spokenBytes := SampleRate * 5 / 1000 * BytesPerSample
	if !bytes.Equal(frames[2][:spokenBytes], bytes.Repeat([]byte{0x5a}, spokenBytes)) {
		t.Fatal("physical partial frame was discarded for streaming WAV")
	}
	if !bytes.Equal(frames[2][spokenBytes:], make([]byte, PCMBytesPerFrame-spokenBytes)) {
		t.Fatal("physical partial frame was not silence padded")
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
