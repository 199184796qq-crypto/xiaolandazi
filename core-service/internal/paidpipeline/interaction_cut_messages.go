package paidpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/messagecaptain"
	"sort"
	"sync"
	"time"
)

type cutIntent struct {
	request   messagecaptain.CutRequest
	offer     messagecaptain.CutOffer
	taskID    string
	offeredAt time.Time
}
type cutMessageState struct {
	mu         sync.Mutex
	captain    *messagecaptain.Captain
	unregister func()
	sequence   uint64
	intents    map[int64]*cutIntent
	samples    map[int64][]messagecaptain.Preparation
}

func (p *Processor) SetMessageCaptain(c *messagecaptain.Captain) {
	p.cutMessages.mu.Lock()
	defer p.cutMessages.mu.Unlock()
	if p.cutMessages.unregister != nil {
		p.cutMessages.unregister()
	}
	p.cutMessages.captain = c
	if c != nil {
		p.cutMessages.unregister = c.Register("interaction-scheduler", p.handleRuntimeMessage)
	}
}
func (p *Processor) handleRuntimeMessage(_ context.Context, m messagecaptain.Message) error {
	if m.Kind != messagecaptain.CutOffered && m.Kind != messagecaptain.InteractionPrepared {
		return nil
	}
	p.cutMessages.mu.Lock()
	defer p.cutMessages.mu.Unlock()
	if m.Kind == messagecaptain.InteractionPrepared {
		var sample messagecaptain.Preparation
		if err := json.Unmarshal(m.Payload, &sample); err != nil {
			return err
		}
		if sample.ElapsedMS > 0 && sample.ElapsedMS <= 120000 {
			for _, old := range p.cutMessages.samples[m.RoomID] {
				if old.DecisionID == sample.DecisionID {
					return nil
				}
			}
			v := append(p.cutMessages.samples[m.RoomID], sample)
			if len(v) > 32 {
				v = v[len(v)-32:]
			}
			p.cutMessages.samples[m.RoomID] = v
		}
		return nil
	}
	var offer messagecaptain.CutOffer
	if err := json.Unmarshal(m.Payload, &offer); err != nil {
		return err
	}
	intent := p.cutMessages.intents[m.RoomID]
	if intent == nil || intent.request.RequestID != offer.RequestID || intent.request.Epoch != offer.Epoch || m.TaskID == "" {
		return nil
	}
	intent.offer, intent.taskID, intent.offeredAt = offer, m.TaskID, m.OccurredAt
	return nil
}
func (p *Processor) cutEpoch(roomID int64) string {
	r := p.runtime.Get(roomID)
	return fmt.Sprintf("%s:%d:%d", r.BootID, r.PlanID, r.RunGeneration)
}
func (p *Processor) preparationLeadLocked(roomID int64) int {
	v := make([]int, 0, len(p.cutMessages.samples[roomID]))
	for _, sample := range p.cutMessages.samples[roomID] {
		v = append(v, sample.ElapsedMS)
	}
	if len(v) == 0 {
		return 15000
	}
	sort.Ints(v)
	return min(30000, max(8000, v[(len(v)*95-1)/100]+3000))
}
func lowCutMission(key string) bool {
	return key == "welcome_named" || key == "welcome_batch" || key == "reply_like" || key == "reply_follow"
}
func (p *Processor) requestInteractionCuts(now time.Time) {
	if p.runtime == nil || p.decisions == nil {
		return
	}
	for _, r := range p.runtime.Snapshots() {
		if r.State != agentwork.StateWorking || r.Mode != agentwork.ModeAnchor {
			continue
		}
		busy := false
		for _, item := range p.decisions.Snapshot(r.RoomID).Queue {
			if item.Scene != "warmup" || item.Status == agentdecision.StatusClaimed {
				busy = true
				break
			}
		}
		if busy {
			continue
		}
		p.interactionMu.Lock()
		due := false
		for key, w := range p.interactionWindows[r.RoomID] {
			if !lowCutMission(key) || w == nil {
				continue
			}
			rule, ok := p.configuredRule(r.RoomID, w.TenantID, key)
			if !ok {
				continue
			}
			pruneFreshInteraction(w, rule.TTL, now)
			if interactionWindowDue(rule, w, now) {
				due = true
				break
			}
		}
		p.interactionMu.Unlock()
		if !due {
			continue
		}
		p.cutMessages.mu.Lock()
		captain := p.cutMessages.captain
		if captain == nil {
			p.cutMessages.mu.Unlock()
			return
		}
		p.cutMessages.sequence++
		request := messagecaptain.CutRequest{RequestID: fmt.Sprintf("%d:%d", r.RoomID, p.cutMessages.sequence), Epoch: p.cutEpoch(r.RoomID), LeadMS: p.preparationLeadLocked(r.RoomID)}
		p.cutMessages.intents[r.RoomID] = &cutIntent{request: request}
		p.cutMessages.mu.Unlock()
		raw, _ := json.Marshal(request)
		captain.Publish(context.Background(), messagecaptain.Message{Kind: messagecaptain.CutRequested, RoomID: r.RoomID, OccurredAt: now, Payload: raw})
	}
}
func (p *Processor) admitNearCut(roomID int64, key string, now time.Time, c *agentdecision.Candidate) bool {
	if !lowCutMission(key) || p.runtime.Get(roomID).Mode != agentwork.ModeAnchor {
		return true
	}
	p.cutMessages.mu.Lock()
	defer p.cutMessages.mu.Unlock()
	if p.cutMessages.captain == nil {
		return true
	} // Compatibility for independent consumers without mainline audio.
	intent := p.cutMessages.intents[roomID]
	if intent == nil || intent.request.Epoch != p.cutEpoch(roomID) || intent.taskID == "" || now.Sub(intent.offeredAt) > 2*time.Second {
		return false
	}
	lead := intent.offer.LeadMS - int(now.Sub(intent.offeredAt).Milliseconds())
	if lead < max(5000, intent.request.LeadMS-3000) || lead > intent.request.LeadMS+2000 {
		return false
	}
	for _, item := range p.decisions.Snapshot(roomID).Queue {
		if item.Scene != "warmup" || item.Status == agentdecision.StatusClaimed {
			return false
		}
	}
	c.PreparedTaskID, c.PreparedCutMS = intent.taskID, intent.offer.CutMS
	return true
}

