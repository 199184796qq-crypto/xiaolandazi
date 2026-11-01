// Package audiocuts 从 PCM 音频中扫描可用于互动插播的声学切点。
//
// 设计要点：
//   - 以 1 毫秒能量包络为判据，不用文字标点（标点不等于声学停顿）
//   - 按全篇平均 RMS 的相对阈值分级，换音色/语速不需要重调
//   - 切点吸附到过零点，消爆音
//   - 分桶取桶内最低，避免"取第一个"捡到凑数的位置
//
// 切点必须在任何改变采样内容的处理（变速、底噪、位置码）之前计算。
package audiocuts

import (
	"errors"
	"math"
	"sort"
)

// Grade 表示切点质量等级。
const (
	GradeA byte = 1 // 深静音，可直接切
	GradeB byte = 2 // 相对低谷，A 不可用时的兜底
)

// 默认参数，取值依据见 docs/2026-11-01-互动立即打断设计.md。
const (
	DefaultGradeAPercent = 0.04  // A 类阈值：全篇平均 RMS 的 4%
	DefaultGradeBPercent = 0.15  // B 类阈值：15%
	DefaultClusterMS     = 250   // 同一停顿内的点合并
	DefaultBucketMS      = 10000 // 选点桶宽，等于期望的切点间隔
	DefaultMinGapMS      = 2500  // 相邻切点最小间隔
	DefaultSnapMS        = 5     // 过零点吸附搜索半径
)

// Point 是一个候选切点。
type Point struct {
	MS           int32   `json:"ms"`            // 毫秒位置，仅用于展示与近似查询
	SampleOffset int64   `json:"sample_offset"` // 采样位置，实际切分以它为准
	RMS          float64 `json:"rms"`           // 该点 1 毫秒窗口的能量
	Grade        byte    `json:"grade"`         // GradeA / GradeB
	Snapped      bool    `json:"snapped"`       // 是否成功吸附到过零点
}

// Options 控制扫描与选点。
type Options struct {
	GradeAPercent float64
	GradeBPercent float64
	ClusterMS     int
	BucketMS      int
	MinGapMS      int
	SnapMS        int
}

// DefaultOptions 返回实测调好的默认参数。
func DefaultOptions() Options {
	return Options{
		GradeAPercent: DefaultGradeAPercent,
		GradeBPercent: DefaultGradeBPercent,
		ClusterMS:     DefaultClusterMS,
		BucketMS:      DefaultBucketMS,
		MinGapMS:      DefaultMinGapMS,
		SnapMS:        DefaultSnapMS,
	}
}

func (o Options) normalized() Options {
	if o.GradeAPercent <= 0 || o.GradeAPercent >= 1 {
		o.GradeAPercent = DefaultGradeAPercent
	}
	if o.GradeBPercent <= o.GradeAPercent || o.GradeBPercent >= 1 {
		o.GradeBPercent = DefaultGradeBPercent
	}
	if o.ClusterMS <= 0 {
		o.ClusterMS = DefaultClusterMS
	}
	if o.BucketMS <= 0 {
		o.BucketMS = DefaultBucketMS
	}
	if o.MinGapMS < 0 {
		o.MinGapMS = DefaultMinGapMS
	}
	if o.SnapMS <= 0 {
		o.SnapMS = DefaultSnapMS
	}
	return o
}

// Envelope 按 1 毫秒分帧计算 RMS 包络。
func Envelope(pcm []int16, sampleRate int) ([]float64, int, error) {
	if sampleRate <= 0 {
		return nil, 0, errors.New("采样率无效")
	}
	frame := sampleRate / 1000
	if frame <= 0 {
		return nil, 0, errors.New("采样率过低，无法按毫秒分帧")
	}
	frames := len(pcm) / frame
	if frames == 0 {
		return nil, 0, errors.New("音频长度不足 1 毫秒")
	}
	env := make([]float64, frames)
	for i := 0; i < frames; i++ {
		var sum float64
		base := i * frame
		for j := 0; j < frame; j++ {
			v := float64(pcm[base+j])
			sum += v * v
		}
		env[i] = math.Sqrt(sum / float64(frame))
	}
	return env, frame, nil
}

// MeanRMS 返回整段音频的均方根，作为相对阈值的基准。
func MeanRMS(pcm []int16) float64 {
	if len(pcm) == 0 {
		return 0
	}
	var sum float64
	for _, v := range pcm {
		f := float64(v)
		sum += f * f
	}
	return math.Sqrt(sum / float64(len(pcm)))
}

// Scan 扫描出全部候选切点，按时间升序返回。
func Scan(pcm []int16, sampleRate int) ([]Point, error) {
	return ScanWith(pcm, sampleRate, DefaultOptions())
}

