package agentdecision

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"livecompanion/core/internal/roomintel"
)

const (
	DefaultTTL            = 2 * time.Minute
	DefaultCooldown       = 90 * time.Second
	DefaultCapacity       = 24
	ClaimExecutionTimeout = 3 * time.Minute
	ManualPriority        = 100
	QuickAnswerPriority   = 120
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

type QuestionDebt struct {
	Topic           string    `json:"topic,omitempty"`
	FirstSeenAt     time.Time `json:"first_seen_at,omitempty"`
	RepeatCount     int       `json:"repeat_count,omitempty"`
	UniqueUsers     int       `json:"unique_users,omitempty"`
	WaitingSeconds  int       `json:"waiting_seconds,omitempty"`
	BusinessValue   float64   `json:"business_value,omitempty"`
	CurrentPriority float64   `json:"current_priority,omitempty"`
}

type InteractionDecision struct {
	OnlineCount       int           `json:"online_count,omitempty"`
	UniqueChatters30s int           `json:"unique_chatters_30s,omitempty"`
	QuestionPressure  float64       `json:"question_pressure,omitempty"`
	Handle            bool          `json:"handle"`
	PrimaryEvent      string        `json:"primary_event,omitempty"`
	MergedEventIDs    []int64       `json:"merged_event_ids,omitempty"`
	EventValue        float64       `json:"event_value,omitempty"`
	ValueLevel        string        `json:"value_level,omitempty"`
	Reason            string        `json:"reason,omitempty"`
	DeadlineAt        time.Time     `json:"deadline_at,omitempty"`
	BudgetLevel       string        `json:"budget_level,omitempty"`
	BudgetAllowed     bool          `json:"budget_allowed"`
	Heat              string        `json:"heat,omitempty"`
	PreferenceFactor  float64       `json:"preference_factor,omitempty"`
	QuestionDebt      *QuestionDebt `json:"question_debt,omitempty"`
}

type Candidate struct {
	PurposeID          string          `json:"purpose_id,omitempty"`
	PurposeKnowledge   json.RawMessage `json:"purpose_knowledge,omitempty"`
	EventExpiresAt     time.Time       `json:"event_expires_at,omitempty"`
	PreparedTaskID     string          `json:"prepared_task_id,omitempty"`
	PreparedCutMS      int             `json:"prepared_cut_ms,omitempty"`
	PriorityClass      string          `json:"priority_class,omitempty"`
	Weight             int             `json:"weight,omitempty"`
	Scene              string          `json:"scene,omitempty"`
	OrderSource        string          `json:"order_source,omitempty"`
	CooldownSeconds    int             `json:"cooldown_seconds,omitempty"`
	RateLimitPerMinute int             `json:"rate_limit_per_minute,omitempty"`
	MaxRetries         int             `json:"max_retries,omitempty"`
	OriginPlanID       int64           `json:"origin_plan_id,omitempty"`
	OriginMainline     string          `json:"origin_mainline,omitempty"`
	OriginTrackID      string          `json:"origin_track_id,omitempty"`
	Schedule           json.RawMessage `json:"schedule,omitempty"`

	Source               Source              `json:"source"`
	Topic                string              `json:"topic,omitempty"`
	Question             string              `json:"question,omitempty"`
	Title                string              `json:"title,omitempty"`
	Summary              string              `json:"summary,omitempty"`
	ReplyHint            string              `json:"reply_hint,omitempty"`
	MissionKind          string              `json:"mission_kind,omitempty"`
	MissionEventCount    int                 `json:"mission_event_count,omitempty"`
	MissionWindowSeconds int                 `json:"mission_window_seconds,omitempty"`
	Priority             int                 `json:"priority,omitempty"`
	EventID              int64               `json:"event_id,omitempty"`
	UserID               string              `json:"user_id,omitempty"`
	Nicknames            []string            `json:"nicknames,omitempty"`
	InteractionDecision  InteractionDecision `json:"interaction_decision,omitempty"`
	ForceReopen          bool                `json:"force_reopen,omitempty"`
	ManualAction         string              `json:"manual_action,omitempty"`
	ManualOrigin         string              `json:"manual_origin,omitempty"`
	ExecutionMode        string              `json:"execution_mode,omitempty"`
	FixedText            string              `json:"fixed_text,omitempty"`
	TTLSeconds           int                 `json:"ttl_seconds,omitempty"`
}

type Item struct {
	CompletionUntil     time.Time       `json:"completion_until,omitempty"`
	PreparationCutPlans json.RawMessage `json:"preparation_cut_plans,omitempty"`
	CutLevel            string          `json:"cut_level,omitempty"`
	SelectedContentPlan json.RawMessage `json:"selected_content_plan,omitempty"`
	PurposeID           string          `json:"purpose_id,omitempty"`
	PurposeKnowledge    json.RawMessage `json:"purpose_knowledge,omitempty"`
	AudioPrepared       bool            `json:"audio_prepared,omitempty"`
	PreparedUntil       time.Time       `json:"prepared_until,omitempty"`
	PreparedTaskID      string          `json:"prepared_task_id,omitempty"`
	PreparedCutMS       int             `json:"prepared_cut_ms,omitempty"`
	PlaybackTaskID      string          `json:"playback_task_id,omitempty"`
	PriorityClass       string          `json:"priority_class,omitempty"`
	Weight              int             `json:"weight,omitempty"`
	Scene               string          `json:"scene,omitempty"`
	OrderSource         string          `json:"order_source,omitempty"`
	CooldownSeconds     int             `json:"cooldown_seconds,omitempty"`
	RateLimitPerMinute  int             `json:"rate_limit_per_minute,omitempty"`
	MaxRetries          int             `json:"max_retries,omitempty"`
	OriginPlanID        int64           `json:"origin_plan_id,omitempty"`
	OriginMainline      string          `json:"origin_mainline,omitempty"`
	OriginTrackID       string          `json:"origin_track_id,omitempty"`
	Schedule            json.RawMessage `json:"schedule,omitempty"`

	Attempts     int       `json:"attempts,omitempty"`
	RetryAfter   time.Time `json:"retry_after,omitempty"`
	ContentGoal  string    `json:"content_goal,omitempty"`
	ContentAngle string    `json:"content_angle,omitempty"`

	ID                   string              `json:"id"`
	RoomID               int64               `json:"room_id"`
	Topic                string              `json:"topic"`
	Title                string              `json:"title"`
	Summary              string              `json:"summary,omitempty"`
	ReplyHint            string              `json:"reply_hint,omitempty"`
	MissionKind          string              `json:"mission_kind,omitempty"`
	MissionEventCount    int                 `json:"mission_event_count,omitempty"`
	MissionWindowSeconds int                 `json:"mission_window_seconds,omitempty"`
	Priority             int                 `json:"priority"`
	Status               Status              `json:"status"`
	Sources              []Source            `json:"sources"`
	MergedCount          int                 `json:"merged_count"`
	SampleQuestions      []string            `json:"sample_questions,omitempty"`
	LinkedEventIDs       []int64             `json:"linked_event_ids,omitempty"`
	UserIDs              []string            `json:"user_ids,omitempty"`
	Nicknames            []string            `json:"nicknames,omitempty"`
	InteractionDecision  InteractionDecision `json:"interaction_decision,omitempty"`
	CreatedAt            time.Time           `json:"created_at"`
	LastSeenAt           time.Time           `json:"last_seen_at"`
	ExpiresAt            time.Time           `json:"expires_at"`
	ClaimedAt            *time.Time          `json:"claimed_at,omitempty"`
	ManualPromoted       bool                `json:"manual_promoted"`
	ManualAction         string              `json:"manual_action,omitempty"`
	ManualOrigin         string              `json:"manual_origin,omitempty"`
	ExecutionMode        string              `json:"execution_mode,omitempty"`
	FixedText            string              `json:"fixed_text,omitempty"`
}

func itemDeadline(item *Item) time.Time {
	if item.AudioPrepared && !item.PreparedUntil.IsZero() {
		return item.PreparedUntil
	}
	if !item.CompletionUntil.IsZero() {
		return item.CompletionUntil
	}
	return item.ExpiresAt
}

type RecentAnswer struct {
	PreparationCutPlans json.RawMessage `json:"preparation_cut_plans,omitempty"`
	CutLevel            string          `json:"cut_level,omitempty"`
	SelectedContentPlan json.RawMessage `json:"selected_content_plan,omitempty"`
	DecisionID          string          `json:"decision_id,omitempty"`
	MissionKind         string          `json:"mission_kind,omitempty"`
	Scene               string          `json:"scene,omitempty"`
	ContentGoal         string          `json:"content_goal,omitempty"`
	ContentAngle        string          `json:"content_angle,omitempty"`
	Topic               string          `json:"topic"`
	Title               string          `json:"title"`
	AnsweredAt          time.Time       `json:"answered_at"`
	CooldownUntil       time.Time       `json:"cooldown_until"`
	Accumulated         int             `json:"accumulated"`
	SampleQuestions     []string        `json:"sample_questions,omitempty"`
}

type Note struct {
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type SimulationResult struct {
	DecisionID       string    `json:"decision_id"`
	Question         string    `json:"question"`
	Reply            string    `json:"reply"`
	ExecutionMode    string    `json:"execution_mode"`
	PlanName         string    `json:"plan_name,omitempty"`
	UserLayerVersion uint64    `json:"user_layer_version,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
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

type CompletionStats struct {
	Admitted          int `json:"admitted"`
	Completed         int `json:"completed"`
	Failed            int `json:"failed"`
	Cancelled         int `json:"cancelled"`
	Merged            int `json:"merged"`
	AdmissionDeferred int `json:"admission_deferred"`
}
type TerminalOutcome struct {
	DecisionID string    `json:"decision_id"`
	EventIDs   []int64   `json:"event_ids"`
	Status     string    `json:"status"`
	Reason     string    `json:"reason"`
	At         time.Time `json:"at"`
}
type Snapshot struct {
	Completion         CompletionStats    `json:"completion"`
	TerminalHistory    []TerminalOutcome  `json:"terminal_history"`
	CompletedCutCounts map[string]int     `json:"completed_cut_counts"`
	PlayedHistory      []RecentAnswer     `json:"played_history"`
	RoomID             int64              `json:"room_id"`
	GeneratedAt        time.Time          `json:"generated_at"`
	Summary            Summary            `json:"summary"`
	Queue              []Item             `json:"queue"`
	RecentlyAnswered   []RecentAnswer     `json:"recently_answered"`
	Notes              []Note             `json:"notes"`
	SimulationResults  []SimulationResult `json:"simulation_results,omitempty"`
	Capacity           int                `json:"capacity"`
	TTLSeconds         int64              `json:"ttl_seconds"`
	CooldownSeconds    int64              `json:"cooldown_seconds"`
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
	completion  CompletionStats
	outcomes    []TerminalOutcome
	cutCounts   map[string]int
	played      []RecentAnswer
	items       []*Item
	recent      map[string]*RecentAnswer
	notes       []Note
	simulations []SimulationResult
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
	input.MissionKind = strings.ToLower(strings.TrimSpace(input.MissionKind))
	if input.MissionEventCount < 0 {
		input.MissionEventCount = 0
	}
	if input.MissionWindowSeconds < 0 {
		input.MissionWindowSeconds = 0
	}
	input.UserID = strings.TrimSpace(input.UserID)
	cleanNicknames := make([]string, 0, len(input.Nicknames))
	for _, nickname := range input.Nicknames {
		appendUniqueString(&cleanNicknames, nickname, 16)
	}
	input.Nicknames = cleanNicknames
	input.InteractionDecision = cloneInteractionDecision(input.InteractionDecision)
	input.Topic = normalizeTopic(input.Topic, input.Question)
	input.ManualAction = strings.ToLower(strings.TrimSpace(input.ManualAction))
	input.ManualOrigin = strings.ToLower(strings.TrimSpace(input.ManualOrigin))
	if input.ManualOrigin == "agent_input_preview" && input.Question != "" {
		input.Topic = "PREVIEW:" + input.Question
	} else if input.ManualOrigin == "test_simulation" && input.Topic == "" && input.Question != "" {
		input.Topic = "TEST:" + input.Question
	}
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
		state.completion.Merged++
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

	// Capacity belongs to admission. Never evict work already selected to run.
	if len(state.items) >= q.capacity {
		state.completion.AdmissionDeferred++
		q.addNoteLocked(state, now, "capacity_wait", "执行队列已满，新候选保留在问题池等待准入；已入队任务继续完成")
		return EnqueueResult{}
	}
	ttl := q.ttl
	if input.TTLSeconds > 0 {
		ttl = time.Duration(input.TTLSeconds) * time.Second
		if ttl > 10*time.Minute {
			ttl = 10 * time.Minute
		}
	}
	expiresAt := now.Add(ttl)
	if !input.EventExpiresAt.IsZero() && input.EventExpiresAt.Before(expiresAt) {
		expiresAt = input.EventExpiresAt
	}
	if !expiresAt.After(now) {
		return EnqueueResult{}
	}
	priority := normalizePriority(input)
	id := fmt.Sprintf("d-%d-%d-%06d", roomID, now.UnixMilli(), q.seq.Add(1))
	item := &Item{
		PurposeID: input.PurposeID, PurposeKnowledge: append(json.RawMessage(nil), input.PurposeKnowledge...),
		PreparedTaskID: input.PreparedTaskID, PreparedCutMS: input.PreparedCutMS,
		ID:                   id,
		RoomID:               roomID,
		Topic:                input.Topic,
		Title:                candidateTitle(input),
		Summary:              input.Summary,
		ReplyHint:            input.ReplyHint,
		MissionKind:          input.MissionKind,
		MissionEventCount:    input.MissionEventCount,
		MissionWindowSeconds: input.MissionWindowSeconds,
		Priority:             priority,
		PriorityClass:        input.PriorityClass, Weight: input.Weight, Scene: input.Scene,
		OrderSource: input.OrderSource, CooldownSeconds: input.CooldownSeconds,
		RateLimitPerMinute: input.RateLimitPerMinute, MaxRetries: input.MaxRetries,
		OriginPlanID: input.OriginPlanID, OriginMainline: input.OriginMainline, OriginTrackID: input.OriginTrackID,
		Schedule:            append(json.RawMessage(nil), input.Schedule...),
		Status:              StatusPending,
		Sources:             []Source{input.Source},
		MergedCount:         1,
		CreatedAt:           now,
		LastSeenAt:          now,
		ExpiresAt:           expiresAt,
		ManualAction:        input.ManualAction,
		ManualOrigin:        input.ManualOrigin,
		ExecutionMode:       input.ExecutionMode,
		FixedText:           input.FixedText,
		InteractionDecision: cloneInteractionDecision(input.InteractionDecision),
	}
	if input.Source == SourceManual {
		item.ManualPromoted = true
	}
	appendUniqueString(&item.SampleQuestions, input.Question, 8)
	appendUniqueInt64(&item.LinkedEventIDs, input.EventID, 16)
	appendUniqueString(&item.UserIDs, input.UserID, 16)
	for _, nickname := range input.Nicknames {
		appendUniqueString(&item.Nicknames, nickname, 16)
	}
	state.items = append(state.items, item)
	q.sortLocked(state)

	state.completion.Admitted++
	if item.Scene == "product_question" || item.Scene == "complaint" || item.Scene == "order" {
		item.CompletionUntil = now.Add(10 * time.Minute)
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
	return EnqueueResult{Item: &copy}
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
		cooldown := q.cooldown
		if item.Scene != "" {
			cooldown = time.Duration(item.CooldownSeconds) * time.Second
		}
		recent := &RecentAnswer{
			DecisionID: item.ID, MissionKind: item.MissionKind, Scene: item.Scene,
			ContentGoal: item.ContentGoal, ContentAngle: item.ContentAngle,
			CutLevel: item.CutLevel, SelectedContentPlan: append(json.RawMessage(nil), item.SelectedContentPlan...), PreparationCutPlans: append(json.RawMessage(nil), item.PreparationCutPlans...),
			Topic:           item.Topic,
			Title:           item.Title,
			AnsweredAt:      now,
			CooldownUntil:   now.Add(cooldown),
			SampleQuestions: append([]string(nil), item.SampleQuestions...),
		}
		state.recent[item.Topic] = recent
		state.played = append(state.played, cloneRecent(*recent))
		if recent.CutLevel != "" {
			state.cutCounts[recent.CutLevel]++
		}
		state.items = append(state.items[:index], state.items[index+1:]...)
		q.recordTerminalLocked(state, item, "COMPLETED", "actual_mainline_return", now)
		q.addNoteLocked(state, now, "answered", fmt.Sprintf("“%s”已回答，%d秒内同类问题先累计", item.Title, int(cooldown.Seconds())))
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
		if item.Status == StatusClaimed {
			return nil, false
		}
	}
	for _, item := range state.items {
		if item.Status != StatusPending {
			continue
		}
		if now.Before(item.RetryAfter) {
			continue
		}
		if item.RateLimitPerMinute > 0 {
			count := 0
			for _, r := range state.played {
				if r.Scene == item.Scene && now.Sub(r.AnsweredAt) < time.Minute {
					count++
				}
			}
			if count >= item.RateLimitPerMinute {
				q.addNoteLocked(state, now, "frequency_wait", item.Title+"本分钟回答上限已到，继续排队")
				continue
			}
		}
		item.Status = StatusClaimed
		item.Attempts++
		item.ClaimedAt = timePtr(now)
		copy := cloneItem(*item)
		q.addNoteLocked(state, now, "claim", fmt.Sprintf("准备打断：%s", item.Title))
		return &copy, true
	}
	return nil, false
}

func (q *Queue) Release(roomID int64, id string) (*Item, bool) {
	return q.release(roomID, id, false)
}

// A rejected cut never submitted audio. Requeue the prepared answer without
// consuming a generation retry or extending the original admission deadline.
func (q *Queue) RescheduleCut(roomID int64, id string, preparedUntil ...time.Time) (*Item, bool) {
	return q.release(roomID, id, true, preparedUntil...)
}

func (q *Queue) release(roomID int64, id string, cutReschedule bool, preparedUntil ...time.Time) (*Item, bool) {
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
		if item.ID != id || item.Status != StatusClaimed {
			continue
		}
		copy := cloneItem(*item)
		exhausted := !cutReschedule && item.Scene != "" && item.Attempts >= item.MaxRetries+1
		expired := !now.Before(itemDeadline(item))
		if cutReschedule {
			if item.PreparedUntil.IsZero() {
				item.PreparedUntil = now.Add(2 * time.Minute)
				if len(preparedUntil) > 0 && !preparedUntil[0].IsZero() && preparedUntil[0].Before(item.PreparedUntil) {
					item.PreparedUntil = preparedUntil[0]
				}
			}
			expired = !now.Before(item.PreparedUntil)
		}
		if expired || exhausted {
			reason := "execution_deadline"
			if exhausted {
				reason = "retry_exhausted"
			}
			q.recordTerminalLocked(state, item, "FAILED", reason, now)
			state.items = append(state.items[:index], state.items[index+1:]...)
			if exhausted {
				q.addNoteLocked(state, now, "retry_exhausted", fmt.Sprintf("“%s”失败已达到最大重试次数，撤销任务", item.Title))
			} else {
				q.addNoteLocked(state, now, "release_expired", fmt.Sprintf("“%s”执行未完成且原任务已过期，撤销任务", item.Title))
			}
			return &copy, true
		}
		item.Status = StatusPending
		item.ClaimedAt = nil
		item.PlaybackTaskID = ""
		if cutReschedule {
			if item.Attempts > 0 {
				item.Attempts--
			}
			item.PreparedTaskID, item.PreparedCutMS = "", 0
			item.AudioPrepared = true
			item.RetryAfter = now.Add(time.Second)
			q.addNoteLocked(state, now, "cut_reschedule", fmt.Sprintf("“%s”声音已备好，等待当前主线的新句末切点", item.Title))
			q.sortLocked(state)
			copy = cloneItem(*item)
			return &copy, true
		}
		item.AudioPrepared = false
		if item.Scene != "" {
			item.RetryAfter = now.Add(5 * time.Second)
		}
		q.addNoteLocked(state, now, "release", fmt.Sprintf("“%s”执行未完成，已退回待打断队列", item.Title))
		q.sortLocked(state)
		copy = cloneItem(*item)
		return &copy, true
	}
	return nil, false
}

// ReleaseClaimed returns all in-flight decisions to pending without extending
// their original expires_at. Agent lifecycle changes stop consumption, not
// task time.
func (q *Queue) ReleaseClaimed(roomID int64) (released, dropped int) {
	if q == nil || roomID <= 0 {
		return 0, 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().UTC()
	state := q.roomLocked(roomID)
	kept := state.items[:0]
	for _, item := range state.items {
		if item.Status != StatusClaimed {
			kept = append(kept, item)
			continue
		}
		if !now.Before(itemDeadline(item)) {
			dropped++
			q.recordTerminalLocked(state, item, "FAILED", "agent_stop_deadline", now)
			q.addNoteLocked(state, now, "agent_stop_expired", fmt.Sprintf("“%s”执行中断时已过期，自动抛出", item.Title))
			continue
		}
		item.Status = StatusPending
		item.ClaimedAt = nil
		released++
		q.addNoteLocked(state, now, "agent_stop_release", fmt.Sprintf("“%s”执行被中断，已退回待执行队列，原到期时间不变", item.Title))
		kept = append(kept, item)
	}
	state.items = kept
	q.sortLocked(state)
	return released, dropped
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
		q.recordTerminalLocked(state, item, "CANCELLED", "manual_cancel", now)
		q.addNoteLocked(state, now, "manual_remove", fmt.Sprintf("“%s”已从待打断队列移除", item.Title))
		return &copy, true
	}
	return nil, false
}

func (q *Queue) CompleteSimulation(roomID int64, id, reply, executionMode, planName string, userLayerVersion uint64) (SimulationResult, bool) {
	id = strings.TrimSpace(id)
	reply = strings.TrimSpace(reply)
	if roomID <= 0 || id == "" || reply == "" {
		return SimulationResult{}, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().UTC()
	state := q.roomLocked(roomID)
	q.pruneLocked(roomID, state, now)
	for index, item := range state.items {
		origin := strings.ToLower(strings.TrimSpace(item.ManualOrigin))
		if item.ID != id || item.Status != StatusClaimed || (origin != "test_simulation" && origin != "agent_input_preview") {
			continue
		}
		question := ""
		if len(item.SampleQuestions) > 0 {
			question = strings.TrimSpace(item.SampleQuestions[0])
		}
		result := SimulationResult{
			DecisionID:       id,
			Question:         question,
			Reply:            reply,
			ExecutionMode:    strings.TrimSpace(executionMode),
			PlanName:         strings.TrimSpace(planName),
			UserLayerVersion: userLayerVersion,
			CreatedAt:        now,
		}
		state.items = append(state.items[:index], state.items[index+1:]...)
		state.simulations = append([]SimulationResult{result}, state.simulations...)
		if len(state.simulations) > 20 {
			state.simulations = state.simulations[:20]
		}
		q.addNoteLocked(state, now, "test_simulation", fmt.Sprintf("测试问题“%s”已完成，仅返回文字，未播音", question))
		return result, true
	}
	return SimulationResult{}, false
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
		copy := cloneItem(*item)
		copy.Schedule = nil
		items = append(items, copy)
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
	simulations := append([]SimulationResult(nil), state.simulations...)
	return Snapshot{
		Completion: state.completion, TerminalHistory: cloneOutcomes(state.outcomes),
		RoomID:             roomID,
		GeneratedAt:        now,
		Summary:            summary,
		Queue:              items,
		CompletedCutCounts: map[string]int{"A": state.cutCounts["A"], "B": state.cutCounts["B"], "C": state.cutCounts["C"]},
		PlayedHistory:      append([]RecentAnswer(nil), state.played...),
		RecentlyAnswered:   recent,
		Notes:              notes,
		SimulationResults:  simulations,
		Capacity:           q.capacity,
		TTLSeconds:         int64(q.ttl.Seconds()),
		CooldownSeconds:    int64(q.cooldown.Seconds()),
	}
}

// Sweep advances task time even when no Agent worker or UI is touching the
// room. It is intentionally independent of Agent start/pause/stop.
func (q *Queue) Sweep() int {
	if q == nil {
		return 0
	}
	now := q.now().UTC()
	q.mu.Lock()
	defer q.mu.Unlock()
	removed := 0
	for roomID, state := range q.rooms {
		if state == nil {
			delete(q.rooms, roomID)
			continue
		}
		before := len(state.items)
		q.pruneLocked(roomID, state, now)
		removed += before - len(state.items)
	}
	return removed
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
		state = &roomState{recent: make(map[string]*RecentAnswer), cutCounts: map[string]int{"A": 0, "B": 0, "C": 0}}
		q.rooms[roomID] = state
	}
	return state
}

func (q *Queue) mergeLocked(item *Item, input Candidate, now time.Time) {
	item.MergedCount++
	item.LastSeenAt = now
	if item.Scene == "" {
		item.ExpiresAt = now.Add(q.ttl)
	}
	if input.Summary != "" {
		item.Summary = input.Summary
	}
	if input.ReplyHint != "" {
		item.ReplyHint = input.ReplyHint
	}
	if input.MissionKind != "" {
		if item.MissionKind == "" {
			item.MissionKind = input.MissionKind
		}
		if item.MissionKind == input.MissionKind && input.MissionEventCount > 0 {
			item.MissionEventCount += input.MissionEventCount
		}
		if input.MissionWindowSeconds > item.MissionWindowSeconds {
			item.MissionWindowSeconds = input.MissionWindowSeconds
		}
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
	for _, nickname := range input.Nicknames {
		appendUniqueString(&item.Nicknames, nickname, 16)
	}
	mergeInteractionDecision(&item.InteractionDecision, input.InteractionDecision)
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

func mergeInteractionDecision(current *InteractionDecision, next InteractionDecision) {
	if current == nil {
		return
	}
	for _, eventID := range next.MergedEventIDs {
		appendUniqueInt64(&current.MergedEventIDs, eventID, 32)
	}
	if next.EventValue >= current.EventValue {
		mergedIDs := append([]int64(nil), current.MergedEventIDs...)
		*current = cloneInteractionDecision(next)
		for _, eventID := range mergedIDs {
			appendUniqueInt64(&current.MergedEventIDs, eventID, 32)
		}
	}
	if current.QuestionDebt != nil && next.QuestionDebt != nil {
		if next.QuestionDebt.RepeatCount > current.QuestionDebt.RepeatCount {
			current.QuestionDebt.RepeatCount = next.QuestionDebt.RepeatCount
		}
		if next.QuestionDebt.UniqueUsers > current.QuestionDebt.UniqueUsers {
			current.QuestionDebt.UniqueUsers = next.QuestionDebt.UniqueUsers
		}
		if next.QuestionDebt.WaitingSeconds > current.QuestionDebt.WaitingSeconds {
			current.QuestionDebt.WaitingSeconds = next.QuestionDebt.WaitingSeconds
		}
		if next.QuestionDebt.CurrentPriority > current.QuestionDebt.CurrentPriority {
			current.QuestionDebt.CurrentPriority = next.QuestionDebt.CurrentPriority
		}
	}
}

func (q *Queue) sortLocked(state *roomState) {
	sort.SliceStable(state.items, func(i, j int) bool {
		left := state.items[i]
		right := state.items[j]

		// The currently claimed interaction owns the room and must remain the
		// first visible item. A newly arrived quick answer never preempts audio
		// that is already playing/returning.
		leftClaimed := left.Status == StatusClaimed
		rightClaimed := right.Status == StatusClaimed
		if leftClaimed != rightClaimed {
			return leftClaimed
		}

		// Quick answer is a business-level hard priority among waiting work. Do
		// not rely on the numeric priority value staying larger than every future
		// strategy priority.
		leftQuick := left.Status == StatusPending && strings.EqualFold(strings.TrimSpace(left.ManualAction), "quick")
		rightQuick := right.Status == StatusPending && strings.EqualFold(strings.TrimSpace(right.ManualAction), "quick")
		if leftQuick != rightQuick {
			return leftQuick
		}
		if left.PriorityClass != "" && right.PriorityClass != "" {
			if left.PriorityClass != right.PriorityClass {
				return left.PriorityClass < right.PriorityClass
			}
			l, r := left.Weight+minInt(int(q.now().Sub(left.CreatedAt)/(15*time.Second)), 20), right.Weight+minInt(int(q.now().Sub(right.CreatedAt)/(15*time.Second)), 20)
			if l != r {
				return l > r
			}
		}
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		return left.CreatedAt.Before(right.CreatedAt)
	})
}

func (q *Queue) pruneLocked(roomID int64, state *roomState, now time.Time) {
	if len(state.items) > 0 {
		kept := state.items[:0]
		for _, item := range state.items {
			// Queue TTL only governs waiting work. Once a decision has been
			// claimed, its lifetime is owned by the execution state machine and it
			// must remain visible until execution completes or explicitly releases.
			deadline := itemDeadline(item)
			if item.Status == StatusPending && !now.Before(deadline) {
				q.recordTerminalLocked(state, item, "FAILED", "waiting_deadline", now)
				q.addNoteLocked(state, now, "execution_failed", fmt.Sprintf("“%s”达到执行期限，记录失败原因并释放占用", item.Title))
				continue
			}
			if item.Status == StatusClaimed {
				claimedAt := item.ClaimedAt
				if claimedAt == nil || now.Sub(claimedAt.UTC()) >= ClaimExecutionTimeout {
					if !now.Before(deadline) {
						q.recordTerminalLocked(state, item, "FAILED", "claim_timeout_deadline", now)
						q.addNoteLocked(state, now, "claim_timeout_expired", fmt.Sprintf("“%s”执行超时且原任务已过期，自动抛出", item.Title))
						continue
					}
					item.Status = StatusPending
					item.ClaimedAt = nil
					q.addNoteLocked(state, now, "claim_timeout_release", fmt.Sprintf("“%s”执行超时，已退回待执行队列，原到期时间不变", item.Title))
				}
			}
			kept = append(kept, item)
		}
		state.items = kept
	}
	keptPlayed := state.played[:0]
	for _, r := range state.played {
		if now.Sub(r.AnsweredAt) < 10*time.Minute {
			keptPlayed = append(keptPlayed, r)
		}
	}
	state.played = keptPlayed
	if len(state.played) > 200 {
		state.played = state.played[len(state.played)-200:]
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
	item.PreparationCutPlans = append(json.RawMessage(nil), item.PreparationCutPlans...)
	item.PurposeKnowledge = append(json.RawMessage(nil), item.PurposeKnowledge...)
	item.SelectedContentPlan = append(json.RawMessage(nil), item.SelectedContentPlan...)
	item.Schedule = append(json.RawMessage(nil), item.Schedule...)
	item.Sources = append([]Source(nil), item.Sources...)
	item.SampleQuestions = append([]string(nil), item.SampleQuestions...)
	item.LinkedEventIDs = append([]int64(nil), item.LinkedEventIDs...)
	item.UserIDs = append([]string(nil), item.UserIDs...)
	item.Nicknames = append([]string(nil), item.Nicknames...)
	item.InteractionDecision = cloneInteractionDecision(item.InteractionDecision)
	if item.ClaimedAt != nil {
		copy := *item.ClaimedAt
		item.ClaimedAt = &copy
	}
	return item
}

func cloneInteractionDecision(input InteractionDecision) InteractionDecision {
	input.MergedEventIDs = append([]int64(nil), input.MergedEventIDs...)
	if input.QuestionDebt != nil {
		copyDebt := *input.QuestionDebt
		input.QuestionDebt = &copyDebt
	}
	return input
}

func cloneRecent(value RecentAnswer) RecentAnswer {
	value.PreparationCutPlans = append(json.RawMessage(nil), value.PreparationCutPlans...)
	value.SelectedContentPlan = append(json.RawMessage(nil), value.SelectedContentPlan...)
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

func (q *Queue) recordTerminalLocked(state *roomState, item *Item, status, reason string, now time.Time) {
	switch status {
	case "COMPLETED":
		state.completion.Completed++
	case "FAILED":
		state.completion.Failed++
	case "CANCELLED":
		state.completion.Cancelled++
	}
	state.outcomes = append(state.outcomes, TerminalOutcome{DecisionID: item.ID, EventIDs: append([]int64(nil), item.LinkedEventIDs...), Status: status, Reason: reason, At: now})
	if len(state.outcomes) > 200 {
		state.outcomes = state.outcomes[len(state.outcomes)-200:]
	}
}
func cloneOutcomes(input []TerminalOutcome) []TerminalOutcome {
	result := append([]TerminalOutcome(nil), input...)
	for i := range result {
		result[i].EventIDs = append([]int64(nil), result[i].EventIDs...)
	}
	return result
}
