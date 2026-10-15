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

func TestFactSidesMapping(t *testing.T) {
	cases := []struct {
		category string
		want     []string
	}{
		{"flavor", []string{"对比参照", "使用场景"}},
		{"craft", []string{"过程细节"}},
		{"price", []string{"利益算账", "对比参照"}},
		{"usage", []string{"使用方式"}},
		{"other", nil},
		{"", nil},
		{"UNKNOWN", nil},
	}
	for _, c := range cases {
		got := FactSides(c.category)
		if len(got) != len(c.want) {
			t.Fatalf("FactSides(%q) len=%d want %d (%v)", c.category, len(got), len(c.want), got)
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Fatalf("FactSides(%q)[%d]=%q want %q", c.category, i, got[i], c.want[i])
			}
		}
	}
}

func TestFactMatchesSide(t *testing.T) {
	cases := []struct {
		category, side string
		want           bool
	}{
		{"craft", "过程细节", true},
		{"craft", "利益算账", false},
		{"flavor", "对比参照", true},
		{"flavor", "使用场景", true},
		{"flavor", "人群痛点", false},
		{"other", "使用方式", true}, // 通用事实任何侧面都算匹配
		{"", "使用方式", true},      // 空分类同通用
		{"unknown", "品质来源", true},
	}
	for _, c := range cases {
		if got := FactMatchesSide(c.category, c.side); got != c.want {
			t.Fatalf("FactMatchesSide(%q,%q)=%v want %v", c.category, c.side, got, c.want)
		}
	}
}

func TestCategoryFromFactText(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"- [product/常温保存期限] 常温保存期限为：抽真空6天，不抽真空3天。", "product"},
		{"- [fulfillment/发货时效] 一般是下单后第二天上午发货。；安全改写=咱们", "fulfillment"},
		{"- [other/每日现做现发] 每天新鲜制作，现卤现炸。", "other"},
		{"- [有效福利] 福利=1:current-benefit02；链接=1、2、3", ""}, // 无 / 标记
		{"开袋可以直接吃。", ""},                                  // 无标记
		{"", ""},
	}
	for _, c := range cases {
		if got := CategoryFromFactText(c.text); got != c.want {
			t.Fatalf("CategoryFromFactText(%q)=%q want %q", c.text, got, c.want)
		}
	}
}

func TestFissionSideName(t *testing.T) {
	cases := []struct {
		side string
		want string
	}{
		{"使用场景：什么场合、谁用得上", "使用场景"},
		{"过程细节：怎么做出来", "过程细节"},
		{"使用方式", "使用方式"},
		{"", ""},
	}
	for _, c := range cases {
		if got := FissionSideName(c.side); got != c.want {
			t.Fatalf("FissionSideName(%q)=%q want %q", c.side, got, c.want)
		}
	}
}

func TestHookFactsForSideFiltersBySide(t *testing.T) {
	s := &State{Config: Config{Rolling: true}}
	s.Queue = []Task{{Key: "K", SellingPoint: "S", Angle: "A"}}
	s.TaskIndex = 0
	s.Facts = "- [price/线下门店价格] 线下门店价格为95.6元。\n" +
		"- [craft/工艺] 老卤汤慢炖4小时。\n" +
		"- [other/适用场景] 当零食直接吃和搭配主食当下酒菜都适合。"

	countData := func() map[string]bool {
		m := map[string]bool{}
		for _, f := range s.HookFactsForSide() {
			m[CategoryFromFactText(f.Data)] = true
		}
		return m
	}

	// 侧面 3 = 利益算账：price 匹配，craft 不匹配，other 通用兜底
	s.Used = map[string]Use{"K": {Count: 3}}
	got := countData()
	if !got["price"] || !got["other"] {
		t.Fatalf("利益算账侧面应含 price+other，got %v", got)
	}
	if got["craft"] {
		t.Fatalf("利益算账侧面不应含 craft，got %v", got)
	}

	// 侧面 0 = 使用场景：price/craft 都不匹配，只有 other 通用兜底
	s.Used = map[string]Use{"K": {Count: 0}}
	got = countData()
	if !got["other"] || got["price"] || got["craft"] {
		t.Fatalf("使用场景侧面应只含 other，got %v", got)
	}
}

func TestRecentSameTask(t *testing.T) {
	// 近期含当前角度 → 复讲
	s := &State{}
	s.Queue = []Task{{Key: "保存×真空"}}
	s.TaskIndex = 0
	s.Accepted = []Segment{{Task: Task{Key: "口感×肉紧"}}, {Task: Task{Key: "保存×真空"}}}
	if !s.RecentSameTask(2) {
		t.Fatal("近2段内讲过同一角度，应判定为复讲")
	}

	// 近期不含当前角度 → 首次讲
	s2 := &State{}
	s2.Queue = []Task{{Key: "保存×真空"}}
	s2.TaskIndex = 0
	s2.Accepted = []Segment{{Task: Task{Key: "口感×肉紧"}}, {Task: Task{Key: "物流×隔天"}}}
	if s2.RecentSameTask(2) {
		t.Fatal("近2段内未讲同一角度，不应判定为复讲")
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
