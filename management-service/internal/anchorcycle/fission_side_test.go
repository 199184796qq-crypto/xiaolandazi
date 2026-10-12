package anchorcycle

import "testing"

// 切入侧面按「卖点×角度」任务自身的使用次数轮换：同一任务连续讲，侧面逐次轮换，
// 第 8 次回到首个侧面（供声音缓存复用）。这是"对观众多样、对服务器省 TTS"的关键。
func TestCurrentFissionSideRotation(t *testing.T) {
	s := &State{Config: Config{Rolling: true}}
	s.Queue = []Task{{Key: "口感×肉紧", SellingPoint: "口感", Angle: "肉紧"}}
	s.TaskIndex = 0
	for i := 0; i < len(fissionAngleSides); i++ {
		s.Used = map[string]Use{"口感×肉紧": {Count: i}}
		if got := s.CurrentFissionSide(); got != fissionAngleSides[i] {
			t.Fatalf("Count=%d 应得侧面 %q，got %q", i, fissionAngleSides[i], got)
		}
	}
	// 第 8 次（Count=7）回到首个侧面，让声音缓存能复用第 1 次的音频。
	s.Used = map[string]Use{"口感×肉紧": {Count: len(fissionAngleSides)}}
	if got := s.CurrentFissionSide(); got != fissionAngleSides[0] {
		t.Fatalf("第 8 次应回到首个侧面，got %q", got)
	}
}

// 不同任务（卖点×角度）独立轮换：各自从侧面 0 开始，互不干扰。
func TestCurrentFissionSideIndependentPerTask(t *testing.T) {
	s := &State{Config: Config{Rolling: true}}
	s.Queue = []Task{{Key: "口感×肉紧"}, {Key: "算账×实惠"}}
	s.Used = map[string]Use{"口感×肉紧": {Count: 2}, "算账×实惠": {Count: 0}}
	s.TaskIndex = 0
	if got := s.CurrentFissionSide(); got != fissionAngleSides[2] {
		t.Fatalf("口感(Count=2)应得侧面 %q，got %q", fissionAngleSides[2], got)
	}
	s.TaskIndex = 1
	if got := s.CurrentFissionSide(); got != fissionAngleSides[0] {
		t.Fatalf("算账(Count=0)应得侧面 %q，got %q", fissionAngleSides[0], got)
	}
}

// 侧面池产品无关：不含商品词、数字、承诺词。
func TestFissionSidesAreProductAgnostic(t *testing.T) {
	for i, side := range fissionAngleSides {
		if side == "" {
			t.Fatalf("侧面 %d 为空", i)
		}
		for _, banned := range []string{"杨鸭子", "菜籽油", "鸭", "油", "价格", "元", "块"} {
			if containsStr(side, banned) {
				t.Fatalf("侧面 %d 含产品相关词 %q：%s", i, banned, side)
			}
		}
	}
}

func containsStr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
