package paidpipeline

import (
	"encoding/json"
	"fmt"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/strategycenter"
	"log"
	"math/rand"
	"strings"
	"time"
)

type sessionSchedule struct {
	key    string
	config strategycenter.InteractionSchedule
}

func (p *Processor) SetContextReader(fn func(int64) (string, string)) {
	p.scheduleMu.Lock()
	p.contextReader = fn
	p.scheduleMu.Unlock()
}
func (p *Processor) SetWarmupEligibility(fn func(int64) bool) {
	p.scheduleMu.Lock()
	p.warmupEligible = fn
	p.scheduleMu.Unlock()
}
func (p *Processor) SetTenantResolver(fn func(int64) (int64, bool)) {
	p.scheduleMu.Lock()
	p.tenantResolver = fn
	p.scheduleMu.Unlock()
}
func (p *Processor) schedule(roomID, tenantID int64) (strategycenter.InteractionSchedule, error) {
	runtime := p.runtime.Get(roomID)
	key := fmt.Sprintf("%s:%d:%d", runtime.BootID, runtime.PlanID, runtime.RunGeneration)
	p.scheduleMu.Lock()
	defer p.scheduleMu.Unlock()
	if c, ok := p.schedules[roomID]; ok && c.key == key {
		return c.config, nil
	}
	var cfg strategycenter.InteractionSchedule
	var err error
	if p.policies != nil {
		cfg, err = p.policies.InteractionScheduleForPlan(roomID, tenantID, runtime.PlanID)
	} else {
		cfg, err = strategycenter.LoadInteractionMother()
	}
	if err != nil {
		return cfg, err
	}
	cfg.PlanID = runtime.PlanID
	p.schedules[roomID] = sessionSchedule{key: key, config: cfg}
	return cfg, nil
}
func (p *Processor) configuredRule(roomID, tenantID int64, key string) (interactionRule, bool) {
	base, ok := defaultInteractionRules[key]
	if !ok {
		return base, false
	}
	cfg, err := p.schedule(roomID, tenantID)
	if err != nil {
		log.Printf("interaction config room=%d: %v", roomID, err)
		return base, false
	}
	e := cfg.Events[strategycenter.InteractionScene(key)]
	if !e.Enabled || e.Weight == 0 {
		return base, false
	}
	base.MinInterval = time.Duration(e.CooldownSeconds) * time.Second
	base.TTL = time.Duration(e.TTLSeconds) * time.Second
	base.MaxWait = time.Duration(e.MaxWaitSeconds) * time.Second

	base.BatchThreshold = e.BatchThreshold
	if t, ok := cfg.TimingOverrides[key]; ok {
		base.MaxWait = time.Duration(t.MaxWaitSeconds) * time.Second
		base.TTL = time.Duration(t.TTLSeconds) * time.Second
		base.BatchThreshold = t.BatchThreshold
	}
	base.MinPending = 1
	if p.policies != nil {
		factor := p.policies.InteractionPreferenceFactor(roomID, interactionPreferenceKindForMission(key))
		if factor <= 0 {
			return base, false
		}
		base.MinInterval = time.Duration(float64(base.MinInterval) / factor)
	}
	return base, true
}
func (p *Processor) decorateCandidate(roomID, tenantID int64, c *agentdecision.Candidate) bool {
	if !p.InteractionsAllowed(roomID, tenantID) {
		return false
	}
	cfg, err := p.schedule(roomID, tenantID)
	if err != nil {
		return false
	}
	scene := strategycenter.InteractionScene(c.MissionKind)
	e, ok := cfg.Events[scene]
	if !ok || !e.Enabled || e.Weight == 0 {
		return false
	}
	kind := interactionPreferenceKindForMission(c.MissionKind)
	if scene == "order" {
		kind = "conversion"
	}
	if scene == "complaint" {
		kind = "complaint"
	}
	if p.policies != nil && p.policies.InteractionPreferenceFactor(roomID, kind) <= 0 {
		return false
	}
	if p.brain != nil {
		if view, err := p.brain.Snapshot(roomID); err == nil {
			c.InteractionDecision.OnlineCount = view.Intelligence.OnlineCount
			c.InteractionDecision.UniqueChatters30s = view.Intelligence.UniqueChatters30s
			c.InteractionDecision.QuestionPressure = view.Intelligence.QuestionPressure
		}
	}
	c.Scene = scene
	c.PriorityClass = e.Priority
	c.Weight = e.Weight
	c.CooldownSeconds = e.CooldownSeconds
	c.MaxRetries = cfg.MaxRetries
	c.TTLSeconds = e.TTLSeconds
	if t, ok := cfg.TimingOverrides[c.MissionKind]; ok {
		c.TTLSeconds = t.TTLSeconds
	}
	c.OriginPlanID = cfg.PlanID
	c.Schedule, _ = json.Marshal(cfg)
	p.scheduleMu.Lock()
	reader := p.contextReader
	p.scheduleMu.Unlock()
	if reader != nil {
		c.OriginMainline, c.OriginTrackID = reader(roomID)
	}
	if scene == "product_question" {
		score := 50
		if p.policies != nil {
			score = p.policies.RoomInteractionPreferences(roomID).QuestionPreference
		}
		preset := "natural"
		if score <= 30 {
			preset = "less"
		} else if score > 70 {
			preset = "more"
		}
		c.RateLimitPerMinute = cfg.AnswerLimits[preset]
	}
	return true
}
func (p *Processor) enqueueDirectEvent(event model.RoomEvent, signal basepipeline.Signal, kind, title string, now time.Time) {
	topic := signal.Topic
	if topic == "" {
		topic = signal.Content
	}
	if topic == "" {
		topic = title
	}
	if kind == "reply_question" && !strings.HasPrefix(topic, "PURPOSE:") {
		topic = p.resolveSemanticTopic(event.RoomID, signal.Content, topic, 120*time.Millisecond)
	}
	topic = qualifiedQuestionTopic(topic, signal.Content)
	scene := strategycenter.InteractionScene(kind)
	if scene == "complaint" {
		topic = scene + ":" + topic
	}
	decision := p.questionInteractionDecision(event, signal, 30, now)
	source := ""
	if kind == "conversion_signal" {
		decision = p.conversionInteractionDecision(event, signal, now)
		source = "viewer_claim"
		if strings.EqualFold(event.EventType, "order") {
			source = "platform_order"
		}
		topic = "SIGNAL:ORDER:" + source
	}
	priority := 30
	executionMode, fixedText := "", ""
	manualAction := ""
	if signal.Clarification && strings.TrimSpace(signal.ClarifyText) != "" {
		priority = 130
		executionMode = "verbatim"
		fixedText = strings.TrimSpace(signal.ClarifyText)
		manualAction = "quick"
		topic = fmt.Sprintf("CLARIFY:%d", event.ID)
	} else if signal.ClarificationReply {
		priority = 130
		manualAction = "quick"
		topic = fmt.Sprintf("CLARIFY_REPLY:%d", event.ID)
	}
	c := agentdecision.Candidate{PurposeID: signal.PurposeID, PurposeKnowledge: signal.PurposeKnowledge, Source: agentdecision.SourceAgent, Topic: topic, Title: title, Question: signal.Content, Summary: "事件已筛选，进入现有队列，由队列管理播放顺序", MissionKind: kind, MissionEventCount: 1, Priority: priority, EventID: event.ID, UserID: event.UserID, Nicknames: []string{event.Nickname}, InteractionDecision: decision, OrderSource: source, ExecutionMode: executionMode, FixedText: fixedText, ManualAction: manualAction}
	if kind == "complaint" {
		c.Priority = 90
	}
	if kind == "conversion_signal" {
		c.Priority = 85
	}
	if !p.decorateCandidate(event.RoomID, event.TenantID, &c) {
		return
	}
	result := p.decisions.Enqueue(event.RoomID, c)
	if result.Item != nil {
		p.purpose.mu.Lock()
		if p.purpose.admitted != nil {
			p.purpose.admitted[p.purposeEventKey(event)] = true
		}
		p.purpose.mu.Unlock()
	}
}

