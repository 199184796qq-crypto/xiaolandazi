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
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/roombrain"
	"livecompanion/core/internal/semantic"
	"livecompanion/core/internal/strategycenter"
)

// Processor is the paid AI boundary. Everything downstream from this processor
// is allowed to schedule large-model work, response generation, TTS and audio.
// It must never control the collector or the free base pipeline.
type Processor struct {
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

	if signal.IsQuestion {
		priority := 30
		if signal.IsNegative {
			priority = 70
		}
		if p.policies != nil {
			factor := p.policies.InteractionPreferenceFactor(event.RoomID, "question")
			if factor <= 0 {
				return
			}
			priority = int(float64(priority) * factor)
			if priority < 1 {
				priority = 1
			}
			if priority > 100 {
				priority = 100
			}
		}
		now := event.OccurredAt.UTC()
		if now.IsZero() {
			now = p.clock()
		}
		topic := p.resolveSemanticTopic(event.RoomID, signal.Content, signal.Topic, 120*time.Millisecond)
		semanticSignal := signal
		if strings.TrimSpace(topic) != "" {
			semanticSignal.Topic = topic
		}
		interactionDecision := p.questionInteractionDecision(event, semanticSignal, priority, now)
		if interactionDecision.QuestionDebt != nil {
			if debtPriority := int(interactionDecision.QuestionDebt.CurrentPriority + 0.5); debtPriority > priority {
				priority = debtPriority
			}
		}
		p.decisions.Enqueue(event.RoomID, agentdecision.Candidate{
			Source:              agentdecision.SourceAgent,
			Topic:               topic,
			Question:            signal.Content,
			Summary:             "观众问题已通过互动决策进入智能体候选",
			ReplyHint:           "这是一次完整口播任务。结合当前主线、打断策略、回归策略和其它生成约束，一次生成最终可播正文。",
			MissionKind:         "reply_question",
			MissionEventCount:   1,
			Priority:            priority,
			EventID:             signal.EventID,
			UserID:              signal.UserID,
			Nicknames:           []string{event.Nickname},
			InteractionDecision: interactionDecision,
		})
		return
	}

	if eventType == "chat" || eventType == "comment" {
		if strings.TrimSpace(event.Content) != "" {
			p.accumulateInteraction(event, "reply_chat", signal.Topic)
		}
		return
	}

	if eventType == "member" {
		if strings.TrimSpace(event.Nickname) != "" {
			p.accumulateInteraction(event, "welcome_named", "")
		}
		p.accumulateInteraction(event, "welcome_batch", "")
		return
	}

	if eventType == "like" {
		p.accumulateInteraction(event, "reply_like", "")
		return
	}

	if eventType == "follow" {
		p.accumulateInteraction(event, "reply_follow", "")
		return
	}

	if signal.IsOrderHint {
		priority := 85
		if p.policies != nil {
			factor := p.policies.InteractionPreferenceFactor(event.RoomID, "conversion")
			if factor <= 0 {
				return
			}
			priority = int(float64(priority) * factor)
			if priority < 1 {
				priority = 1
			}
			if priority > 100 {
				priority = 100
			}
		}
		now := event.OccurredAt.UTC()
		if now.IsZero() {
			now = p.clock()
		}
		interactionDecision := p.conversionInteractionDecision(event, signal, now)
		p.decisions.Enqueue(event.RoomID, agentdecision.Candidate{
			Source:              agentdecision.SourceAgent,
			Topic:               "SIGNAL:ORDER",
			Title:               "下单信号",
			Question:            signal.Content,
			Summary:             "检测到高价值下单信号，已通过互动决策进入保留通道",
			ReplyHint:           "优先自然回应成交信号，不能编造订单、库存、价格或承诺。",
			MissionKind:         "conversion_signal",
			MissionEventCount:   1,
			Priority:            priority,
			EventID:             signal.EventID,
			UserID:              signal.UserID,
			Nicknames:           []string{event.Nickname},
			InteractionDecision: interactionDecision,
			TTLSeconds:          45,
		})
	}
}

func eventSeed(event model.RoomEvent) string {
	if event.ID > 0 {
		return fmt.Sprintf("event-%d", event.ID)
	}
	return event.OccurredAt.UTC().Format(time.RFC3339Nano) + ":" + event.UserID + ":" + event.EventType
}
