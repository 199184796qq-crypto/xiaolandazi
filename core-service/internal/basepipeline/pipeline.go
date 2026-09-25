package basepipeline

import (
	"strings"

	"livecompanion/core/internal/model"
	"livecompanion/core/internal/questionqueue"
	"livecompanion/core/internal/roombrain"
	"livecompanion/core/internal/userblock"
)

// Signal is the free, deterministic result of processing one public-screen event.
// It never means a large model has been called or that AI billing should start.
type Signal struct {
	RoomID      int64
	EventID     int64
	UserID      string
	Content     string
	Topic       string
	IsQuestion  bool
	IsNegative  bool
	IsOrderHint bool
}

// Processor owns the always-on, no-AI-cost event processing layer.
// Collection, storage, rough question detection, clustering and order-signal
// recognition belong here and must not depend on paid AI runtime state.
type Processor struct {
	brain     *roombrain.Manager
	questions *questionqueue.Queue
	blocks    *userblock.Store
}

func New(brain *roombrain.Manager, questions *questionqueue.Queue, blocks *userblock.Store) *Processor {
	return &Processor{
		brain:     brain,
		questions: questions,
		blocks:    blocks,
	}
}

func (p *Processor) Handle(event model.RoomEvent) Signal {
	eventType := strings.ToLower(strings.TrimSpace(event.EventType))

	if eventType == "session_start" || eventType == "session_end" {
		if p.brain != nil {
			p.brain.Ingest(event)
		}
		if p.questions != nil {
			p.questions.ClearRoom(event.RoomID)
		}
		return Signal{RoomID: event.RoomID, EventID: event.ID}
	}

	if p.blocks != nil && p.blocks.IsBlocked(event.RoomID, event.UserID, event.Nickname) {
		return Signal{RoomID: event.RoomID, EventID: event.ID}
	}

	if p.brain != nil {
		p.brain.Ingest(event)
	}

	signal := Signal{
		RoomID:  event.RoomID,
		EventID: event.ID,
		UserID:  event.UserID,
		Content: event.Content,
	}

	switch eventType {
	case "chat", "comment":
		if p.brain != nil {
			classification := p.brain.Classify(event.Content)
			signal.Topic = classification.Topic
			signal.IsQuestion = classification.Question
			signal.IsNegative = classification.Negative
			if classification.Question && p.questions != nil {
				p.questions.Enqueue(event.RoomID, event.Content, classification.Topic, event.UserID)
			}
		}
		signal.IsOrderHint = roombrain.IsOrderSignalText(event.Content)
	case "order_signal":
		signal.IsOrderHint = true
	}

	return signal
}
