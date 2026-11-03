// Package audiocode 在 PCM 音频中嵌入与读取位置码。
//
// 用途：音频被切开、拼接或从中间起播后，仍能读出"这是哪个资产、第几帧"。
// 只在 Management → Core 之间的规范 PCM 上使用；网关之后的 Opus 有损，不在范围内。
//
// 帧结构（100 毫秒一帧，24 kHz 下 2400 采样）：
//
//	前导 10 毫秒：11.6 kHz 单音，供解码端定位帧边界
//	数据 3 符号 × 30 毫秒：16 载波 QPSK，每符号 32 位，共 96 位 = 两份 48 位副本
//
// 载荷 48 位：资产代号 16 + 帧号 16 + 版本 8 + CRC 8。
package audiocode

import (
	"encoding/binary"
	"errors"
	"math"
	"sort"
)

const (
	// SampleRate 是唯一支持的采样率，与全链路一致。
	SampleRate = 24000
	// FrameSamples 是一帧的采样数（100 毫秒）。
	FrameSamples = 2400
	// PreambleSamples 是前导长度（10 毫秒）。
	PreambleSamples = 240
	// SymbolSamples 是每个数据符号的长度（30 毫秒）。
	SymbolSamples = 720
	// SymbolCount 是每帧的数据符号数。
	SymbolCount = 3
	// SyncHz 是前导音频率，独立于数据载波。
	SyncHz = 11600.0
	// LevelDBFS 是嵌入电平，实测 -60 dBFS 下单帧成功率 85%，5 帧窗口 100%。
	LevelDBFS = -60.0
	// FormatVersion 是当前编码格式版本。
	FormatVersion = 1
)

// carriers 是 16 个数据载波，8.0 到 11.0 kHz，间隔 200 Hz。
var carriers = [16]float64{
	8000, 8200, 8400, 8600, 8800, 9000, 9200, 9400,
	9600, 9800, 10000, 10200, 10400, 10600, 10800, 11000,
}

// cosTable 是相位查表，避免逐样本调用 math.Cos。
var cosTable = func() [1024]float64 {
	var t [1024]float64
	for i := range t {
		t[i] = math.Cos(2 * math.Pi * float64(i) / 1024)
	}
	return t
}()

// Payload 是一帧携带的信息。
type Payload struct {
	AssetTag uint16
	Frame    uint16
	Version  uint8
}

