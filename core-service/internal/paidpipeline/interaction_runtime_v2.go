package paidpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/interactionruntime"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/model"
	"log"
	"strings"
	"sync"
	"time"
)

type interactionV2State struct {
	engine        *interactionruntime.Engine
	resolver      HTTPPurposeResolver
	slots         chan struct{}
	templateSlots chan struct{}
	mu            sync.Mutex
	rooms         map[int64]interactionruntime.Scope
}

func (p *Processor) v2Policy(s interactionruntime.Scope) interactionruntime.Policy {
	policy := interactionruntime.Policy{Active: p.InteractionsAllowed(s.Room, s.Tenant), Enabled: map[string]bool{}, Cooldown: map[string]time.Duration{}}
	run := p.runtime.Get(s.Room)
	if run.WorkingSince != nil {
		policy.SessionStartedAt = *run.WorkingSince
	}
	p.scheduleMu.Lock()
	startedReader := p.audioStarted
	p.scheduleMu.Unlock()
	if startedReader != nil {
		if at := startedReader(s.Room); !at.IsZero() {
			policy.SessionStartedAt = at
		}
	}
	view := p.decisions.Snapshot(s.Room)
	for _, receipt := range view.TerminalHistory {
		if receipt.Status == "COMPLETED" && receipt.Reason == "actual_mainline_return" && !receipt.At.Before(policy.SessionStartedAt) && receipt.At.After(policy.LastEndedAt) {
			policy.LastEndedAt = receipt.At
		}
	}
	for _, answer := range view.PlayedHistory {
		if !answer.AnsweredAt.Before(policy.SessionStartedAt) && answer.AnsweredAt.After(policy.LastEndedAt) {
			policy.LastEndedAt = answer.AnsweredAt
		}
	}
	for _, item := range view.Queue {
		if item.Scene != "warmup" {
			policy.Busy = true
		}
	}
	cfg, err := p.schedule(s.Room, s.Tenant)
	if err != nil {
		policy.Active = false
		return policy
	}
	scenes := map[string]string{"semantic": "product_question", "dialogue": "product_question", "urgent": "complaint", "welcome": "welcome", "like": "like_follow", "follow": "like_follow", "order": "order", "engagement": "smalltalk", "guide": "warmup"}
	for kind, scene := range scenes {
		rule, ok := cfg.Events[scene]
		policy.Enabled[kind] = ok && rule.Enabled && rule.Weight > 0
		factor := 1.0
		if p.policies != nil {
			factor = p.policies.InteractionPreferenceFactor(s.Room, map[string]string{"semantic": "question", "dialogue": "question", "urgent": "complaint", "welcome": "welcome", "like": "engagement", "follow": "engagement", "order": "conversion", "engagement": "chat", "guide": "warmup"}[kind])
		}
		if factor <= 0 {
			policy.Enabled[kind] = false
		}
		if rule.CooldownSeconds > 0 && factor > 0 {
			policy.Cooldown[kind] = time.Duration(float64(rule.CooldownSeconds) * float64(time.Second) / factor)
		}
	}
	policy.Enabled["question"] = policy.Enabled["semantic"]
	rule := cfg.Events["smalltalk"]
	policy.Enabled["chat"] = rule.Enabled && rule.Weight > 0 && (p.policies == nil || p.policies.InteractionPreferenceFactor(s.Room, "chat") > 0)
	policy.Enabled["semantic"] = policy.Enabled["question"] || policy.Enabled["chat"]
	p.scheduleMu.Lock()
	eligible := p.warmupEligible
	p.scheduleMu.Unlock()
	if p.runtime.Mode(s.Room) != agentwork.ModeAnchor || (eligible != nil && !eligible(s.Room)) {
		policy.Enabled["guide"] = false
	}
	return policy
}