// ScanWith 使用自定义参数扫描。
func ScanWith(pcm []int16, sampleRate int, opt Options) ([]Point, error) {
	opt = opt.normalized()
	env, frame, err := Envelope(pcm, sampleRate)
	if err != nil {
		return nil, err
	}
	mean := MeanRMS(pcm)
	if mean <= 0 {
		return nil, errors.New("音频全为静音，无法建立能量基准")
	}
	thA := mean * opt.GradeAPercent
	thB := mean * opt.GradeBPercent

	cluster := func(lo, hi float64, grade byte) []Point {
		var out []Point
		start := -1
		for i := 0; i <= len(env); i++ {
			in := i < len(env) && env[i] >= lo && env[i] < hi
			if in {
				if start < 0 {
					start = i
				}
				continue
			}
			if start >= 0 {
				best := start
				for k := start; k < i; k++ {
					if env[k] < env[best] {
						best = k
					}
				}
				out = append(out, Point{MS: int32(best), RMS: env[best], Grade: grade})
				start = -1
			}
		}
		return out
	}

	points := mergeClusters(cluster(0, thA, GradeA), opt.ClusterMS)
	bPoints := mergeClusters(cluster(thA, thB, GradeB), opt.ClusterMS)
	for _, b := range bPoints {
		close := false
		for _, a := range points {
			if absInt(int(a.MS)-int(b.MS)) < 1000 {
				close = true
				break
			}
		}
		if !close {
			points = append(points, b)
		}
	}

	for i := range points {
		points[i].SampleOffset = int64(points[i].MS) * int64(frame)
		snapToZeroCrossing(pcm, sampleRate, opt.SnapMS, &points[i])
	}
	sort.Slice(points, func(i, j int) bool { return points[i].SampleOffset < points[j].SampleOffset })
	return points, nil
}

func mergeClusters(points []Point, clusterMS int) []Point {
	if len(points) == 0 {
		return nil
	}
	out := []Point{points[0]}
	for _, p := range points[1:] {
		last := &out[len(out)-1]
		if int(p.MS)-int(last.MS) <= clusterMS {
			if p.RMS < last.RMS {
				*last = p
			}
			continue
		}
		out = append(out, p)
	}
	return out
}

func snapToZeroCrossing(pcm []int16, sampleRate, radiusMS int, p *Point) {
	center := int(p.SampleOffset)
	radius := sampleRate * radiusMS / 1000
	lo := center - radius
	if lo < 0 {
		lo = 0
	}
	hi := center + radius
	if hi >= len(pcm)-1 {
		hi = len(pcm) - 2
	}
	best := -1
	for i := lo; i <= hi; i++ {
		a, b := pcm[i], pcm[i+1]
		if a == 0 || (a > 0) != (b > 0) {
			if best < 0 || absInt(i-center) < absInt(best-center) {
				best = i
			}
		}
	}
	if best < 0 {
		return
	}
	p.SampleOffset = int64(best)
	p.MS = int32(best * 1000 / sampleRate)
	p.Snapped = true
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Select 按桶选点：每桶取 RMS 最低的候选，再按最小间隔去重。
func Select(points []Point, bucketMS, minGapMS int) []Point {
	if bucketMS <= 0 {
		bucketMS = DefaultBucketMS
	}
	if minGapMS < 0 {
		minGapMS = DefaultMinGapMS
	}
	if len(points) == 0 {
		return nil
	}
	lastMS := points[len(points)-1].MS
	var out []Point
	for start := 0; int32(start) <= lastMS; start += bucketMS {
		end := int32(start + bucketMS)
		best := -1
		for i, p := range points {
			if p.MS < int32(start) || p.MS >= end {
				continue
			}
			if best < 0 || p.RMS < points[best].RMS {
				best = i
			}
		}
		if best < 0 {
			continue
		}
		if len(out) > 0 {
			prev := &out[len(out)-1]
			gap := int(points[best].MS) - int(prev.MS)
			if gap < minGapMS {
				if points[best].RMS < prev.RMS {
					*prev = points[best]
				}
				continue
			}
		}
		out = append(out, points[best])
	}
	return out
}

// SelectForPlayback 选取段内可用切点：去掉首尾边界，按默认桶宽与间隔筛选。
func SelectForPlayback(points []Point, durationMS int32) []Point {
	inner := make([]Point, 0, len(points))
	for _, p := range points {
		if p.MS < 500 || (durationMS > 0 && p.MS >= durationMS-1000) {
			continue
		}
		inner = append(inner, p)
	}
	return Select(inner, DefaultBucketMS, DefaultMinGapMS)
}

// NextCut 在 [fromMS, deadlineMS] 内找最近的可用切点，A 优先，B 兜底。
// 返回零值表示窗口内没有可用切点。
func NextCut(points []Point, fromMS, deadlineMS int32) (Point, bool) {
	var bestB Point
	hasB := false
	for _, p := range points {
		if p.MS < fromMS {
			continue
		}
		if deadlineMS > 0 && p.MS > deadlineMS {
			break
		}
		if p.Grade == GradeA {
			return p, true
		}
		if !hasB || p.MS < bestB.MS {
			bestB, hasB = p, true
		}
	}
	if hasB {
		return bestB, true
	}
	return Point{}, false
}
