package anchorcycle

import (
	"strings"
	"time"
	"unicode"
)

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
// 正式场首次开场实时，其后普通主线按七三混合；其它表面保留旧新鲜期及 N 段交替策略。
func (s *State) ShouldReuse() bool {
	if !s.SpeechReuseEnabled() {
		return false
	}
	if s.Surface == "formal_live" {
		return s.formalMainlineShouldReuse()
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

// AddToSpeechPool 采纳后入池；正式场按环节、卖点角度及侧面分别匹配。
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

// normalizeSpeechForDedup 归一化：只保留字母和数字，去掉标点、空白与表情符号。
// 用于逐字去重比对，避免"买鸭子最怕买到贴牌的、假的"与"买鸭子最怕买到贴牌的假的"被判成不同。
func normalizeSpeechForDedup(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// IsDuplicateSpeech 判断 text 是否与已采纳话术逐字重复（归一化后比对历史话术池）。
// 用于拦截"轮盘循环 + 模型低温复读"导致的跨轮次复读。
func (s *State) IsDuplicateSpeech(text string) bool {
	norm := normalizeSpeechForDedup(text)
	if norm == "" {
		return false
	}
	for i := range s.SpeechPool {
		if normalizeSpeechForDedup(s.SpeechPool[i].Text) == norm {
			return true
		}
	}
	return false
}

// RecentSameTask 判断当前任务（卖点×角度）是否在近期已采纳段里出现过。
// 用于润色降级：同一角度首次讲不润色（正文 + 侧面筛事实已拉开结构），
// 近期复讲（≈侧面轮换一圈回到同侧面）才润色换措辞，避免"叮1分钟"这类数字短语照抄。
func (s *State) RecentSameTask(recentN int) bool {
	if recentN <= 0 {
		return false
	}
	task := s.Task()
	start := len(s.Accepted) - recentN
	if start < 0 {
		start = 0
	}
	for _, part := range s.Accepted[start:] {
		if part.Task.Key == task.Key {
			return true
		}
	}
	return false
}

// RotateAfterDuplicate 复读时换到下一个未复读过的角度。
// 返回 false 表示角度池已轮尽（所有角度都复读过一次），此时放弃去重、接受本段。
func (s *State) RotateAfterDuplicate() bool {
	if s.DuplicateSkippedKeys == nil {
		s.DuplicateSkippedKeys = map[string]bool{}
	}
	s.DuplicateSkippedKeys[s.Task().Key] = true
	if s.Config.Rolling && s.PriceHook == nil {
		// A duplicate changes the pending direction, not its product quota.
		// Use the same point/angle scheduler instead of walking the global queue.
		if s.chooseProductWeightedTask() {
			return true
		}
		// Only this product is exhausted. Preserve other products' history and
		// the existing accept-on-exhaustion behavior without borrowing a task.
		for _, task := range s.Queue {
			if s.productTaskEligible(task) {
				delete(s.DuplicateSkippedKeys, task.Key)
			}
		}
		return false
	}
	remaining := 0
	for _, t := range s.Queue {
		if t.Key != "" && !s.DuplicateSkippedKeys[t.Key] {
			remaining++
		}
	}
	if remaining == 0 {
		s.DuplicateSkippedKeys = map[string]bool{}
		return false
	}
	for offset := 1; offset <= len(s.Queue); offset++ {
		i := (s.TaskIndex + offset) % len(s.Queue)
		if s.Queue[i].Key != "" && !s.DuplicateSkippedKeys[s.Queue[i].Key] {
			s.TaskIndex = i
			s.Cursor = i
			s.Status = "ready"
			return true
		}
	}
	return false
}