func (p *Processor) setInteractionV2(h HTTPPurposeResolver) {
	p.interactionV2.Store(&interactionV2State{engine: interactionruntime.New(), resolver: h, slots: make(chan struct{}, 4), templateSlots: make(chan struct{}, 4), rooms: map[int64]interactionruntime.Scope{}})
}
func (p *Processor) v2Scope(tenant, room int64) interactionruntime.Scope {
	r := p.runtime.Get(room)
	return interactionruntime.Scope{Tenant: tenant, Room: room, Plan: r.PlanID, Epoch: p.cutEpoch(room)}
}
func (p *Processor) handleInteractionV2(event model.RoomEvent, signal basepipeline.Signal) {
	v := p.interactionV2.Load()
	if v == nil {
		return
	}
	s := p.v2Scope(event.TenantID, event.RoomID)
	if s.Plan <= 0 {
		return
	}
	v.mu.Lock()
	v.rooms[event.RoomID] = s
	v.mu.Unlock()
	kind := strings.ToLower(strings.TrimSpace(event.EventType))
	if kind == "comment" {
		kind = "chat"
	}
	// Text questions, purchase intent and complaints are classified together by
	// the plan model. Coarse event hints must not split people into another lane.
	urgent := signal.IsNegative && kind != "chat"
	if kind != "chat" && signal.IsOrderHint && !signal.IsQuestion && !urgent {
		kind = "order"
	}
	v.engine.Add(s, interactionruntime.Event{ID: eventSeed(event), UID: event.UserID, Name: event.Nickname, Kind: kind, Text: event.Content, At: event.OccurredAt, Urgent: urgent}, p.clock())
	for _, id := range v.engine.Cancelled(s.Room) {
		p.decisions.Remove(s.Room, id)
	}
	return
}

