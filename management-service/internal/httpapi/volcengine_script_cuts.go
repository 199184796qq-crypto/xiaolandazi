package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"livecompanion/management/internal/audiocuts"
)

// volcengineCutAlgorithm 标识切点算法版本。算法变更时递增，旧表自动失效。
const volcengineCutAlgorithm = "audiocuts-v1"

// volcengineCutTable 是随音频资产一起归档的切点表。
// 在归一化后的最终播放 PCM 上计算；加噪和位置码在扫描之后处理。
type volcengineCutTable struct {
	Key         string            `json:"key"`
	Percent     int               `json:"percent"`
	Algorithm   string            `json:"algorithm"`
	SourceSHA   string            `json:"source_sha256"`
	SampleRate  int               `json:"sample_rate"`
	SampleCount int64             `json:"sample_count"`
	MeanRMS     float64           `json:"mean_rms"`
	Points      []audiocuts.Point `json:"points"`
	Selected    []audiocuts.Point `json:"selected"`
}

func volcengineCutsPath(dir, key string, percent int) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(dir, fmt.Sprintf("cuts-%d-%s.json", percent, hex.EncodeToString(sum[:6])))
}

// readWAVPCM16Mono 读取 24 kHz 单声道 16 位 WAV 的采样。
func readWAVPCM16Mono(path string) ([]int16, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxVolcengineRenderBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if int64(len(raw)) > maxVolcengineRenderBytes || len(raw) < 44 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, 0, errors.New("不是 WAV 文件")
	}
	raw, err = normalizeScriptFirstWAVHeader(raw)
	if err != nil {
		return nil, 0, err
	}
	var sampleRate int
	var channels int
	var bits int
	var format int
	dataAt := -1
	dataLen := 0
	for i := 12; i+8 <= len(raw); {
		id := string(raw[i : i+4])
		size := int(binary.LittleEndian.Uint32(raw[i+4 : i+8]))
		body := i + 8
		if size < 0 || size > len(raw)-body {
			return nil, 0, errors.New("WAV 分块不完整")
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, errors.New("WAV fmt 段损坏")
			}
			channels = int(binary.LittleEndian.Uint16(raw[body+2 : body+4]))
			format = int(binary.LittleEndian.Uint16(raw[body : body+2]))
			sampleRate = int(binary.LittleEndian.Uint32(raw[body+4 : body+8]))
			bits = int(binary.LittleEndian.Uint16(raw[body+14 : body+16]))
		case "data":
			dataAt = body
			dataLen = size
		}
		i = body + size + size%2
	}
	if dataAt < 0 {
		return nil, 0, errors.New("WAV 缺少 data 段")
	}
	if format != 1 || channels != 1 || bits != 16 {
		return nil, 0, fmt.Errorf("只支持单声道 16 位，实际 %d 声道 %d 位", channels, bits)
	}
	if sampleRate != 24000 {
		return nil, 0, errors.New("WAV 采样率无效")
	}
	if dataLen < 2 || dataLen%2 != 0 {
		return nil, 0, errors.New("WAV 数据为空")
	}
	count := dataLen / 2
	pcm := make([]int16, count)
	for i := 0; i < count; i++ {
		pcm[i] = int16(binary.LittleEndian.Uint16(raw[dataAt+i*2 : dataAt+i*2+2]))
	}
	return pcm, sampleRate, nil
}

// ensureVolcengineCuts 为一段音频生成并缓存切点表。
// 缺表时保留完整音频，不根据猜测的位置硬切。
func (s *Server) ensureVolcengineCuts(ctx context.Context, audioPath, dir, key string, percent int) (volcengineCutTable, error) {
	empty := volcengineCutTable{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	pcm, sampleRate, err := readWAVPCM16Mono(audioPath)
	if err != nil {
		return empty, err
	}
	sum := sha256.Sum256(int16Bytes(pcm))
	table := volcengineCutTable{
		Key:         key,
		Percent:     percent,
		Algorithm:   volcengineCutAlgorithm,
		SourceSHA:   hex.EncodeToString(sum[:]),
		SampleRate:  sampleRate,
		SampleCount: int64(len(pcm)),
		MeanRMS:     audiocuts.MeanRMS(pcm),
	}
	points, err := audiocuts.Scan(pcm, sampleRate)
	if err != nil {
		return empty, err
	}
	durationMS := int32(int64(len(pcm)) * 1000 / int64(sampleRate))
	table.Points = points
	table.Selected = audiocuts.SelectForPlayback(points, durationMS)
	if err = writeVolcengineJSON(volcengineCutsPath(dir, key, percent), table); err != nil {
		return empty, err
	}
	log.Printf("cuts scanned key=%s percent=%d samples=%d points=%d selected=%d mean_rms=%.0f",
		key[:min(len(key), 8)], percent, len(pcm), len(table.Points), len(table.Selected), table.MeanRMS)
	return table, nil
}

func int16Bytes(pcm []int16) []byte {
	out := make([]byte, len(pcm)*2)
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(v))
	}
	return out
}

// loadVolcengineCuts 读取已归档的切点表；缺失或版本不符时返回 false。
func loadVolcengineCuts(dir, key string, percent int) (volcengineCutTable, bool) {
	var table volcengineCutTable
	if err := readVolcengineJSON(volcengineCutsPath(dir, key, percent), &table); err != nil {
		return table, false
	}
	if table.Algorithm != volcengineCutAlgorithm || table.Key != key || table.Percent != percent || table.SampleRate != 24000 || table.SampleCount <= 0 || len(table.SourceSHA) != 64 {
		return table, false
	}
	return table, true
}
