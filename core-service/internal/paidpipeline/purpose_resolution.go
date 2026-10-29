package paidpipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/model"
)

type PurposeResolver interface {
	Resolve(context.Context, model.RoomEvent, int64) (messagecaptain.PurposeResult, error)
}
type purposeJob struct {
	event  model.RoomEvent
	signal basepipeline.Signal
	epoch  string
	plan   int64
}
type purposeState struct {
	mu       sync.Mutex
	epochs   map[int64]string
	resolver PurposeResolver
	jobs     chan purposeJob
	seen     map[string]bool
	admitted map[string]bool
	pending  map[string]bool
	results  map[string]messagecaptain.PurposeResult
}

type HTTPPurposeResolver struct {
	URL, Token string
	Client     *http.Client
}

func (h HTTPPurposeResolver) Resolve(ctx context.Context, event model.RoomEvent, plan int64) (messagecaptain.PurposeResult, error) {
	input, _ := json.Marshal(map[string]any{"tenant_id": event.TenantID, "room_id": event.RoomID, "plan_id": plan, "user_id": event.UserID, "nickname": event.Nickname, "text": event.Content})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(h.URL, "/")+"/internal/v1/interaction-purpose/resolve", bytes.NewReader(input))
	if err != nil {
		return messagecaptain.PurposeResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Core-Token", h.Token)
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return messagecaptain.PurposeResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return messagecaptain.PurposeResult{}, fmt.Errorf("purpose resolve http %d", response.StatusCode)
	}
	var result messagecaptain.PurposeResult
	err = json.NewDecoder(http.MaxBytesReader(nil, response.Body, 64<<10)).Decode(&result)
	return result, err
}

func (p *Processor) SetPurposeResolver(resolver PurposeResolver) {
	p.purpose.mu.Lock()
	p.purpose.resolver = resolver
	if p.purpose.jobs != nil {
		p.purpose.mu.Unlock()
		return
	}
	p.purpose.jobs = make(chan purposeJob, 64)
	p.purpose.seen = map[string]bool{}
	p.purpose.admitted = map[string]bool{}
	p.purpose.pending = map[string]bool{}
	p.purpose.results = map[string]messagecaptain.PurposeResult{}
	p.purpose.mu.Unlock()
	for i := 0; i < 4; i++ {
		go p.runPurposeWorker()
	}
}
func (p *Processor) purposeEventKey(event model.RoomEvent) string {
	return fmt.Sprintf("%d:%s:%s", event.RoomID, p.cutEpoch(event.RoomID), eventSeed(event))
}
func (p *Processor) submitPurpose(event model.RoomEvent, signal basepipeline.Signal) bool {
	p.purpose.mu.Lock()
	defer p.purpose.mu.Unlock()
	if p.purpose.resolver == nil {
		return false
	}
	epoch := p.cutEpoch(event.RoomID)
	if p.purpose.epochs == nil {
		p.purpose.epochs = map[int64]string{}
	}
	if old := p.purpose.epochs[event.RoomID]; old != "" && old != epoch {
		prefix := fmt.Sprintf("%d:%s:", event.RoomID, old)
		for key := range p.purpose.seen {
			if strings.HasPrefix(key, prefix) {
				delete(p.purpose.seen, key)
				delete(p.purpose.admitted, key)
				delete(p.purpose.pending, key)
				delete(p.purpose.results, key)
			}
		}
	}
	p.purpose.epochs[event.RoomID] = epoch
	key := p.purposeEventKey(event)
	if p.purpose.seen[key] {
		return true
	}
	if len(p.purpose.seen) >= 4096 {
		// Use the existing nonblocking heuristic/pool path for new events.
		// Never erase admitted/in-flight identities merely to make room.
		return false
	}
	job := purposeJob{event: event, signal: signal, epoch: p.cutEpoch(event.RoomID), plan: p.runtime.Get(event.RoomID).PlanID}
	select {
	case p.purpose.jobs <- job:
		p.purpose.seen[key] = true
		p.purpose.pending[key] = true
		return true
	default:
		return false
	}
}
func (p *Processor) runPurposeWorker() {
	for job := range p.purpose.jobs {
		if p.cutEpoch(job.event.RoomID) != job.epoch || !p.runtime.IsWorking(job.event.RoomID) {
			continue
		}
		p.purpose.mu.Lock()
		resolver := p.purpose.resolver
		p.purpose.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		result, err := resolver.Resolve(ctx, job.event, job.plan)
		cancel()
		if err != nil {
			log.Printf("purpose resolution fallback room=%d event=%d error=%v", job.event.RoomID, job.event.ID, err)
			result = messagecaptain.PurposeResult{Question: job.signal.IsQuestion, Relevant: job.signal.IsQuestion, Topic: job.signal.Topic, Source: "heuristic_fallback"}
		}
		result.Event, result.Epoch = job.event, job.epoch
		if !result.Relevant {
			result.Question = false
		}
		p.cutMessages.mu.Lock()
		captain := p.cutMessages.captain
		p.cutMessages.mu.Unlock()
		if captain != nil {
			raw, _ := json.Marshal(result)
			captain.Publish(context.Background(), messagecaptain.Message{Kind: messagecaptain.PurposeResolved, RoomID: job.event.RoomID, Payload: raw})
		}
	}
}
func (p *Processor) acceptPurpose(msg messagecaptain.Message) error {
	var result messagecaptain.PurposeResult
	if err := json.Unmarshal(msg.Payload, &result); err != nil {
		return err
	}
	if p.cutEpoch(msg.RoomID) != result.Epoch || !p.runtime.IsWorking(msg.RoomID) {
		return nil
	}
	key := p.purposeEventKey(result.Event)
	p.purpose.mu.Lock()
	delete(p.purpose.pending, key)
	if p.purpose.results != nil {
		p.purpose.results[key] = result
	}
	p.purpose.mu.Unlock()
	if !p.InteractionsAllowed(msg.RoomID, result.Event.TenantID) {
		return nil
	}
	signal := basepipeline.Signal{RoomID: msg.RoomID, EventID: result.Event.ID, Content: result.Event.Content, Topic: result.Topic, IsQuestion: result.Question, PurposeID: result.PurposeID, PurposeKnowledge: result.Knowledge, Clarification: result.Ambiguous, ClarifyText: result.Clarify, ClarificationReply: result.ClarificationReply}
	if result.Question {
		p.enqueueDirectEvent(result.Event, signal, "reply_question", "回答问题", p.clock())
	} else if result.Relevant {
		p.accumulateInteraction(result.Event, "reply_chat", result.Topic)
	}
	return nil
}

func (p *Processor) purposeAlreadyAdmitted(roomID, eventID int64) bool {
	if eventID <= 0 {
		return false
	}
	key := p.purposeEventKey(model.RoomEvent{RoomID: roomID, ID: eventID})
	p.purpose.mu.Lock()
	defer p.purpose.mu.Unlock()
	return p.purpose.admitted[key]
}

func (p *Processor) purposeEventPending(roomID, eventID int64) bool {
	key := p.purposeEventKey(model.RoomEvent{RoomID: roomID, ID: eventID})
	p.purpose.mu.Lock()
	defer p.purpose.mu.Unlock()
	return p.purpose.pending[key]
}
func (p *Processor) purposeKnowledgeFor(roomID, eventID int64) messagecaptain.PurposeResult {
	key := p.purposeEventKey(model.RoomEvent{RoomID: roomID, ID: eventID})
	p.purpose.mu.Lock()
	defer p.purpose.mu.Unlock()
	return p.purpose.results[key]
}
