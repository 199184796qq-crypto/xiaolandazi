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
