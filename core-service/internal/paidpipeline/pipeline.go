package paidpipeline

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/events"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/roombrain"
	"livecompanion/core/internal/semantic"
	"livecompanion/core/internal/strategycenter"
)

// Processor is the paid AI boundary. Everything downstream from this processor
// is allowed to schedule large-model work, response generation, TTS and audio.
// It must never control the collector or the free base pipeline.
type Processor struct {
	purpose        purposeState
	cutMessages    cutMessageState
	scheduleMu     sync.Mutex
	schedules      map[int64]sessionSchedule
	completedSeen  map[int64]map[string]bool
	activity       map[int64]time.Time
	timeReportDue  map[int64]time.Time
	contextReader  func(int64) (string, string)
	tenantResolver func(int64) (int64, bool)
	warmupEligible func(int64) bool
	audioStarted   func(int64) time.Time

	runtime               *agentwork.Registry
	decisions             *agentdecision.Queue
	policies              *strategycenter.Store
	brain                 *roombrain.Manager
	topicResolver         semantic.TopicResolver
	sessions              *events.Store
	stageMu               sync.RWMutex
	stageAt               map[int64]time.Time
	interactionMu         sync.Mutex
	interactionWindows    map[int64]map[string]*interactionWindow
	interactionBudgetMu   sync.Mutex
	interactionBudgetUsed map[int64][]interactionBudgetUse
	questionMu            sync.Mutex
	questionDebts         map[int64]map[string]*questionDebtState
	debtMu                sync.Mutex
	debtRetryAfter        map[string]time.Time
	debtLastEventID       map[string]int64
	now                   func() time.Time
}

func New(runtime *agentwork.Registry, decisions *agentdecision.Queue, policies ...*strategycenter.Store) *Processor {
	p := &Processor{
		cutMessages: cutMessageState{intents: make(map[int64]*cutIntent), samples: make(map[int64][]messagecaptain.Preparation)},
		schedules:   make(map[int64]sessionSchedule), completedSeen: make(map[int64]map[string]bool), activity: make(map[int64]time.Time),
		timeReportDue:         make(map[int64]time.Time),
		runtime:               runtime,
		decisions:             decisions,
		stageAt:               make(map[int64]time.Time),
		interactionWindows:    make(map[int64]map[string]*interactionWindow),
		interactionBudgetUsed: make(map[int64][]interactionBudgetUse),
		questionDebts:         make(map[int64]map[string]*questionDebtState),
		debtRetryAfter:        make(map[string]time.Time),
		debtLastEventID:       make(map[string]int64),
		now:                   func() time.Time { return time.Now().UTC() },
		topicResolver:         semantic.NewTopicResolver(nil),
	}
	if len(policies) > 0 {
		p.policies = policies[0]
	}
	return p
}

func (p *Processor) SetBrain(brain *roombrain.Manager) {
	if p != nil {
		p.brain = brain
	}
}

func (p *Processor) SetSemanticEmbedder(embedder semantic.Embedder) {
	if p == nil {
		return
	}
	p.topicResolver = semantic.NewTopicResolver(embedder)
}

func (p *Processor) SetSessions(sessions *events.Store) {
	if p != nil {
		p.sessions = sessions
	}
}

func (p *Processor) setSessionStartedAt(roomID int64, startedAt time.Time) {
	if p == nil || roomID <= 0 || startedAt.IsZero() {
		return
	}
	p.stageMu.Lock()
	p.stageAt[roomID] = startedAt.UTC()
	p.stageMu.Unlock()
}