func crc8(data []byte) byte {
	var crc byte
	for _, b := range data {
		crc ^= b
		for i := 0; i < 8; i++ {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func packPayload(p Payload) []byte {
	out := make([]byte, 6)
	binary.BigEndian.PutUint16(out[0:], p.Frame)
	binary.BigEndian.PutUint16(out[2:], p.AssetTag)
	out[4] = p.Version
	out[5] = crc8(out[:5])
	return out
}

func unpackPayload(raw []byte) (Payload, bool) {
	if len(raw) != 6 || crc8(raw[:5]) != raw[5] {
		return Payload{}, false
	}
	return Payload{
		Frame:    binary.BigEndian.Uint16(raw[0:2]),
		AssetTag: binary.BigEndian.Uint16(raw[2:4]),
		Version:  raw[4],
	}, true
}

func payloadBits(p Payload) []byte {
	raw := packPayload(p)
	bits := make([]byte, 0, 96)
	for _, b := range raw {
		for i := 7; i >= 0; i-- {
			bits = append(bits, (b>>uint(i))&1)
		}
	}
	return bits // 48 位
}

func bitsPayload(bits []byte) (Payload, bool) {
	if len(bits) != 48 {
		return Payload{}, false
	}
	raw := make([]byte, 6)
	for i := 0; i < 6; i++ {
		var v byte
		for j := 0; j < 8; j++ {
			v = v<<1 | bits[i*8+j]
		}
		raw[i] = v
	}
	return unpackPayload(raw)
}

// qpskPhase 把两位映射到相位（格雷码）。
func qpskPhase(b0, b1 byte) float64 {
	switch {
	case b0 == 0 && b1 == 0:
		return 0
	case b0 == 0 && b1 == 1:
		return math.Pi / 2
	case b0 == 1 && b1 == 1:
		return math.Pi
	default:
		return 3 * math.Pi / 2
	}
}

func phaseQuadrant(phase float64) (byte, byte) {
	q := int(math.Round(phase/(math.Pi/2))) % 4
	if q < 0 {
		q += 4
	}
	switch q {
	case 0:
		return 0, 0
	case 1:
		return 0, 1
	case 2:
		return 1, 1
	default:
		return 1, 0
	}
}

// carrierRef 是每个载波在一个符号内的参考正余弦，避免逐样本做取模与查表。
type carrierRef struct {
	cos []float64
	sin []float64
}

var carrierTable = func() []carrierRef {
	table := make([]carrierRef, len(carriers))
	for c, freq := range carriers {
		ref := carrierRef{cos: make([]float64, SymbolSamples), sin: make([]float64, SymbolSamples)}
		for i := 0; i < SymbolSamples; i++ {
			angle := 2 * math.Pi * freq * float64(i) / SampleRate
			ref.cos[i] = math.Cos(angle)
			ref.sin[i] = math.Sin(angle)
		}
		table[c] = ref
	}
	return table
}()

// amplitude 返回每个载波的幅度，使总体 RMS 达到 LevelDBFS。'
func amplitude() float64 {
	full := 32768 * math.Pow(10, LevelDBFS/20)
	return full / math.Sqrt(float64(len(carriers))/2)
}

// Embed 在 pcm 上叠加位置码，返回新切片（不修改入参）。
// 采样数不变，因此切点表与时间轴保持有效。
func Embed(pcm []int16, assetTag uint16) []int16 {
	out := make([]int16, len(pcm))
	copy(out, pcm)
	amp := amplitude()
	frames := len(out) / FrameSamples
	for f := 0; f < frames; f++ {
		base := f * FrameSamples
		// 前导音
		for i := 0; i < PreambleSamples; i++ {
			idx := int(math.Mod(SyncHz*float64(i)/SampleRate*1024, 1024))
			add(out, base+i, amp*4*cosTable[idx])
		}
		bits := payloadBits(Payload{AssetTag: assetTag, Frame: uint16(f), Version: FormatVersion})
		bits = append(bits, bits...) // 两份副本
		for s := 0; s < SymbolCount; s++ {
			off := base + PreambleSamples + s*SymbolSamples
			for c, freq := range carriers {
				bitIdx := (s*len(carriers) + c) * 2
				phase := qpskPhase(bits[bitIdx], bits[bitIdx+1])
				for i := 0; i < SymbolSamples; i++ {
					angle := 2*math.Pi*freq*float64(i)/SampleRate + phase
					idx := int(math.Mod(angle/(2*math.Pi)*1024, 1024))
					add(out, off+i, amp*cosTable[idx])
				}
			}
		}
	}
	return out
}

func add(pcm []int16, i int, v float64) {
	if i < 0 || i >= len(pcm) {
		return
	}
	f := float64(pcm[i]) + v
	if f > 32767 {
		f = 32767
	}
	if f < -32768 {
		f = -32768
	}
	pcm[i] = int16(math.Round(f))
}

// syncEnergy 计算同步音的滑动能量，用于定位帧边界候选。
func syncEnergy(pcm []int16) []float64 {
	n := len(pcm)
	out := make([]float64, n)
	if n < PreambleSamples {
		return out
	}
	// 复数混频后做滑动和：用正弦/余弦两个累加器。
	re := make([]float64, n+1)
	im := make([]float64, n+1)
	for i := 0; i < n; i++ {
		angle := 2 * math.Pi * SyncHz * float64(i) / SampleRate
		v := float64(pcm[i])
		re[i+1] = re[i] + v*cosTable[int(math.Mod(angle/(2*math.Pi)*1024, 1024))]
		im[i+1] = im[i] - v*cosTable[int(math.Mod((angle+math.Pi/2)/(2*math.Pi)*1024, 1024))]
	}
	for i := 0; i+PreambleSamples < n; i++ {
		dr := re[i+PreambleSamples] - re[i]
		di := im[i+PreambleSamples] - im[i]
		out[i] = dr*dr + di*di
	}
	return out
}

func decodeAt(pcm []int16, start int) (Payload, bool) {
	if start < 0 || start+FrameSamples > len(pcm) {
		return Payload{}, false
	}
	bits := make([]byte, 0, 96)
	for s := 0; s < SymbolCount; s++ {
		off := start + PreambleSamples + s*SymbolSamples
		for _, freq := range carriers {
			var re, im float64
			for i := 0; i < SymbolSamples; i++ {
				angle := 2 * math.Pi * freq * float64(i) / SampleRate
				idx := int(math.Mod(angle/(2*math.Pi)*1024, 1024))
				c := cosTable[idx]
				sn := cosTable[(idx+256)%1024]
				v := float64(pcm[off+i])
				re += v * c
				im += v * sn
			}
			b0, b1 := phaseQuadrant(math.Atan2(im, re))
			bits = append(bits, b0, b1)
		}
	}
	if p, ok := bitsPayload(bits[:48]); ok {
		return p, true
	}
	return bitsPayload(bits[48:])
}

// Decode 在任意位置起播的音频中读取位置码。
// 先用同步音能量给出候选，再用"连续两帧帧号连号"确认对齐，失败时扫描一个完整帧周期。
func Decode(pcm []int16) (Payload, bool) {
	if len(pcm) < FrameSamples*2 {
		return Payload{}, false
	}
	energy := syncEnergy(pcm)
	// 候选优先：同步音能量最高的若干位置通常包含真实帧边界。
	for _, limit := range []int{30, 128} {
		for _, off := range topCandidates(energy, len(pcm), limit) {
			if p, ok := confirm(pcm, off); ok {
				return p, true
			}
		}
	}
	// 兜底：按 8 采样步长扫一个完整帧周期，只要求连续两帧，命中后再放宽跨度。
	for off := 0; off < FrameSamples; off += 8 {
		if p, ok := confirmSpan(pcm, off, 2); ok {
			return p, true
		}
	}
	for off := 0; off < FrameSamples; off += 8 {
		if p, ok := confirmSpan(pcm, off, 8); ok {
			return p, true
		}
	}
	return Payload{}, false
}

func confirm(pcm []int16, off int) (Payload, bool) { return confirmSpan(pcm, off, 8) }

// confirmSpan 在指定跨度内寻找"连续两帧帧号连号"的对齐。
func confirmSpan(pcm []int16, off, span int) (Payload, bool) {
	for k := 0; k < span; k++ {
		pos := off + k*FrameSamples
		if pos+FrameSamples > len(pcm) {
			break
		}
		a, okA := decodeAt(pcm, pos)
		if !okA {
			continue
		}
		b, okB := decodeAt(pcm, pos+FrameSamples)
		if okB && b.Frame == a.Frame+1 {
			return a, true
		}
	}
	return Payload{}, false
}

type syncCand struct {
	idx int
	v   float64
}

func topCandidates(energy []float64, n, limit int) []int {
	best := make([]syncCand, 0, limit)
	for i := 0; i+PreambleSamples+2*FrameSamples < n; i++ {
		best = append(best, syncCand{i, energy[i]})
	}
	sort.Slice(best, func(a, b int) bool { return best[a].v > best[b].v })
	if len(best) > limit {
		best = best[:limit]
	}
	out := make([]int, 0, len(best))
	for _, c := range best {
		out = append(out, c.idx)
	}
	return out
}

// Validate 检查参数是否可用。
func Validate(pcm []int16) error {
	if len(pcm) < FrameSamples {
		return errors.New("音频不足一帧，无法嵌入位置码")
	}
	return nil
}