// Keep observations until admission so a fresh arrival cannot extend the life
// of an old nickname. Aggregates contain only still-relevant events.
func pruneFreshInteraction(w *interactionWindow, ttl time.Duration, now time.Time) {
	if len(w.RecentEvents) == 0 {
		return
	}
	kept := w.RecentEvents[:0]
	for _, e := range w.RecentEvents {
		if now.Sub(e.OccurredAt) < ttl {
			kept = append(kept, e)
		}
	}
	w.RecentEvents = kept
	w.PendingCount = len(kept)
	w.Names = nil
	w.UserIDs = nil
	w.EventIDs = nil
	w.FirstPending = time.Time{}
	if len(kept) == 0 {
		return
	}
	for _, e := range kept {
		if w.FirstPending.IsZero() || e.OccurredAt.Before(w.FirstPending) {
			w.FirstPending = e.OccurredAt
		}
		appendInteractionName(&w.Names, e.Nickname, 12)
		appendInteractionName(&w.UserIDs, e.UserID, 24)
		appendInteractionEventID(&w.EventIDs, e.ID, 32)
	}
	e := kept[len(kept)-1]
	w.LastEventAt = e.OccurredAt
	w.LatestEventID = e.ID
	w.LatestUserID = e.UserID
	w.LatestName = e.Nickname
	w.LatestContent = e.Content
}

func (p *Processor) consumeSiblingWelcomes(roomID int64, key string, now time.Time) {
	if key != "welcome_named" && key != "welcome_batch" {
		return
	}
	for other, w := range p.interactionWindows[roomID] {
		if other == key || (other != "welcome_named" && other != "welcome_batch") || w == nil {
			continue
		}
		kept := w.RecentEvents[:0]
		for _, event := range w.RecentEvents {
			if event.OccurredAt.After(now) {
				kept = append(kept, event)
			}
		}
		w.RecentEvents = kept
		if len(kept) == 0 {
			w.PendingCount = 0
			w.FirstPending = time.Time{}
			w.Names = nil
			w.UserIDs = nil
			w.EventIDs = nil
		} else {
			rule, ok := p.configuredRule(roomID, w.TenantID, other)
			if ok {
				pruneFreshInteraction(w, rule.TTL, now)
			}
		}
	}
}
