package audiocuts

import (
	"math"
	"math/rand"
	"testing"
)

const testRate = 24000

// synth 生成"噪声段 + 静音段"交替的测试音频。
// bursts 为每段噪声时长（毫秒），gaps 为每段静音时长（毫秒）。
func synth(bursts, gaps []int, amp float64) ([]int16, []int) {
	var out []int16
	var silenceCenters []int
	pos := 0
	for i := 0; i < len(bursts); i++ {
		n := testRate * bursts[i] / 1000
		for j := 0; j < n; j++ {
			out = append(out, int16(amp*math.Sin(2*math.Pi*220*float64(pos+j)/testRate)))
		}
		pos += n
		if i < len(gaps) {
			g := testRate * gaps[i] / 1000
			for j := 0; j < g; j++ {
				out = append(out, 0)
			}
			silenceCenters = append(silenceCenters, (pos+g/2)*1000/testRate)
			pos += g
		}
	}
	return out, silenceCenters
}

func TestScanFindsSilenceBetweenBursts(t *testing.T) {
	bursts := []int{800, 800, 800, 800, 800}
	gaps := []int{300, 300, 300, 300, 300}
	pcm, centers := synth(bursts, gaps, 3000)

	points, err := Scan(pcm, testRate)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if len(points) < len(centers)-1 {
		t.Fatalf("切点太少: 得到 %d 个，静音段 %d 个", len(points), len(centers))
	}
	for _, p := range points {
		nearest := 1 << 30
		for _, c := range centers {
			if d := absInt(int(p.MS) - c); d < nearest {
				nearest = d
			}
		}
		if nearest > 160 {
			t.Errorf("切点 %d 毫秒离静音中心太远（%d 毫秒）", p.MS, nearest)
		}
	}
}

func TestScanSnapsToZeroCrossing(t *testing.T) {
	pcm, _ := synth([]int{500, 500, 500}, []int{300, 300, 300}, 4000)
	points, err := Scan(pcm, testRate)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("没有扫出切点")
	}
	for _, p := range points {
		i := int(p.SampleOffset)
		if i < 0 || i+1 >= len(pcm) {
			t.Fatalf("切点采样位置越界: %d", i)
		}
		a, b := pcm[i], pcm[i+1]
		if !(a == 0 || (a > 0) != (b > 0)) {
			t.Errorf("切点 %d 未落在过零点（%d, %d）", p.MS, a, b)
		}
	}
}

func TestScanRejectsPureSilence(t *testing.T) {
	pcm := make([]int16, testRate)
	if _, err := Scan(pcm, testRate); err == nil {
		t.Fatal("全静音应返回错误")
	}
}

func TestScanRejectsTooShort(t *testing.T) {
	if _, err := Scan(make([]int16, 3), testRate); err == nil {
		t.Fatal("过短音频应返回错误")
	}
}

func TestSelectKeepsBucketsAndMinGap(t *testing.T) {
	pts := []Point{
		{MS: 1000, RMS: 40}, {MS: 2000, RMS: 5},
		{MS: 11000, RMS: 30}, {MS: 12000, RMS: 3},
		{MS: 12500, RMS: 1},
	}
	got := Select(pts, 10000, 2500)
	if len(got) != 2 {
		t.Fatalf("期望每桶一个共 2 个，得到 %d 个: %+v", len(got), got)
	}
	if got[0].MS != 2000 || got[1].MS != 12500 {
		t.Fatalf("桶内应取 RMS 最低: %+v", got)
	}
	if gap := int(got[1].MS - got[0].MS); gap < 2500 {
		t.Fatalf("最小间隔未生效: %d", gap)
	}
}

func TestSelectForPlaybackDropsEdges(t *testing.T) {
	pts := []Point{
		{MS: 0, RMS: 1}, {MS: 5000, RMS: 2}, {MS: 99000, RMS: 1}, {MS: 100000, RMS: 1},
	}
	got := SelectForPlayback(pts, 100000)
	for _, p := range got {
		if p.MS == 0 || p.MS >= 99000 {
			t.Fatalf("首尾边界不应作为切点: %+v", p)
		}
	}
}

func TestNextCutPrefersA(t *testing.T) {
	pts := []Point{
		{MS: 1000, Grade: GradeB, RMS: 50},
		{MS: 4000, Grade: GradeA, RMS: 5},
	}
	got, ok := NextCut(pts, 0, 30000)
	if !ok || got.MS != 4000 {
		t.Fatalf("应优先 A 类: %+v ok=%v", got, ok)
	}
}

func TestNextCutFallsBackToB(t *testing.T) {
	pts := []Point{{MS: 1000, Grade: GradeB, RMS: 50}}
	got, ok := NextCut(pts, 0, 30000)
	if !ok || got.MS != 1000 {
		t.Fatalf("无 A 时应退回 B: %+v ok=%v", got, ok)
	}
}

func TestNextCutRespectsDeadline(t *testing.T) {
	pts := []Point{{MS: 40000, Grade: GradeA}}
	if _, ok := NextCut(pts, 0, 30000); ok {
		t.Fatal("超出窗口不应返回切点")
	}
}

func TestScanOnRealisticAudioStaysStable(t *testing.T) {
	// 模拟连续说话：少量静音，中间是随机噪声
	rng := rand.New(rand.NewSource(7))
	pcm := make([]int16, 0, testRate*6)
	for i := 0; i < testRate*6; i++ {
		v := 0.0
		if (i/testRate)%2 == 0 {
			v = (rng.Float64()*2 - 1) * 2000
		}
		pcm = append(pcm, int16(v))
	}
	points, err := Scan(pcm, testRate)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	for _, p := range points {
		if p.SampleOffset < 0 || int(p.SampleOffset) >= len(pcm) {
			t.Fatalf("切点越界: %+v", p)
		}
		if p.Grade != GradeA && p.Grade != GradeB {
			t.Fatalf("等级无效: %+v", p)
		}
	}
}