// Cooling and spent budgets come exclusively from the queue's actual played
// history. Pending items are reservations, never successful answers.
func (p *Processor) syncPlayed(roomID int64, now time.Time) {
	if p.decisions == nil {
		return
	}
	history := p.decisions.Snapshot(roomID).PlayedHistory
	for _, r := range history {
		if r.Scene == "" {
			continue
		}
		p.scheduleMu.Lock()
		seen := p.completedSeen[roomID]
		if seen == nil {
			seen = map[string]bool{}
			p.completedSeen[roomID] = seen
		}
		already := seen[r.DecisionID]
		seen[r.DecisionID] = true
		p.scheduleMu.Unlock()
		if already {
			continue
		}
		p.interactionMu.Lock()
		if w := p.interactionWindows[roomID][r.MissionKind]; w != nil {
			w.LastEmittedAt = r.AnsweredAt
			w.EmittedCount++
		}
		p.interactionMu.Unlock()
		p.recordInteractionBudgetUse(roomID, p.interactionSignals(roomID).Heat, r.Scene == "complaint" || r.Scene == "product_question" || r.Scene == "order", r.AnsweredAt)
	}
}

func nextTimeReportDelay() time.Duration {
	// 2 hours + a random 0..120 minutes, independently chosen each round.
	return 2*time.Hour + time.Duration(rand.Intn(121))*time.Minute
}

