package paidpipeline

import (
	"encoding/json"
	"fmt"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/strategycenter"
	"math/rand"
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
	if c.MissionKind == "time_report" {
		// Time reports are mandatory: do not depend on the warmup toggle.
		e = strategycenter.InteractionEventRule{
			Name: "时间互动", Enabled: true, Priority: "P0", Weight: 100,
			TTLSeconds: 20 * 60, MaxWaitSeconds: 20 * 60,
		}
		ok = true
	}
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
	if c.MissionKind != "time_report" && p.policies != nil && p.policies.InteractionPreferenceFactor(roomID, kind) <= 0 {
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

func nextTimeReportDelay() time.Duration {
	// 2 hours plus a random -30..+120 minutes, independently each round.
	return 2*time.Hour + time.Duration(rand.Intn(151)-30)*time.Minute
}
