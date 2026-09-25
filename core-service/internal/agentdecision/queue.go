package agentdecision

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"livecompanion/core/internal/roomintel"
)

const (
	DefaultTTL          = 2 * time.Minute
	DefaultCooldown     = 90 * time.Second
	DefaultCapacity     = 24
	ManualPriority      = 100
	QuickAnswerPriority = 120
)

type Source string

const (
	SourceAgent  Source = "agent"
	SourceManual Source = "manual"
)

type Status string

const (
	StatusPending Status = "PENDING"
	StatusClaimed Status = "CLAIMED"
)

type Candidate struct {
	Source        Source `json:"source"`
	Topic         string `json:"topic,omitempty"`
	Question      string `json:"question,omitempty"`
	Title         string `json:"title,omitempty"`
	Summary       string `json:"summary,omitempty"`
	ReplyHint     string `json:"reply_hint,omitempty"`
	Priority      int    `json:"priority,omitempty"`
	EventID       int64  `json:"event_id,omitempty"`
	UserID        string `json:"user_id,omitempty"`
	ForceReopen   bool   `json:"force_reopen,omitempty"`
	ManualAction  string `json:"manual_action,omitempty"`
	ManualOrigin  string `json:"manual_origin,omitempty"`
	ExecutionMode string `json:"execution_mode,omitempty"`
	FixedText     string `json:"fixed_text,omitempty"`
	TTLSeconds    int    `json:"ttl_seconds,omitempty"`
}

type Item struct {
	ID              string     `json:"id"`
	RoomID          int64      `json:"room_id"`
	Topic           string     `json:"topic"`
	Title           string     `json:"title"`
	Summary         string     `json:"summary,omitempty"`
	ReplyHint       string     `json:"reply_hint,omitempty"`
	Priority        int        `json:"priority"`
	Status          Status     `json:"status"`
	Sources         []Source   `json:"sources"`
	MergedCount     int        `json:"merged_count"`
	SampleQuestions []string   `json:"sample_questions,omitempty"`
	LinkedEventIDs  []int64    `json:"linked_event_ids,omitempty"`
	UserIDs         []string   `json:"user_ids,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	LastSeenAt      time.Time  `json:"last_seen_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	ClaimedAt       *time.Time `json:"claimed_at,omitempty"`
	ManualPromoted  bool       `json:"manual_promoted"`
	ManualAction    string     `json:"manual_action,omitempty"`
	ManualOrigin    string     `json:"manual_origin,omitempty"`
	ExecutionMode   string     `json:"execution_mode,omitempty"`
	FixedText       string     `json:"fixed_text,omitempty"`
}

type RecentAnswer struct {
	Topic           string    `json:"topic"`
	Title           string    `json:"title"`
	AnsweredAt      time.Time `json:"answered_at"`
	CooldownUntil   time.Time `json:"cooldown_until"`
	Accumulated     int       `json:"accumulated"`
	SampleQuestions []string  `json:"sample_questions,omitempty"`
}

