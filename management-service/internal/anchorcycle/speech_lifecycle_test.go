package anchorcycle

import (
	"testing"
	"time"
)

func TestSpeechLifecycleDefaults(t *testing.T) {
	s := &State{Config: Config{}}
	if !s.SpeechReuseEnabled() {
		t.Fatal("字段为 0 时应用默认值并启用复用")
	}
	if s.freshWindowMinutes() != 60 || s.reuseEveryN() != 3 || s.maxReuse() != 3 {
		t.Fatalf("默认值错误: %d %d %d", s.freshWindowMinutes(), s.reuseEveryN(), s.maxReuse())
	}
}

func TestSpeechReuseDisabledByNegative(t *testing.T) {
	if (&State{Config: Config{FreshWindowMinutes: -1}}).SpeechReuseEnabled() {
		t.Fatal("负值应禁用复用")
	}
	if (&State{Config: Config{MaxReuse: -1}}).SpeechReuseEnabled() {
		t.Fatal("负值应禁用复用")
	}
}

func TestShouldReuseFreshWindow(t *testing.T) {
	s := &State{Config: Config{}, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	s.Accepted = []Segment{{}, {}}
	if s.ShouldReuse() {
		t.Fatal("新鲜期内不应复用")
	}
}

func TestShouldReuseAlternate(t *testing.T) {
	s := &State{Config: Config{}, CreatedAt: time.Now().Add(-61 * time.Minute).UTC().Format(time.RFC3339Nano)}
	for i := 1; i <= 9; i++ {
		s.Accepted = make([]Segment, i)
		want := i%3 != 0
		if s.ShouldReuse() != want {
			t.Fatalf("seq=%d ShouldReuse=%v want=%v", i, s.ShouldReuse(), want)
		}
	}
}

func TestAddAndPickReuse(t *testing.T) {
	s := &State{Config: Config{CombinationGap: 3, PointGap: 3}}
	s.Accepted = append(s.Accepted, Segment{})
	s.AddToSpeechPool("话术A", "口感×肉紧")
	s.Accepted = append(s.Accepted, Segment{})
	s.AddToSpeechPool("话术B", "规格×价格")
	// 推进 Accepted，让话术A 距离超过 gap（防连续重复）
	for i := 0; i < 5; i++ {
		s.Accepted = append(s.Accepted, Segment{})
	}
	if e := s.PickReuse("口感×肉紧"); e == nil || e.Text != "话术A" {
		t.Fatal("应复用同 Key 的话术A")
	}
	if e := s.PickReuse("不存在"); e != nil {
		t.Fatal("不同 Key 不应复用")
	}
	s.AddToSpeechPool("话术A", "口感×肉紧")
	if len(s.SpeechPool) != 2 {
		t.Fatalf("去重失败: %d", len(s.SpeechPool))
	}
}

func TestMaxReuseEviction(t *testing.T) {
	s := &State{Config: Config{}}
	s.Accepted = append(s.Accepted, Segment{})
	s.AddToSpeechPool("话术", "口感×肉紧")
	for i := 0; i < 5; i++ {
		s.Accepted = append(s.Accepted, Segment{})
	}
	e := s.PickReuse("口感×肉紧")
	if e == nil {
		t.Fatal("应能取到")
	}
	for i := 0; i < 3; i++ {
		s.MarkSpeechReused(e)
	}
	if s.PickReuse("口感×肉紧") != nil {
		t.Fatal("复用达上限应淘汰")
	}
}

func TestIsDuplicateSpeechNormalized(t *testing.T) {
	s := &State{}
	s.AddToSpeechPool("买鸭子最怕啥？怕买到贴牌的、假的！啊", "品牌×正品")
	if !s.IsDuplicateSpeech("买鸭子最怕啥?怕买到贴牌的 假的 啊") {
		t.Fatal("归一化后标点/空白差异应判为重复")
	}
	if s.IsDuplicateSpeech("这鸭子口感酥脆爆汁") {
		t.Fatal("不同内容不应判为重复")
	}
	if s.IsDuplicateSpeech("") {
		t.Fatal("空文本不应判为重复")
	}
}

func TestRotateAfterDuplicate(t *testing.T) {
	s := &State{Config: Config{}}
	s.Queue = []Task{
		{SellingPoint: "口感", Key: "口感×A"},
		{SellingPoint: "工艺", Key: "工艺×B"},
		{SellingPoint: "规格", Key: "规格×C"},
	}
	s.TaskIndex = 0
	s.Cursor = 0
	if !s.RotateAfterDuplicate() {
		t.Fatal("应能换到下一个角度")
	}
	if s.Task().Key == "口感×A" {
		t.Fatal("复读后不应停留在原角度")
	}
	// 连续跳过所有角度后应返回 false
	s.DuplicateSkippedKeys["工艺×B"] = true
	s.DuplicateSkippedKeys["规格×C"] = true
	s.TaskIndex = 0
	s.Cursor = 0
	s.DuplicateSkippedKeys["口感×A"] = true
	if s.RotateAfterDuplicate() {
		t.Fatal("池轮尽后应返回 false")
	}
}

func TestExpressionVariantRotates(t *testing.T) {
	s := &State{Config: Config{}}
	s.Queue = []Task{{SellingPoint: "口感", Key: "口感×A"}}
	s.TaskIndex = 0
	s.Cursor = 0
	first := s.ExpressionVariant()
	if first == "" {
		t.Fatal("变体不应为空")
	}
	// 每采纳一段，表达变体应轮换（不同段数 → 不同包装/长度）
	s.Accepted = append(s.Accepted, Segment{})
	if s.ExpressionVariant() == first {
		t.Fatal("不同段数应产生不同包装/长度变体")
	}
	// 一轮（6 段，6 种包装 3 种长度的最小公倍数）后回到起点
	for i := 0; i < 5; i++ {
		s.Accepted = append(s.Accepted, Segment{})
	}
	if s.ExpressionVariant() != first {
		t.Fatal("6 段后应回到初始包装/长度")
	}
}