// flushTimeReports queues a natural time-check after a randomized 2h–4h gap.
func (p *Processor) flushTimeReports(now time.Time) {
	if p.brain == nil {
		return
	}
	for _, r := range p.runtime.Snapshots() {
		if r.State != agentwork.StateWorking || r.Mode != agentwork.ModeAnchor || r.PlanID <= 0 {
			continue
		}
		if view, err := p.brain.Snapshot(r.RoomID); err != nil || view.Intelligence.OnlineCount <= 0 {
			continue
		}
		if len(p.decisions.Snapshot(r.RoomID).Queue) > 0 {
			continue
		}
		p.scheduleMu.Lock()
		due := p.timeReportDue[r.RoomID]
		if due.IsZero() {
			base := now
			if r.WorkingSince != nil {
				base = *r.WorkingSince
			}
			p.timeReportDue[r.RoomID] = base.Add(nextTimeReportDelay())
			p.scheduleMu.Unlock()
			continue
		}
		if now.Before(due) {
			p.scheduleMu.Unlock()
			continue
		}
		p.scheduleMu.Unlock()
		prefs := strategycenter.RoomInteractionPreferences{}
		if p.policies != nil {
			prefs = p.policies.RoomInteractionPreferences(r.RoomID)
		}
		if prefs.TenantID <= 0 {
			p.scheduleMu.Lock()
			resolver := p.tenantResolver
			p.scheduleMu.Unlock()
			if resolver == nil {
				continue
			}
			tenantID, ok := resolver(r.RoomID)
			if !ok || tenantID <= 0 {
				continue
			}
			prefs.TenantID = tenantID
		}
		c := agentdecision.Candidate{
			Source:      agentdecision.SourceAgent,
			Topic:       "INTERACTION:TIME_REPORT",
			Title:       "时间互动",
			MissionKind: "time_report",
			Question:    "结合当前时间自然提一句时间，不要像报时钟",
			Priority:    15,
			Scene:       "time_report",
			InteractionDecision: agentdecision.InteractionDecision{
				Handle: true, PrimaryEvent: "time_report", Reason: "每两小时一次的自然时间互动",
			},
		}
		if p.decorateCandidate(r.RoomID, prefs.TenantID, &c) {
			p.decisions.Enqueue(r.RoomID, c)
			p.scheduleMu.Lock()
			p.timeReportDue[r.RoomID] = now.Add(nextTimeReportDelay())
			p.scheduleMu.Unlock()
		}
	}
}

func (p *Processor) cancelWarmup(roomID int64) {
	for _, i := range p.decisions.Snapshot(roomID).Queue {
		if i.Scene == "warmup" && i.Status == agentdecision.StatusPending {
			p.decisions.Remove(roomID, i.ID)
		}
	}
}
func (p *Processor) flushWarmups(now time.Time) {
	if p.brain == nil {
		return
	}
	for _, r := range p.runtime.Snapshots() {
		if r.State != agentwork.StateWorking || r.Mode != agentwork.ModeAnchor || r.PlanID <= 0 {
			continue
		}
		prefs := strategycenter.RoomInteractionPreferences{}
		if p.policies != nil {
			prefs = p.policies.RoomInteractionPreferences(r.RoomID)
		}
		if prefs.TenantID <= 0 {
			p.scheduleMu.Lock()
			resolver := p.tenantResolver
			p.scheduleMu.Unlock()
			if resolver == nil {
				continue
			}
			tenantID, ok := resolver(r.RoomID)
			if !ok || tenantID <= 0 {
				continue
			}
			prefs.TenantID = tenantID
		}
		cfg, err := p.schedule(r.RoomID, prefs.TenantID)
		if err != nil {
			continue
		}
		e := cfg.Events["warmup"]
		if !e.Enabled || e.Weight == 0 {
			continue
		}
		heat := p.interactionSignals(r.RoomID).Heat
		if heat != "COLD" && heat != "WARM" {
			continue
		}
		// Do not warm up an empty room; there is nobody to hear it.
		if view, viewErr := p.brain.Snapshot(r.RoomID); viewErr != nil || view.Intelligence.OnlineCount <= 0 {
			continue
		}
		p.scheduleMu.Lock()
		last := p.activity[r.RoomID]
		p.scheduleMu.Unlock()
		if last.IsZero() && r.WorkingSince != nil {
			last = *r.WorkingSince
		}
		wait := time.Duration(e.MaxWaitSeconds) * time.Second
		if wait < 45*time.Second {
			wait = 45 * time.Second
		}
		if last.IsZero() || now.Sub(last) < wait {
			continue
		}
		snapshot := p.decisions.Snapshot(r.RoomID)
		if len(snapshot.Queue) > 0 {
			continue
		}
		cooling := false
		for _, played := range snapshot.PlayedHistory {
			if played.Scene == "warmup" && now.Sub(played.AnsweredAt) < time.Duration(e.CooldownSeconds)*time.Second {
				cooling = true
			}
		}
		if cooling {
			continue
		}
		p.scheduleMu.Lock()
		eligible := p.warmupEligible
		p.scheduleMu.Unlock()
		if eligible == nil || !eligible(r.RoomID) {
			continue
		}
		c := agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "INTERACTION:WARMUP", Title: "暖场互动", MissionKind: "warmup", Question: "围绕当前商品和讲解内容了解观众需求", Priority: 20, InteractionDecision: agentdecision.InteractionDecision{Handle: true, PrimaryEvent: "warmup", Heat: heat, Reason: "当前热度较低且一段时间没有有效提问，安排有目的的暖场"}}
		if p.decorateCandidate(r.RoomID, prefs.TenantID, &c) && strings.TrimSpace(c.OriginMainline) != "" {
			p.decisions.Enqueue(r.RoomID, c)
		}
	}
}