func (p *Processor) tickInteractionV2(ctx context.Context, now time.Time) {
	v := p.interactionV2.Load()
	if v == nil {
		return
	}
	v.mu.Lock()
	scopes := make([]interactionruntime.Scope, 0, len(v.rooms))
	for _, s := range v.rooms {
		scopes = append(scopes, s)
	}
	v.mu.Unlock()
	// Include empty rooms so a genuinely cold room can get a contextual guide.
	for _, run := range p.runtime.Snapshots() {
		if run.State != agentwork.StateWorking || run.PlanID <= 0 {
			continue
		}
		found := false
		for _, s := range scopes {
			if s.Room == run.RoomID {
				found = true
				break
			}
		}
		if found {
			continue
		}
		tenant := p.policies.RoomInteractionPreferences(run.RoomID).TenantID
		if tenant <= 0 {
			p.scheduleMu.Lock()
			resolve := p.tenantResolver
			p.scheduleMu.Unlock()
			if resolve != nil {
				tenant, _ = resolve(run.RoomID)
			}
		}
		if tenant > 0 {
			scopes = append(scopes, p.v2Scope(tenant, run.RoomID))
		}
	}
	for _, s := range scopes {
		if !p.runtime.IsWorking(s.Room) || p.cutEpoch(s.Room) != s.Epoch {
			v.engine.Reset(s.Room)
			v.mu.Lock()
			delete(v.rooms, s.Room)
			v.mu.Unlock()
			continue
		}
		v.mu.Lock()
		v.rooms[s.Room] = s
		v.mu.Unlock()
		liveDecisions := map[string]bool{}
		for _, item := range p.decisions.Snapshot(s.Room).Queue {
			liveDecisions[item.ID] = true
		}
		v.engine.ReconcilePlayback(s, liveDecisions, now)
		// Select observes/creates room state before Jobs, including zero-event time.
		for _, id := range v.engine.Cancelled(s.Room) {
			p.decisions.Remove(s.Room, id)
		}
		for _, t := range v.engine.Snapshot(s.Room) {
			if (t.State == "selected" || t.State == "playing") && t.Decision != "" {
				if _, exists := p.decisions.Get(s.Room, t.Decision); !exists {
					v.engine.Lost(s.Room, t.Decision, now)
				}
			}
		}
		v.engine.SetPolicy(s, p.v2Policy(s), now)
		p.publishInteractionStats(s.Room, now)
		if !p.InteractionsAllowed(s.Room, s.Tenant) {
			continue
		}
		queue := p.decisions.Snapshot(s.Room).Queue
		if len(queue) > 0 {
			// Only our one unplayed item may be superseded by an urgent ready
			// answer. Manual items and playing audio retain their ownership.
			var envelope struct {
				Protocol string `json:"protocol"`
			}
			json.Unmarshal(queue[0].PurposeKnowledge, &envelope)
			if len(queue) != 1 || queue[0].PlaybackTaskID != "" || queue[0].Status == agentdecision.StatusClaimed || envelope.Protocol != interactionruntime.Protocol {
				continue
			}
		}
		t := v.engine.Select(s, now)
		if t == nil {
			continue
		}
		for _, id := range v.engine.Cancelled(s.Room) {
			p.decisions.Remove(s.Room, id)
		}
		mission := map[string]string{"semantic": "reply_question", "dialogue": "reply_question", "urgent": "complaint", "welcome": "welcome_named", "like": "reply_like", "follow": "reply_follow", "order": "conversion_signal", "engagement": "reply_chat", "guide": "warmup"}[t.Work.Kind]
		if t.Work.WelcomeQuota && t.Work.Count > 1 {
			mission = "welcome_batch"
		}
		if t.Content.Intent.Type == "投诉" {
			mission = "complaint"
		}
		if t.Work.Kind == "semantic" && !t.Content.Intent.Question {
			mission = "reply_chat"
		}
		names := []string{}
		uid := ""
		for _, person := range t.Work.People {
			names = append(names, person.Name)
			if uid == "" {
				uid = person.UID
			}
		}
		ttl := int(t.Work.Expires.Sub(now).Seconds())
		if ttl < 1 {
			v.engine.Bind(t.Work, "", now)
			continue
		}
		unique := 0
		if p.brain != nil {
			if view, err := p.brain.Snapshot(s.Room); err == nil {
				unique = view.Intelligence.UniqueChatters30s
			}
		}
		candidate := agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: t.Work.ID, Title: "互动 · " + mission, Question: t.Work.Text, MissionKind: mission, MissionEventCount: t.Work.Count, UserID: uid, Nicknames: names, Priority: 50 + t.Work.Priority*10, EventExpiresAt: t.Work.Expires, TTLSeconds: ttl, PurposeKnowledge: t.Content.Knowledge, OriginPlanID: s.Plan, Scene: "interaction_v2", InteractionDecision: agentdecision.InteractionDecision{Handle: true, UniqueChatters30s: unique, PrimaryEvent: t.Work.Kind, DeadlineAt: t.Work.Expires, BudgetAllowed: true}}
		result := p.decisions.Enqueue(s.Room, candidate)
		id := ""
		if result.Item != nil {
			id = result.Item.ID
		}
		v.engine.Bind(t.Work, id, now)
	}
	free := cap(v.slots) - len(v.slots)
	// Expiry and entitlement maintenance must run even when every model slot
	// is busy. A stale welcome cannot retain a claimed queue item until then.
	jobs := v.engine.Batches(now, max(0, free))
	jobs = append(jobs, v.engine.TemplateBatches(now, cap(v.templateSlots)-len(v.templateSlots))...)
	for _, s := range scopes {
		for _, id := range v.engine.Cancelled(s.Room) {
			p.decisions.Remove(s.Room, id)
		}
	}
	for _, job := range jobs {
		first := job.Works[0]
		if !p.runtime.IsWorking(first.Scope.Room) || p.cutEpoch(first.Scope.Room) != first.Scope.Epoch {
			continue
		}
		p.scheduleMu.Lock()
		reader := p.contextReader
		p.scheduleMu.Unlock()
		if reader != nil {
			mainline, _ := reader(first.Scope.Room)
			for i := range job.Works {
				job.Works[i].Mainline = strings.TrimSpace(mainline + "\n" + job.Works[i].Mainline)
			}
		}
		slots := v.slots
		if first.Template != "" {
			slots = v.templateSlots
		}
		slots <- struct{}{}
		go func(batch interactionruntime.Batch, slots chan struct{}) {
			defer func() { <-slots }()
			deadline := batch.Works[0].Expires
			for _, w := range batch.Works {
				if w.Expires.After(deadline) {
					deadline = w.Expires
				}
			}
			workCtx, cancel := context.WithDeadline(ctx, deadline)
			defer cancel()
			results, err := v.resolver.prepareBatchV2(workCtx, batch)
			if err != nil {
				log.Printf("interaction v2 batch room=%d batch=%s count=%d failed=%v", batch.Works[0].Scope.Room, batch.ID, len(batch.Works), err)
			}
			for i, w := range batch.Works {
				if !p.runtime.IsWorking(w.Scope.Room) || p.cutEpoch(w.Scope.Room) != w.Scope.Epoch {
					v.engine.Accept(w, interactionruntime.Content{}, fmt.Errorf("interaction scope changed"), p.clock())
					continue
				}
				itemErr := err
				var content interactionruntime.Content
				if err == nil {
					itemErr = results[i].Err
					content = results[i].Content
				}
				if itemErr != nil {
					log.Printf("interaction v2 result room=%d task=%s failed=%v", w.Scope.Room, w.ID, itemErr)
				}
				v.engine.Accept(w, content, itemErr, p.clock())
			}
		}(job, slots)
	}
	return
}
func (p *Processor) interactionV2Receipt(m messagecaptain.Message) bool {
	if m.Kind != "interaction.v2.playback" {
		return false
	}
	v := p.interactionV2.Load()
	if v == nil {
		return true
	}
	var r struct {
		Decision string    `json:"decision_id"`
		Status   string    `json:"status"`
		At       time.Time `json:"at"`
	}
	if json.Unmarshal(m.Payload, &r) != nil {
		return true
	}
	if r.At.IsZero() {
		r.At = m.OccurredAt
	}
	item, exists := p.decisions.Get(m.RoomID, r.Decision)
	if !exists || item.Status != agentdecision.StatusClaimed || !p.runtime.IsWorking(m.RoomID) || m.RunID != p.cutEpoch(m.RoomID) || m.TaskID == "" || item.PlaybackTaskID != m.TaskID {
		return true
	}
	var envelope struct {
		Epoch string `json:"epoch"`
	}
	json.Unmarshal(item.PurposeKnowledge, &envelope)
	if envelope.Epoch != "" && envelope.Epoch != m.RunID {
		return true
	}
	tenant := int64(0)
	if p.policies != nil {
		tenant = p.policies.RoomInteractionPreferences(m.RoomID).TenantID
	}
	if tenant <= 0 {
		p.scheduleMu.Lock()
		resolve := p.tenantResolver
		p.scheduleMu.Unlock()
		if resolve != nil {
			tenant, _ = resolve(m.RoomID)
		}
	}
	v.mu.Lock()
	scope, known := v.rooms[m.RoomID]
	v.mu.Unlock()
	if !known {
		scope = p.v2Scope(tenant, m.RoomID)
	}
	if scope.Epoch != m.RunID {
		return true
	}
	v.engine.SetPolicy(scope, p.v2Policy(scope), r.At)
	v.engine.Receipts(m.RoomID, r.Decision, r.Status, r.At)
	return true
}
func (p *Processor) reserveAudioV2(m messagecaptain.Message) error {
	var req struct {
		Decision string    `json:"decision_id"`
		Duration int       `json:"duration_ms"`
		Start    time.Time `json:"start_at"`
	}
	if json.Unmarshal(m.Payload, &req) != nil {
		return fmt.Errorf("invalid audio reservation")
	}
	item, ok := p.decisions.Get(m.RoomID, req.Decision)
	if !ok {
		return fmt.Errorf("interaction decision removed")
	}
	if item.MissionKind == "time_report" {
		if item.TimeReportValidUntil.IsZero() {
			return fmt.Errorf("time report is missing original time validity")
		}
		start := req.Start
		if start.IsZero() || start.Before(p.now()) {
			start = p.now()
		}
		if req.Duration <= 0 || !start.Add(time.Duration(req.Duration)*time.Millisecond).Before(item.TimeReportValidUntil) {
			return fmt.Errorf("time report would finish after its original time validity")
		}
	}
	v := p.interactionV2.Load()
	if v == nil {
		return nil
	}
	var protocol struct {
		Protocol string `json:"protocol"`
	}
	json.Unmarshal(item.PurposeKnowledge, &protocol)
	if protocol.Protocol != interactionruntime.Protocol {
		return nil
	}
	var scopeEnvelope struct {
		Tenant int64  `json:"tenant_id"`
		Epoch  string `json:"epoch"`
	}
	json.Unmarshal(item.PurposeKnowledge, &scopeEnvelope)
	if scopeEnvelope.Epoch != p.cutEpoch(m.RoomID) {
		return fmt.Errorf("interaction run changed")
	}
	v.engine.SetPolicy(p.v2Scope(scopeEnvelope.Tenant, m.RoomID), p.v2Policy(p.v2Scope(scopeEnvelope.Tenant, m.RoomID)), p.clock())
	if !v.engine.ReserveAudio(m.RoomID, req.Decision, req.Duration, req.Start) {
		for _, id := range v.engine.Cancelled(m.RoomID) {
			p.decisions.Remove(m.RoomID, id)
		}
		return fmt.Errorf("interaction actual audio budget/deadline unavailable")
	}
	return nil
}
