package anchorcycle

import "time"

const (
	defaultFreshWindowMinutes = 60 // 默认新鲜期 60 分钟
	defaultReuseEveryN        = 3  // 默认每 3 段 1 段实时、2 段复用（1:2）
	defaultMaxReuse           = 3  // 默认单段最多复用 3 次
)

// SpeechReuseEnabled 话术复用是否启用。任一字段为负值表示显式禁用；字段为 0 时用默认值。
func (s *State) SpeechReuseEnabled() bool {
	if s == nil {
		return false
	}
	return s.Config.FreshWindowMinutes >= 0 && s.Config.ReuseEveryN >= 0 && s.Config.MaxReuse >= 0
}

func (s *State) freshWindowMinutes() int {
	if s.Config.FreshWindowMinutes > 0 {
		return s.Config.FreshWindowMinutes
	}
	return defaultFreshWindowMinutes
}

func (s *State) reuseEveryN() int {
	if s.Config.ReuseEveryN > 1 {
		return s.Config.ReuseEveryN
	}
	return defaultReuseEveryN
}

func (s *State) maxReuse() int {
	if s.Config.MaxReuse > 0 {
		return s.Config.MaxReuse
	}
	return defaultMaxReuse
}

func (s *State) elapsedMinutes() int {
	t, err := time.Parse(time.RFC3339Nano, s.CreatedAt)
	if err != nil {
		return 0
	}
	return int(time.Since(t).Minutes())
}

// ShouldReuse 判断当前这一段是否复用旧话术。
// 新鲜期（前 freshWindowMinutes 分钟）全实时；混合期按 reuseEveryN 交替（每 N 段 1 段实时，其余复用）。
func (s *State) ShouldReuse() bool {
	if !s.SpeechReuseEnabled() {
		return false
	}
	if s.elapsedMinutes() < s.freshWindowMinutes() {
		return false
	}
	seq := len(s.Accepted)
	if seq <= 0 {
		return false
	}
	return seq%s.reuseEveryN() != 0
}

// PickReuse 从话术池选一段可复用的同卖点×角度话术：未达复用上限、不是最近刚用过的（换顺序，LRU）。
func (s *State) PickReuse(key string) *SpeechPoolEntry {
	if len(s.SpeechPool) == 0 {
		return nil
	}
	gap := max(3, s.Config.CombinationGap, s.Config.PointGap)
	limit := s.maxReuse()
	best := -1
	for i := range s.SpeechPool {
		e := &s.SpeechPool[i]
		if e.Text == "" || e.ReuseCount >= limit {
			continue
		}
		// 只复用同卖点×角度的话术，避免讲错卖点。
		if key != "" && e.Key != key {
			continue
		}
		if e.LastIndex >= 0 && len(s.Accepted)-e.LastIndex <= gap {
			continue
		}
		if best == -1 || e.LastIndex < s.SpeechPool[best].LastIndex {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	return &s.SpeechPool[best]
}

// AddToSpeechPool 生成并采纳一段 body 话术后入池（按文本去重，复用段不会重复入池）。
func (s *State) AddToSpeechPool(text, key string) {
	if text == "" {
		return
	}
	for i := range s.SpeechPool {
		if s.SpeechPool[i].Text == text {
			return
		}
	}
	lastIndex := len(s.Accepted) - 1
	if lastIndex < 0 {
		lastIndex = 0
	}
	s.SpeechPool = append(s.SpeechPool, SpeechPoolEntry{Text: text, Key: key, ReuseCount: 0, LastIndex: lastIndex})
	if len(s.SpeechPool) > 200 {
		s.SpeechPool = s.SpeechPool[len(s.SpeechPool)-200:]
	}
}

// MarkSpeechReused 复用某段后更新复用次数与最近使用位置。
func (s *State) MarkSpeechReused(e *SpeechPoolEntry) {
	if e == nil {
		return
	}
	e.ReuseCount++
	e.LastIndex = len(s.Accepted)
}
