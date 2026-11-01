package httpapi

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// Qwen HTTP can finish a WAV download with the streaming writer's placeholder
// RIFF/data lengths. Recognize only the exact header observed in production,
// after the paid response body was fully retained. Ordinary oversized chunks
// remain errors; this is not a general permission to clamp truncated files.
// The immutable paid source and its checksum are never rewritten. Only the
// returned playback/archive copy receives actual lengths; PCM stays identical.
func normalizeScriptFirstWAVHeader(raw []byte) ([]byte, error) {
	if len(raw) < 8 || string(raw[:4]) != "RIFF" || binary.LittleEndian.Uint32(raw[4:8]) != 0x7fffffbf {
		return raw, nil
	}
	if len(raw) <= 44 || int64(len(raw)) > maxVolcengineRenderBytes ||
		string(raw[8:12]) != "WAVE" || string(raw[12:16]) != "fmt " || binary.LittleEndian.Uint32(raw[16:20]) != 16 ||
		binary.LittleEndian.Uint16(raw[20:22]) != 1 || binary.LittleEndian.Uint16(raw[22:24]) != 1 ||
		binary.LittleEndian.Uint32(raw[24:28]) != 24000 || binary.LittleEndian.Uint32(raw[28:32]) != 48000 ||
		binary.LittleEndian.Uint16(raw[32:34]) != 2 || binary.LittleEndian.Uint16(raw[34:36]) != 16 ||
		string(raw[36:40]) != "data" || binary.LittleEndian.Uint32(raw[40:44]) != 0x7fffff9b || (len(raw)-44)%2 != 0 {
		return nil, errors.New("标准声音WAV占位头与已接收PCM不匹配")
	}
	canonical := bytes.Clone(raw)
	binary.LittleEndian.PutUint32(canonical[4:8], uint32(len(canonical)-8))
	binary.LittleEndian.PutUint32(canonical[40:44], uint32(len(canonical)-44))
	return canonical, nil
}
