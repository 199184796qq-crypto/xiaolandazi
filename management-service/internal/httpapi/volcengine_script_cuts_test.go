package httpapi

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func writeTestWAV(t *testing.T, path string, pcm []int16, rate int) {
	t.Helper()
	data := make([]byte, len(pcm)*2)
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(v))
	}
	header := make([]byte, 44)
	copy(header[0:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+len(data)))
	copy(header[8:], "WAVE")
	copy(header[12:], "fmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 1)
	binary.LittleEndian.PutUint32(header[24:], uint32(rate))
	binary.LittleEndian.PutUint32(header[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(len(data)))
	if err := os.WriteFile(path, append(header, data...), 0600); err != nil {
		t.Fatalf("写测试 WAV 失败: %v", err)
	}
}

func TestReadWAVPCM16MonoRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.wav")
	pcm := []int16{0, 100, -100, 32767, -32768, 5}
	writeTestWAV(t, path, pcm, 24000)

	got, rate, err := readWAVPCM16Mono(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if rate != 24000 {
		t.Fatalf("采样率错误: %d", rate)
	}
	if len(got) != len(pcm) {
		t.Fatalf("采样数不符: %d != %d", len(got), len(pcm))
	}
	for i := range pcm {
		if got[i] != pcm[i] {
			t.Fatalf("第 %d 个采样不符: %d != %d", i, got[i], pcm[i])
		}
	}
}

func TestReadWAVRejectsNonWAV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.wav")
	os.WriteFile(path, []byte("not a wav file at all"), 0600)
	if _, _, err := readWAVPCM16Mono(path); err == nil {
		t.Fatal("非 WAV 应报错")
	}
}

func TestEnsureVolcengineCutsWritesTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "voice.wav")
	rate := 24000
	var pcm []int16
	pos := 0
	for i := 0; i < 6; i++ {
		for j := 0; j < rate/2; j++ {
			pcm = append(pcm, int16(2500*math.Sin(2*math.Pi*180*float64(pos+j)/float64(rate))))
		}
		pos += rate / 2
		for j := 0; j < rate/3; j++ {
			pcm = append(pcm, 0)
		}
		pos += rate / 3
	}
	writeTestWAV(t, path, pcm, rate)

	s := &Server{}
	table, err := s.ensureVolcengineCuts(t.Context(), path, dir, "test-key-0123456789", 100)
	if err != nil {
		t.Fatalf("生成切点表失败: %v", err)
	}
	if len(table.Points) == 0 {
		t.Fatal("没有扫出候选点")
	}
	if table.Algorithm != volcengineCutAlgorithm || table.Key == "" || table.SourceSHA == "" {
		t.Fatalf("表元数据缺失: %+v", table)
	}
	loaded, ok := loadVolcengineCuts(dir, "test-key-0123456789", 100)
	if !ok {
		t.Fatal("回读失败")
	}
	if len(loaded.Selected) != len(table.Selected) || len(loaded.Points) != len(table.Points) {
		t.Fatalf("回读内容不一致")
	}
}

func TestLoadVolcengineCutsMissing(t *testing.T) {
	if _, ok := loadVolcengineCuts(t.TempDir(), "nope", 100); ok {
		t.Fatal("缺表应返回 false")
	}
}