type Note struct {
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type Summary struct {
	State         string `json:"state"`
	Focus         string `json:"focus,omitempty"`
	Reason        string `json:"reason,omitempty"`
	QueueLength   int    `json:"queue_length"`
	ManualWaiting int    `json:"manual_waiting"`
	AgentWaiting  int    `json:"agent_waiting"`
	CoolingTopics int    `json:"cooling_topics"`
}

type Snapshot struct {
	RoomID           int64          `json:"room_id"`
	GeneratedAt      time.Time      `json:"generated_at"`
	Summary          Summary        `json:"summary"`
	Queue            []Item         `json:"queue"`
	RecentlyAnswered []RecentAnswer `json:"recently_answered"`
	Notes            []Note         `json:"notes"`
	Capacity         int            `json:"capacity"`
	TTLSeconds       int64          `json:"ttl_seconds"`
	CooldownSeconds  int64          `json:"cooldown_seconds"`
}

type EnqueueResult struct {
	Item             *Item         `json:"item,omitempty"`
	Merged           bool          `json:"merged"`
	Promoted         bool          `json:"promoted"`
	Suppressed       bool          `json:"suppressed"`
	RecentlyAnswered *RecentAnswer `json:"recently_answered,omitempty"`
	Dropped          *Item         `json:"dropped,omitempty"`
}

type roomState struct {
	items  []*Item
	recent map[string]*RecentAnswer
	notes  []Note
}

type Queue struct {
	mu       sync.Mutex
	rooms    map[int64]*roomState
	now      func() time.Time
	ttl      time.Duration
	cooldown time.Duration
	capacity int
	seq      atomic.Uint64
}

func New() *Queue {
	return NewWithClock(time.Now, DefaultTTL, DefaultCooldown, DefaultCapacity)
}

func NewWithClock(now func() time.Time, ttl, cooldown time.Duration, capacity int) *Queue {
	if now == nil {
		now = time.Now
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if cooldown <= 0 {
		cooldown = DefaultCooldown
	}
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Queue{
		rooms:    make(map[int64]*roomState),
		now:      now,
		ttl:      ttl,
		cooldown: cooldown,
		capacity: capacity,
	}
}

func (q *Queue) Enqueue(roomID int64, input Candidate) EnqueueResult {
	if roomID <= 0 {
		return EnqueueResult{}
	}
	input.Question = strings.TrimSpace(input.Question)
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.ReplyHint = strings.TrimSpace(input.ReplyHint)
	input.UserID = strings.TrimSpace(input.UserID)
	input.Topic = normalizeTopic(input.Topic, input.Question)
	input.ManualAction = strings.ToLower(strings.TrimSpace(input.ManualAction))
	input.ManualOrigin = strings.ToLower(strings.TrimSpace(input.ManualOrigin))
	input.ExecutionMode = strings.ToLower(strings.TrimSpace(input.ExecutionMode))
	input.FixedText = strings.TrimSpace(input.FixedText)
	if input.ExecutionMode != "verbatim" {
		input.ExecutionMode = "intent"
		input.FixedText = ""
	}
	if input.Source == SourceManual {
		if input.ManualAction == "quick" {
			input.ForceReopen = true
		} else {
			input.ManualAction = "answer"
		}
	} else {
		input.ManualAction = ""
	}
	if input.Topic == "" {
		return EnqueueResult{}
	}
	if input.Source != SourceManual {
		input.Source = SourceAgent
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	now := q.now().UTC()
	state := q.roomLocked(roomID)
	q.pruneLocked(roomID, state, now)

	if recent := state.recent[input.Topic]; recent != nil && now.Before(recent.CooldownUntil) && !input.ForceReopen {
		recent.Accumulated++
		appendUniqueString(&recent.SampleQuestions, input.Question, 8)
		q.addNoteLocked(state, now, "cooldown", fmt.Sprintf("%s刚回答过，当前新问题先累计，不重复打断", recent.Title))
		copy := cloneRecent(*recent)
		return EnqueueResult{Suppressed: true, RecentlyAnswered: &copy}
	}

	for _, item := range state.items {
		if item.Topic != input.Topic {
			continue
		}
		beforePriority := item.Priority
		q.mergeLocked(item, input, now)
		promoted := input.Source == SourceManual && (beforePriority < item.Priority || !item.ManualPromoted)
		if input.Source == SourceManual {
			item.ManualPromoted = true
			if input.ManualAction == "quick" {
				q.addNoteLocked(state, now, "manual_quick", fmt.Sprintf("人工抢答已融合到“%s”并升到最高优先", item.Title))
			} else {
				q.addNoteLocked(state, now, "manual_promote", fmt.Sprintf("人工回答已融合到“%s”并置顶", item.Title))
			}
		} else {
			q.addNoteLocked(state, now, "merge", fmt.Sprintf("相似问题已融合到“%s”，当前累计%d条", item.Title, item.MergedCount))
		}
		q.sortLocked(state)
		copy := cloneItem(*item)
		return EnqueueResult{Item: &copy, Merged: true, Promoted: promoted}
	}

	ttl := q.ttl
	if input.TTLSeconds > 0 {
		ttl = time.Duration(input.TTLSeconds) * time.Second
		if ttl > 10*time.Minute {
			ttl = 10 * time.Minute
		}
	}
	priority := normalizePriority(input)
	id := fmt.Sprintf("d-%d-%d-%06d", roomID, now.UnixMilli(), q.seq.Add(1))
	item := &Item{
		ID:            id,
		RoomID:        roomID,
		Topic:         input.Topic,
		Title:         candidateTitle(input),
		Summary:       input.Summary,
		ReplyHint:     input.ReplyHint,
		Priority:      priority,
		Status:        StatusPending,
		Sources:       []Source{input.Source},
		MergedCount:   1,
		CreatedAt:     now,
		LastSeenAt:    now,
		ExpiresAt:     now.Add(ttl),
		ManualAction:  input.ManualAction,
		ManualOrigin:  input.ManualOrigin,
		ExecutionMode: input.ExecutionMode,
		FixedText:     input.FixedText,
	}
	if input.Source == SourceManual {
		item.ManualPromoted = true
	}
	appendUniqueString(&item.SampleQuestions, input.Question, 8)
	appendUniqueInt64(&item.LinkedEventIDs, input.EventID, 16)
	appendUniqueString(&item.UserIDs, input.UserID, 16)
	state.items = append(state.items, item)
	q.sortLocked(state)

	var dropped *Item
	if len(state.items) > q.capacity {
		last := state.items[len(state.items)-1]
		copy := cloneItem(*last)
		dropped = &copy
		state.items = state.items[:len(state.items)-1]
		q.addNoteLocked(state, now, "capacity_drop", fmt.Sprintf("队列已满，低优先级“%s”已抛出", last.Title))
	}
	if input.Source == SourceManual {
		if input.ManualAction == "quick" {
			q.addNoteLocked(state, now, "manual_quick", fmt.Sprintf("人工抢答“%s”已进入最高优先区", item.Title))
		} else {
			q.addNoteLocked(state, now, "manual_enqueue", fmt.Sprintf("人工回答“%s”已进入队首优先区", item.Title))
		}
	} else {
		q.addNoteLocked(state, now, "agent_enqueue", fmt.Sprintf("Agent发现“%s”，已进入待打断队列", item.Title))
	}
	copy := cloneItem(*item)
	return EnqueueResult{Item: &copy, Dropped: dropped}
}

func (q *Queue) Complete(roomID int64, id string) (*RecentAnswer, bool) {
	id = strings.TrimSpace(id)
	if roomID <= 0 || id == "" {
		return nil, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().UTC()
	state := q.roomLocked(roomID)
	q.pruneLocked(roomID, state, now)
	for index, item := range state.items {
		if item.ID != id {
			continue
		}
		recent := &RecentAnswer{
			Topic:           item.Topic,
			Title:           item.Title,
			AnsweredAt:      now,
			CooldownUntil:   now.Add(q.cooldown),
			SampleQuestions: append([]string(nil), item.SampleQuestions...),
		}
		state.recent[item.Topic] = recent
		state.items = append(state.items[:index], state.items[index+1:]...)
		q.addNoteLocked(state, now, "answered", fmt.Sprintf("“%s”已回答，%d秒内同类问题先累计", item.Title, int(q.cooldown.Seconds())))
		copy := cloneRecent(*recent)
		return &copy, true
	}
	return nil, false
}

func (q *Queue) ClaimNext(roomID int64) (*Item, bool) {
	if roomID <= 0 {
		return nil, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().UTC()
	state := q.roomLocked(roomID)
	q.pruneLocked(roomID, state, now)
	q.sortLocked(state)
	for _, item := range state.items {
		if item.Status != StatusPending {
			continue
		}
		item.Status = StatusClaimed
		item.ClaimedAt = timePtr(now)
		copy := cloneItem(*item)
		q.addNoteLocked(state, now, "claim", fmt.Sprintf("准备打断：%s", item.Title))
		return &copy, true
	}
	return nil, false
}

func (q *Queue) Release(roomID int64, id string) (*Item, bool) {
	id = strings.TrimSpace(id)
	if roomID <= 0 || id == "" {
		return nil, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().UTC()
	state := q.roomLocked(roomID)
	q.pruneLocked(roomID, state, now)
	for _, item := range state.items {
		if item.ID != id || item.Status != StatusClaimed {
			continue
		}
		item.Status = StatusPending
		item.ClaimedAt = nil
		item.ExpiresAt = now.Add(q.ttl)
		q.addNoteLocked(state, now, "release", fmt.Sprintf("“%s”执行未完成，已退回待打断队列", item.Title))
		q.sortLocked(state)
		copy := cloneItem(*item)
		return &copy, true
	}
	return nil, false
}

func (q *Queue) Remove(roomID int64, id string) (*Item, bool) {
	id = strings.TrimSpace(id)
	if roomID <= 0 || id == "" {
		return nil, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().UTC()
	state := q.roomLocked(roomID)
	q.pruneLocked(roomID, state, now)
	for index, item := range state.items {
		if item.ID != id {
			continue
		}
		copy := cloneItem(*item)
		state.items = append(state.items[:index], state.items[index+1:]...)
		q.addNoteLocked(state, now, "manual_remove", fmt.Sprintf("“%s”已从待打断队列移除", item.Title))
		return &copy, true
	}
	return nil, false
}

func (q *Queue) Get(roomID int64, id string) (*Item, bool) {
	id = strings.TrimSpace(id)
	if roomID <= 0 || id == "" {
		return nil, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().UTC()
	state := q.roomLocked(roomID)
	q.pruneLocked(roomID, state, now)
	for _, item := range state.items {
		if item.ID != id {
			continue
		}
		copy := cloneItem(*item)
		return &copy, true
	}
	return nil, false
}

func (q *Queue) Snapshot(roomID int64) Snapshot {
	now := q.now().UTC()
	if roomID <= 0 {
		return Snapshot{RoomID: roomID, GeneratedAt: now}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	state := q.roomLocked(roomID)
	q.pruneLocked(roomID, state, now)
	q.sortLocked(state)

	items := make([]Item, 0, len(state.items))
	manualWaiting := 0
	agentWaiting := 0
	for _, item := range state.items {
		items = append(items, cloneItem(*item))
		if hasSource(item.Sources, SourceManual) {
			manualWaiting++
		} else {
			agentWaiting++
		}
	}
	recent := make([]RecentAnswer, 0, len(state.recent))
	for _, value := range state.recent {
		recent = append(recent, cloneRecent(*value))
	}
	sort.SliceStable(recent, func(i, j int) bool {
		return recent[i].AnsweredAt.After(recent[j].AnsweredAt)
	})

	summary := Summary{
		State:         "OBSERVING",
		QueueLength:   len(items),
		ManualWaiting: manualWaiting,
		AgentWaiting:  agentWaiting,
		CoolingTopics: len(recent),
	}
	if len(items) > 0 {
		summary.State = "READY_TO_INTERRUPT"
		summary.Focus = items[0].Title
		if hasSource(items[0].Sources, SourceManual) {
			if items[0].ManualAction == "quick" {
				summary.Reason = "人工抢答最高优先，允许进入硬打断流程"
			} else {
				summary.Reason = "人工回答优先，等待当前播音窗口结束后插入"
			}
		} else if items[0].MergedCount > 1 {
			summary.Reason = fmt.Sprintf("同类问题累计%d条，优先统一回答", items[0].MergedCount)
		} else {
			summary.Reason = "Agent候选按优先级与时间排队"
		}
	}

	notes := append([]Note(nil), state.notes...)
	return Snapshot{
		RoomID:           roomID,
		GeneratedAt:      now,
		Summary:          summary,
		Queue:            items,
		RecentlyAnswered: recent,
		Notes:            notes,
		Capacity:         q.capacity,
		TTLSeconds:       int64(q.ttl.Seconds()),
		CooldownSeconds:  int64(q.cooldown.Seconds()),
	}
}

func (q *Queue) ClearRoom(roomID int64) {
	if roomID <= 0 {
		return
	}
	q.mu.Lock()
	delete(q.rooms, roomID)
	q.mu.Unlock()
}

func (q *Queue) roomLocked(roomID int64) *roomState {
	state := q.rooms[roomID]
	if state == nil {
		state = &roomState{recent: make(map[string]*RecentAnswer)}
		q.rooms[roomID] = state
	}
	return state
}

func (q *Queue) mergeLocked(item *Item, input Candidate, now time.Time) {
	item.MergedCount++
	item.LastSeenAt = now
	item.ExpiresAt = now.Add(q.ttl)
	if input.Summary != "" {
		item.Summary = input.Summary
	}
	if input.ReplyHint != "" {
		item.ReplyHint = input.ReplyHint
	}
	if input.ManualOrigin != "" {
		item.ManualOrigin = input.ManualOrigin
	}
	if input.ExecutionMode == "verbatim" {
		item.ExecutionMode = "verbatim"
		if input.FixedText != "" {
			item.FixedText = input.FixedText
		}
	} else if item.ExecutionMode == "" {
		item.ExecutionMode = "intent"
	}
	if input.Title != "" && item.Title == "" {
		item.Title = input.Title
	}
	appendUniqueSource(&item.Sources, input.Source)
	appendUniqueString(&item.SampleQuestions, input.Question, 8)
	appendUniqueInt64(&item.LinkedEventIDs, input.EventID, 16)
	appendUniqueString(&item.UserIDs, input.UserID, 16)
	if input.Source == SourceManual {
		if input.ManualAction == "quick" {
			item.ManualAction = "quick"
		} else if item.ManualAction == "" {
			item.ManualAction = "answer"
		}
	}
	base := normalizePriority(input)
	if input.Source == SourceManual {
		if item.Priority < base {
			item.Priority = base
		}
	} else {
		repetitionBoost := minInt(item.MergedCount-1, 6) * 5
		if base+repetitionBoost > item.Priority {
			item.Priority = base + repetitionBoost
		}
	}
}

func (q *Queue) sortLocked(state *roomState) {
	sort.SliceStable(state.items, func(i, j int) bool {
		if state.items[i].Priority != state.items[j].Priority {
			return state.items[i].Priority > state.items[j].Priority
		}
		return state.items[i].CreatedAt.Before(state.items[j].CreatedAt)
	})
}

func (q *Queue) pruneLocked(roomID int64, state *roomState, now time.Time) {
	if len(state.items) > 0 {
		kept := state.items[:0]
		for _, item := range state.items {
			if !now.Before(item.ExpiresAt) {
				q.addNoteLocked(state, now, "expired", fmt.Sprintf("“%s”等待过久已自动抛出", item.Title))
				continue
			}
			kept = append(kept, item)
		}
		state.items = kept
	}
	for topic, recent := range state.recent {
		if now.Before(recent.CooldownUntil) {
			continue
		}
		delete(state.recent, topic)
	}
}

func (q *Queue) addNoteLocked(state *roomState, now time.Time, kind, message string) {
	state.notes = append([]Note{{Kind: kind, Message: message, CreatedAt: now}}, state.notes...)
	if len(state.notes) > 10 {
		state.notes = state.notes[:10]
	}
}

func normalizeTopic(topic, question string) string {
	topic = strings.TrimSpace(topic)
	if topic != "" {
		return topic
	}
	return roomintel.QuestionTopic(question)
}

func normalizePriority(input Candidate) int {
	if input.Source == SourceManual {
		if input.ManualAction == "quick" {
			return QuickAnswerPriority
		}
		return ManualPriority
	}
	priority := input.Priority
	if priority <= 0 {
		priority = 30
	}
	if strings.HasPrefix(input.Topic, "FAMILY:售后") {
		priority = maxInt(priority, 70)
	}
	if strings.HasPrefix(input.Topic, "SIGNAL:ORDER") {
		priority = maxInt(priority, 85)
	}
	if priority > 95 {
		priority = 95
	}
	return priority
}

func candidateTitle(input Candidate) string {
	if input.Title != "" {
		return input.Title
	}
	if strings.HasPrefix(input.Topic, "FAMILY:") {
		return strings.TrimPrefix(input.Topic, "FAMILY:")
	}
	if input.Topic == "SIGNAL:ORDER" {
		return "下单信号"
	}
	if input.Question != "" {
		return input.Question
	}
	return input.Topic
}

func hasSource(values []Source, target Source) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func appendUniqueSource(values *[]Source, value Source) {
	if hasSource(*values, value) {
		return
	}
	*values = append(*values, value)
}

func appendUniqueString(values *[]string, value string, limit int) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	for _, existing := range *values {
		if existing == value {
			return
		}
	}
	*values = append(*values, value)
	if len(*values) > limit {
		*values = (*values)[len(*values)-limit:]
	}
}

func appendUniqueInt64(values *[]int64, value int64, limit int) {
	if value <= 0 {
		return
	}
	for _, existing := range *values {
		if existing == value {
			return
		}
	}
	*values = append(*values, value)
	if len(*values) > limit {
		*values = (*values)[len(*values)-limit:]
	}
}

func cloneItem(item Item) Item {
	item.Sources = append([]Source(nil), item.Sources...)
	item.SampleQuestions = append([]string(nil), item.SampleQuestions...)
	item.LinkedEventIDs = append([]int64(nil), item.LinkedEventIDs...)
	item.UserIDs = append([]string(nil), item.UserIDs...)
	if item.ClaimedAt != nil {
		copy := *item.ClaimedAt
		item.ClaimedAt = &copy
	}
	return item
}

func cloneRecent(value RecentAnswer) RecentAnswer {
	value.SampleQuestions = append([]string(nil), value.SampleQuestions...)
	return value
}

func timePtr(value time.Time) *time.Time {
	copy := value
	return &copy
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
