package paidpipeline

import (
	"fmt"
	"log"
	"strings"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/roombrain"
	"livecompanion/core/internal/strategycenter"
)

// Processor is the paid AI boundary. Everything downstream from this processor
// is allowed to schedule large-model work, response generation, TTS and audio.
// It must never control the collector or the free base pipeline.
type Processor struct {
	runtime   *agentwork.Registry
	decisions *agentdecision.Queue
	policies  *strategycenter.Store
	brain     *roombrain.Manager
}

func New(runtime *agentwork.Registry, decisions *agentdecision.Queue, policies ...*strategycenter.Store) *Processor {
	p := &Processor{
		runtime:   runtime,
		decisions: decisions,
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

func (p *Processor) Handle(event model.RoomEvent, signal basepipeline.Signal) {
	if p == nil || p.runtime == nil || p.decisions == nil {
		return
	}

	eventType := strings.ToLower(strings.TrimSpace(event.EventType))
	switch eventType {
	case "session_start":
		p.decisions.ClearRoom(event.RoomID)
		return
	case "session_end":
		p.decisions.ClearRoom(event.RoomID)
		_, _ = p.runtime.StopAgent(event.RoomID, agentwork.StopReasonLiveFinished)
		return
	}

	runtime := p.runtime.Get(event.RoomID)
	if runtime.State != agentwork.StateWorking {
		return
	}
	// 中控和主播模式都允许监控 Agent 扫描新事件并生成待打断候选。
	// 两种模式只在后续播音/主线衔接方式上不同，不能阻断问题扫描装载。
	signals := strategycenter.Signals{}
	if p.brain != nil {
		if view, err := p.brain.Snapshot(event.RoomID); err == nil {
			signals.Entries30s = view.Intelligence.Entries30s
			signals.Likes30s = view.Intelligence.Likes30s
			signals.Follows30s = view.Intelligence.Follows30s
			signals.Heat = view.Intelligence.Heat
		}
	}
	trigger := func(key string) bool {
		if p.policies == nil {
			return true
		}
		decision := p.policies.Trigger(event.TenantID, "interaction", key, signals, eventSeed(event))
		log.Printf("interaction strategy room=%d event=%d type=%s key=%s probability=%d roll=%d triggered=%t entries30s=%d heat=%s", event.RoomID, event.ID, eventType, key, decision.Probability, decision.Roll, decision.Triggered, signals.Entries30s, signals.Heat)
		return decision.Triggered
	}

	if signal.IsQuestion {
		if !trigger("reply_chat") {
			return
		}
		priority := 30
		if signal.IsNegative {
			priority = 70
		}
		p.decisions.Enqueue(event.RoomID, agentdecision.Candidate{
			Source:   agentdecision.SourceAgent,
			Topic:    signal.Topic,
			Question: signal.Content,
			Summary:  "观众问题已进入智能体候选，等待进一步判断",
			Priority: priority,
			EventID:  signal.EventID,
			UserID:   signal.UserID,
		})
		return
	}

	if eventType == "chat" || eventType == "comment" {
		if strings.TrimSpace(event.Content) != "" && trigger("reply_chat") {
			p.decisions.Enqueue(event.RoomID, agentdecision.Candidate{
				Source: agentdecision.SourceAgent, Topic: "INTERACTION:CHAT", Question: event.Content,
				Summary: "普通弹幕互动候选", Priority: 18, EventID: event.ID, UserID: event.UserID, TTLSeconds: 35,
			})
		}
		return
	}

	if eventType == "member" {
		key := "welcome_named"
		if signals.Entries30s >= 20 {
			key = "welcome_batch"
		}
		if trigger(key) {
			question := "欢迎刚进入直播间的新朋友"
			if key == "welcome_named" && strings.TrimSpace(event.Nickname) != "" {
				question = "自然欢迎新进入直播间的 " + strings.TrimSpace(event.Nickname)
			}
			p.decisions.Enqueue(event.RoomID, agentdecision.Candidate{
				Source: agentdecision.SourceAgent, Topic: "INTERACTION:WELCOME:" + key, Question: question,
				Summary: "新人欢迎互动候选", Priority: 12, EventID: event.ID, UserID: event.UserID, TTLSeconds: 20,
			})
		}
		return
	}

	if eventType == "like" && trigger("reply_like") {
		p.decisions.Enqueue(event.RoomID, agentdecision.Candidate{
			Source: agentdecision.SourceAgent, Topic: "INTERACTION:LIKE", Question: "自然感谢大家刚才的点赞支持",
			Summary: "点赞互动候选，不逐个报名字", Priority: 8, EventID: event.ID, UserID: event.UserID, TTLSeconds: 20,
		})
		return
	}

	if eventType == "follow" && trigger("reply_follow") {
		p.decisions.Enqueue(event.RoomID, agentdecision.Candidate{
			Source: agentdecision.SourceAgent, Topic: "INTERACTION:FOLLOW", Question: "自然感谢刚刚新增的关注",
			Summary: "关注互动候选", Priority: 10, EventID: event.ID, UserID: event.UserID, TTLSeconds: 25,
		})
		return
	}

	if signal.IsOrderHint {
		p.decisions.Enqueue(event.RoomID, agentdecision.Candidate{
			Source:     agentdecision.SourceAgent,
			Topic:      "SIGNAL:ORDER",
			Title:      "下单信号",
			Question:   signal.Content,
			Summary:    "检测到高价值下单信号，等待智能体判断是否插播",
			Priority:   85,
			EventID:    signal.EventID,
			UserID:     signal.UserID,
			TTLSeconds: 45,
		})
	}
}

func eventSeed(event model.RoomEvent) string {
	if event.ID > 0 {
		return fmt.Sprintf("event-%d", event.ID)
	}
	return event.OccurredAt.UTC().Format(time.RFC3339Nano) + ":" + event.UserID + ":" + event.EventType
}
