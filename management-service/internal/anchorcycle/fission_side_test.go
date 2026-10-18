package anchorcycle

import (
	"encoding/json"
	"testing"
)

// 切入侧面按「卖点×角度」任务自身的使用次数轮换：同一任务连续讲，侧面逐次轮换，
// 第 8 次回到首个侧面（供声音缓存复用）。这是"对观众多样、对服务器省 TTS"的关键。
// 现在侧面只从「有专属事实支撑」的侧面里轮换，测试需给 State 配齐全分类事实。
func TestCurrentFissionSideRotation(t *testing.T) {
	s := &State{Config: Config{Rolling: true}}
	s.Queue = []Task{{Key: "口感×肉紧", SellingPoint: "口感", Angle: "肉紧"}}
	s.TaskIndex = 0
	// 给每个侧面都配一条专属事实，保证 7 个侧面全部有支撑，轮换机制得以完整验证。
	s.Facts = "" +
		"- [product/规格] 规格事实\n" + // 过程细节/品质来源
		"- [usage/吃法] 吃法事实\n" + // 使用方式
		"- [price/价格] 价格事实\n" + // 利益算账/对比参照
		"- [flavor/口味] 口味事实\n" + // 对比参照/使用场景
		"- [after_sales/售后] 售后事实\n" + // 人群痛点
		"- [origin/产地] 产地事实\n" // 品质来源
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
	s.Facts = "" +
		"- [product/规格] 规格\n" +
		"- [usage/吃法] 吃法\n" +
		"- [price/价格] 价格\n" +
		"- [flavor/口味] 口味\n" +
		"- [after_sales/售后] 售后\n" +
		"- [origin/产地] 产地\n"
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
		{"开袋可以直接吃。", ""}, // 无标记
		{"", ""},
		{"- [用户指定公共；appointment/预约] 无需预约", "appointment"},
		{`- [用户指定商品="2号链接"；audience/人数] 适合分享`, "audience"},
		{`- [用户指定商品="套餐/双人；节日]\\\"款"；package/内容] 套餐内容`, "package"},
		{"- [用户指定公共； price /价格] 价格已确认", "price"},
		{"- [商品卡] 链接=1号链接", ""},
		{"- [商品卡] 说明 [用户指定公共；location/停车] 免费停车", "location"},
		{"事实正文提到用户指定公共；[location/停车] 免费", "location"},
		{`- [用户指定商品="2号链接"audience/人数] 缺分号`, ""},
		{`- [用户指定商品=""；audience/人数] 空链接`, ""},
		{`- [用户指定商品=2；audience/人数] 非字符串`, ""},
		{`- [用户指定商品="未闭合；audience/人数]`, ""},
		{`- [用户指定商品="2号链接"；audience/人数 未闭合标题`, ""},
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
	// 只配「利益算账」一个侧面，直接验证它筛出 price 专属 + other 通用，滤掉 craft。
	s.FissionSides = []string{"利益算账：划不划算"}
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

	got := countData()
	if !got["price"] || !got["other"] {
		t.Fatalf("利益算账侧面应含 price+other，got %v", got)
	}
	if got["craft"] {
		t.Fatalf("利益算账侧面不应含 craft，got %v", got)
	}
}

// 核心回归：调度选「特色菜」角度（只有 flavor/craft/origin 事实），不能轮换到
// 「利益算账」把主体事实滤光只剩价格。必须回退全量，让模型拿到完整素材。
func TestCurrentFissionSideFallsBackWhenNoDedicatedFact(t *testing.T) {
	s := &State{Config: Config{Rolling: true}}
	s.Queue = []Task{{Key: "特色菜×招牌", SellingPoint: "特色菜", Angle: "招牌"}}
	s.TaskIndex = 0
	// 只有 flavor/craft/origin，没有任何 price/trade 事实。
	s.Facts = "- [flavor/口味] 麻辣五香。\n- [craft/工艺] 老卤慢炖。\n- [origin/产地] 川渝。"
	s.Used = map[string]Use{"特色菜×招牌": {Count: 2}} // 轮换到「利益算账」的位置
	side := s.CurrentFissionSide()
	// 利益算账无 price/trade 支撑，不应被选中；应落在有支撑的侧面或回退空。
	if side != "" && FissionSideName(side) == "利益算账" {
		t.Fatalf("特色菜角度不应选到利益算账，got %q", side)
	}
	// 无论如何，最终喂给生成的事实不能只剩价格——这里根本没有价格事实，也不能是空。
	got := s.HookFactsForSide()
	if len(got) == 0 {
		t.Fatal("无价格事实时筛出空素材，应回退全量")
	}
}