func (p *Processor) sessionStartedAt(roomID int64, fallback *time.Time) time.Time {
	if p == nil || roomID <= 0 {
		return time.Time{}
	}
	p.stageMu.RLock()
	startedAt := p.stageAt[roomID]
	p.stageMu.RUnlock()
	if !startedAt.IsZero() {
		return startedAt
	}
	if p.sessions != nil {
		if stats, err := p.sessions.GetSessionStats(context.Background(), roomID); err == nil && !stats.StartedAt.IsZero() {
			startedAt = stats.StartedAt.UTC()
			p.setSessionStartedAt(roomID, startedAt)
			return startedAt
		}
	}
	if fallback != nil && !fallback.IsZero() {
		return fallback.UTC()
	}
	return time.Time{}
}

func (p *Processor) resolveSemanticTopic(roomID int64, text, fallback string, timeout time.Duration) string {
	fallback = strings.TrimSpace(fallback)
	if strings.HasPrefix(fallback, "PURPOSE:") || strings.HasPrefix(fallback, "FAMILY:") {
		return fallback
	}
	if p == nil || p.topicResolver == nil || roomID <= 0 {
		return fallback
	}
	if timeout <= 0 {
		timeout = 120 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	resolution := p.topicResolver.Resolve(ctx, roomID, text, fallback)
	if key := strings.TrimSpace(resolution.ClusterKey); key != "" {
		return key
	}
	return fallback
}

func (p *Processor) Handle(event model.RoomEvent, signal basepipeline.Signal) {
	if p == nil || p.runtime == nil || p.decisions == nil {
		return
	}

	eventType := strings.ToLower(strings.TrimSpace(event.EventType))
	switch eventType {
	case "session_start":
		p.setSessionStartedAt(event.RoomID, event.OccurredAt)
		p.clearInteractionRoom(event.RoomID)
		return
	case "session_end":
		p.clearInteractionRoom(event.RoomID)
		_, _ = p.runtime.StopAgent(event.RoomID, agentwork.StopReasonLiveFinished)
		return
	}

	runtime := p.runtime.Get(event.RoomID)
	if runtime.State != agentwork.StateWorking {
		return
	}
	// 中控和主播模式都允许监控 Agent 扫描新事件并生成口播任务。
	// 互动类不再按单事件概率抽签，而是由时间窗调度器保证在合理窗口内发生。

	now := event.OccurredAt.UTC()
	if now.IsZero() {
		now = p.clock()
	}
	if eventType == "chat" || eventType == "comment" || signal.IsQuestion || signal.IsOrderHint || signal.IsNegative {
		p.scheduleMu.Lock()
		p.activity[event.RoomID] = now
		p.scheduleMu.Unlock()
		p.cancelWarmup(event.RoomID)
	}
	if strings.TrimSpace(signal.Content) != "" && !signal.IsNegative && !signal.IsOrderHint && (eventType == "chat" || eventType == "comment") && p.submitPurpose(event, signal) {
		return
	}
	if signal.IsNegative {
		p.enqueueDirectEvent(event, signal, "complaint", "投诉与负面反馈", now)
		return
	}
	if signal.IsQuestion {
		p.enqueueDirectEvent(event, signal, "reply_question", "回答问题", now)
		return
	}
	if signal.IsOrderHint || eventType == "order" || eventType == "order_signal" {
		p.enqueueDirectEvent(event, signal, "conversion_signal", "下单回应", now)
		return
	}
	switch eventType {
	case "chat", "comment":
		if strings.TrimSpace(signal.Content) != "" {
			p.accumulateInteraction(event, "reply_chat", signal.Topic)
		}
	case "member":
		// One entry contributes to one welcome mission, never both named and
		// batch queues. Addressing remains the existing generation policy.
		p.accumulateInteraction(event, "welcome_named", "")
	case "like":
		p.accumulateInteraction(event, "reply_like", "")
	case "follow":
		p.accumulateInteraction(event, "reply_follow", "")
	}
}

func eventSeed(event model.RoomEvent) string {
	if event.ID > 0 {
		return fmt.Sprintf("event-%d", event.ID)
	}
	return event.OccurredAt.UTC().Format(time.RFC3339Nano) + ":" + event.UserID + ":" + event.EventType
}
