package paidpipeline

import (
	"strings"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/model"
)

// Processor is the paid AI boundary. Everything downstream from this processor
// is allowed to schedule large-model work, response generation, TTS and audio.
// It must never control the collector or the free base pipeline.
type Processor struct {
	runtime   *agentwork.Registry
	decisions *agentdecision.Queue
}

func New(runtime *agentwork.Registry, decisions *agentdecision.Queue) *Processor {
	return &Processor{
		runtime:   runtime,
		decisions: decisions,
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
		_, _ = p.runtime.Set(event.RoomID, agentwork.StateStopped)
		return
	}

	runtime := p.runtime.Get(event.RoomID)
	if runtime.State != agentwork.StateWorking {
		return
	}
	// 中控模式只保留免费采集/聚类，由人工决定是否回答；主播模式才允许
	// 付费智能体根据事件自动生成待打断候选。
	if runtime.Mode == agentwork.ModeControl {
		return
	}

	if signal.IsQuestion {
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