// 只有通用事实（无任何分类专属事实）时，任何侧面都无专属支撑，应返回空串回退全量，
// 而不是硬套一个侧面。
func TestCurrentFissionSideNoSideWhenOnlyGenericFacts(t *testing.T) {
	s := &State{Config: Config{Rolling: true}}
	s.Queue = []Task{{Key: "K"}}
	s.TaskIndex = 0
	s.Facts = "- [other/通用] 通用信息。\n- 无标记事实。"
	s.Used = map[string]Use{"K": {Count: 0}}
	if got := s.CurrentFissionSide(); got != "" {
		t.Fatalf("只有通用事实时不应选侧面，got %q", got)
	}
	if got := s.HookFactsForSide(); len(got) != 2 {
		t.Fatalf("应回退全量事实，got %d 条", len(got))
	}
}

func TestHookFactsForSideScopedIndustryFactsKeepProductIsolation(t *testing.T) {
	for _, bound := range []bool{false, true} {
		t.Run(map[bool]string{false: "product-focus", true: "task-scope"}[bound], func(t *testing.T) {
			s := &State{Queue: []Task{{Key: "K", FocusLink: "1", AllowedLinksJSON: `["1"]`}}, FissionSides: []string{"利益算账：价格与人均"}, FactSideMap: map[string][]string{
				"price": {"利益算账"}, "package": {"利益算账"}, "appointment": {"预约核销"}, "location": {"地理位置"},
			}}
			facts := []ScopedFact{
				{ID: "own", Text: `- [用户指定商品="1"；price/价格] 本商品价格`, Links: []string{"1"}, Applicable: true},
				{ID: "package", Text: `- [用户指定商品="1"；package/内容] 本商品套餐`, Links: []string{"1"}, Applicable: true},
				{ID: "other", Text: `- [用户指定商品="2"；price/价格] 其它商品价格`, Links: []string{"2"}, Applicable: true},
				{ID: "booking", Text: "- [用户指定公共；appointment/预约] 不用预约", Links: []string{"1", "2"}, Shared: true},
				{ID: "parking", Text: "- [用户指定公共；location/停车] 免费停车", Links: []string{"1", "2"}, Shared: true},
				{ID: "unknown", Text: "- [用户指定公共；custom/自定义] 通用信息", Links: []string{"1", "2"}, Shared: true},
			}
			if bound {
				scope := &TaskFactScope{}
				for _, f := range facts {
					scope.Facts = append(scope.Facts, HookFact{ID: f.ID, Data: f.Text, Products: f.Links})
				}
				s.Queue[0].FactScope = scope
			} else {
				s.ProductFocus = &ProductFocus{Products: []FocusProduct{{LinkKey: "1"}, {LinkKey: "2"}}, Facts: facts}
			}
			sourceFacts := func() any {
				if bound {
					return s.Queue[0].FactScope
				}
				return s.ProductFocus.Facts
			}
			before, _ := json.Marshal(sourceFacts())
			assertIDs := func(want ...string) {
				t.Helper()
				got := s.HookFactsForSide()
				if len(got) != len(want) {
					t.Fatalf("side %s: got %v, want %v", s.CurrentFissionSide(), got, want)
				}
				for i, id := range want {
					if got[i].ID != id {
						t.Fatalf("got %v, want %v", got, want)
					}
				}
			}
			assertIDs("own", "package", "unknown")
			s.FissionSides = []string{"预约核销"}
			assertIDs("booking", "unknown")
			s.FissionSides = []string{"地理位置"}
			assertIDs("parking", "unknown")
			s.FissionSides = []string{"利益算账：价格与人均"}
			after, _ := json.Marshal(sourceFacts())
			if string(before) != string(after) {
				t.Fatal("fact filtering mutated source state")
			}
		})
	}
}

func TestHookFactsForSidePreservesEmptyFallback(t *testing.T) {
	s := &State{Queue: []Task{{Key: "K"}}, FissionSides: []string{"利益算账"}, Facts: "- [用户指定公共；appointment/预约] 无需预约", FactSideMap: map[string][]string{"appointment": {"预约核销"}}}
	if len(s.HookFactsForSide()) != 1 {
		t.Fatal("existing empty-side fallback lost")
	}
	s.FissionSides = nil
	if len(s.HookFactsForSide()) != 1 {
		t.Fatal("legacy no-side fallback lost")
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
