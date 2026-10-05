package paidpipeline

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/strategycenter"
	"livecompanion/core/internal/timeline"
)

type interactionRule struct {
	Key            string
	Topic          string
	Title          string
	Priority       int
	MinInterval    time.Duration
	MaxWait        time.Duration
	BatchThreshold int
	MinPending     int
	TTL            time.Duration
}

type interactionWindow struct {
	TenantID              int64
	RoomID                int64
	Key                   string
	PendingCount          int
	TotalEvents           int64
	EmittedCount          int64
	LastMissionEventCount int
	FirstPending          time.Time
	LastEventAt           time.Time
	LastEmittedAt         time.Time
	LatestEventID         int64
	LatestUserID          string
	LatestName            string
	LatestContent         string
	Names                 []string
	EventIDs              []int64
	UserIDs               []string
	LastEventValue        float64
	LastValueLevel        string
	LastDecisionReason    string
	LastBudgetLevel       string
	LastBudgetAllowed     bool
	TopicHint             string
	RetryAfter            time.Time
}

type interactionBudgetUse struct {
	At        time.Time
	HighValue bool
}

type interactionBudget struct {
	Level          string
	MaxMissions    int
	ReservedHigh   int
	RemainingSlots int
}

type questionDebtState struct {
	FirstSeenAt   time.Time
	LastSeenAt    time.Time
	RepeatCount   int
	Users         map[string]struct{}
	BusinessValue float64
}

const interactionBudgetWindow = 30 * time.Second

var defaultInteractionRules = map[string]interactionRule{
	"reply_chat": {
		Key: "reply_chat", Topic: "INTERACTION:CHAT", Title: "回复弹幕",
		Priority: 18, MinInterval: 12 * time.Second, MaxWait: 12 * time.Second,
		BatchThreshold: 1, MinPending: 1, TTL: 40 * time.Second,
	},
	"reply_follow": {
		Key: "reply_follow", Topic: "INTERACTION:FOLLOW", Title: "回应关注",
		Priority: 14, MinInterval: 45 * time.Second, MaxWait: 45 * time.Second,
		BatchThreshold: 3, MinPending: 1, TTL: 70 * time.Second,
	},
	"reply_like": {
		Key: "reply_like", Topic: "INTERACTION:LIKE", Title: "回应点赞",
		Priority: 12, MinInterval: 60 * time.Second, MaxWait: 60 * time.Second,
		BatchThreshold: 30, MinPending: 1, TTL: 90 * time.Second,
	},
	"welcome_named": {
		Key: "welcome_named", Topic: "INTERACTION:WELCOME:welcome_named", Title: "点名欢迎",
		Priority: 13, MinInterval: 90 * time.Second, MaxWait: 10 * time.Second,
		BatchThreshold: 0, MinPending: 1, TTL: 40 * time.Second,
	},
	"welcome_batch": {
		Key: "welcome_batch", Topic: "INTERACTION:WELCOME:welcome_batch", Title: "打包欢迎",
		Priority: 12, MinInterval: 60 * time.Second, MaxWait: 35 * time.Second,
		BatchThreshold: 5, MinPending: 2, TTL: 60 * time.Second,
	},
}

func (p *Processor) Run(ctx context.Context) {
	if p == nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			p.FlushInteractionWindows(now.UTC())
			p.FlushDebtBacklog(now.UTC())
		}
	}
}

func (p *Processor) interactionSignals(roomID int64) strategycenter.Signals {
	if p == nil || p.brain == nil || roomID <= 0 {
		return strategycenter.Signals{}
	}
	view, err := p.brain.Snapshot(roomID)
	if err != nil {
		return strategycenter.Signals{}
	}
	return strategycenter.Signals{
		Entries30s: view.Intelligence.Entries30s,
		Likes30s:   view.Intelligence.Likes30s,
		Follows30s: view.Intelligence.Follows30s,
		Heat:       view.Intelligence.Heat,
	}
}

func (p *Processor) interactionRuleWeight(roomID, tenantID int64, key string) (configured, effective int, enabled bool) {
	if p == nil || p.policies == nil {
		return 100, 100, true
	}
	configured, effective, enabled = p.policies.RuleWeight(tenantID, "interaction", key, p.interactionSignals(roomID))
	if !enabled {
		return configured, 0, false
	}
	effective = p.policies.AdjustInteractionWeight(roomID, key, effective)
	return configured, effective, effective > 0
}

func interactionEffectivePriority(basePriority, effectiveWeight int) int {
	if effectiveWeight < 0 {
		effectiveWeight = 0
	}
	if effectiveWeight > 100 {
		effectiveWeight = 100
	}
	return basePriority + effectiveWeight/5
}

func normalizedInteractionHeat(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "COLD":
		return "COLD"
	case "BUSY":
		return "BUSY"
	case "HOT":
		return "HOT"
	case "OVERHEATED":
		return "OVERHEATED"
	default:
		return "WARM"
	}
}

func interactionBudgetForHeat(raw string) interactionBudget {
	switch normalizedInteractionHeat(raw) {
	case "COLD":
		return interactionBudget{Level: "HIGH", MaxMissions: 6, ReservedHigh: 0, RemainingSlots: 6}
	case "BUSY":
		return interactionBudget{Level: "NORMAL", MaxMissions: 4, ReservedHigh: 1, RemainingSlots: 4}
	case "HOT":
		return interactionBudget{Level: "LOW", MaxMissions: 3, ReservedHigh: 1, RemainingSlots: 3}
	case "OVERHEATED":
		return interactionBudget{Level: "PROTECTED", MaxMissions: 2, ReservedHigh: 1, RemainingSlots: 2}
	default:
		return interactionBudget{Level: "NORMAL", MaxMissions: 5, ReservedHigh: 1, RemainingSlots: 5}
	}
}

func (p *Processor) interactionBudgetDecision(roomID int64, heat string, highValue bool, now time.Time, commit bool) (interactionBudget, bool, string) {
	budget := interactionBudgetForHeat(heat)
	if p == nil || roomID <= 0 {
		return budget, true, "互动预算未限制"
	}
	if now.IsZero() {
		now = p.clock()
	}
	cutoff := now.Add(-interactionBudgetWindow)
	p.interactionBudgetMu.Lock()
	uses := p.interactionBudgetUsed[roomID]
	kept := uses[:0]
	normalUsed := 0
	for _, use := range uses {
		if use.At.Before(cutoff) {
			continue
		}
		kept = append(kept, use)
		if !use.HighValue {
			normalUsed++
		}
	}
	uses = kept
	allowed := false
	if highValue {
		allowed = len(uses) < budget.MaxMissions
	} else {
		normalCapacity := budget.MaxMissions - budget.ReservedHigh
		if normalCapacity < 0 {
			normalCapacity = 0
		}
		allowed = len(uses) < budget.MaxMissions && normalUsed < normalCapacity
	}
	if allowed && commit {
		uses = append(uses, interactionBudgetUse{At: now, HighValue: highValue})
	}
	p.interactionBudgetUsed[roomID] = uses
	budget.RemainingSlots = budget.MaxMissions - len(uses)
	if budget.RemainingSlots < 0 {
		budget.RemainingSlots = 0
	}
	p.interactionBudgetMu.Unlock()
	if allowed {
		if highValue && budget.ReservedHigh > 0 {
			return budget, true, "当前事件价值高，允许占用高价值保留通道"
		}
		return budget, true, fmt.Sprintf("当前互动预算允许，本窗口剩余%d个任务位", budget.RemainingSlots)
	}
	if highValue {
		return budget, false, "当前30秒互动预算已满，高价值任务保留在候选队列等待下一窗口"
	}
	return budget, false, "当前互动预算需要保护主线，普通互动继续聚合等待下一窗口"
}

func (p *Processor) tryConsumeInteractionBudget(roomID int64, heat string, highValue bool, now time.Time) (interactionBudget, bool, string) {
	return p.interactionBudgetDecision(roomID, heat, highValue, now, true)
}

func (p *Processor) checkInteractionBudget(roomID int64, heat string, highValue bool, now time.Time) (interactionBudget, bool, string) {
	return p.interactionBudgetDecision(roomID, heat, highValue, now, false)
}

func (p *Processor) recordInteractionBudgetUse(roomID int64, heat string, highValue bool, now time.Time) {
	_, _, _ = p.interactionBudgetDecision(roomID, heat, highValue, now, true)
}

func interactionPreferenceKindForMission(key string) string {
	switch strings.TrimSpace(key) {
	case "welcome_named", "welcome_batch":
		return "welcome"
	case "reply_like", "reply_follow":
		return "engagement"
	case "reply_chat":
		return "chat"
	default:
		return "question"
	}
}

func interactionBaseEventValue(key string) float64 {
	switch strings.TrimSpace(key) {
	case "reply_chat":
		return 52
	case "reply_follow":
		return 48
	case "reply_like":
		return 24
	case "welcome_named":
		return 34
	case "welcome_batch":
		return 40
	default:
		return 35
	}
}

func interactionHeatFactor(key, heat string) float64 {
	heat = normalizedInteractionHeat(heat)
	kind := interactionPreferenceKindForMission(key)
	switch heat {
	case "COLD":
		switch kind {
		case "welcome":
			return 1.35
		case "chat":
			return 1.22
		case "engagement":
			return 1.15
		default:
			return 1.12
		}
	case "BUSY":
		switch kind {
		case "welcome":
			return 0.82
		case "engagement":
			return 0.78
		case "chat":
			return 0.88
		default:
			return 1.05
		}
	case "HOT":
		switch kind {
		case "welcome":
			return 0.58
		case "engagement":
			return 0.55
		case "chat":
			return 0.7
		default:
			return 1.15
		}
	case "OVERHEATED":
		switch kind {
		case "welcome":
			return 0.38
		case "engagement":
			return 0.35
		case "chat":
			return 0.52
		default:
			return 1.25
		}
	default:
		return 1
	}
}

func interactionValueLevel(value float64) string {
	switch {
	case value >= 80:
		return "HIGH"
	case value >= 45:
		return "MEDIUM"
	default:
		return "LOW"
	}
}

func (p *Processor) interactionWindowDecision(rule interactionRule, window interactionWindow, effectiveWeight int, now time.Time) agentdecision.InteractionDecision {
	heat := normalizedInteractionHeat(p.interactionSignals(window.RoomID).Heat)
	preference := 1.0
	if p != nil && p.policies != nil {
		preference = p.policies.InteractionPreferenceFactor(window.RoomID, interactionPreferenceKindForMission(rule.Key))
	}
	count := maxInteractionInt(window.PendingCount, 1)
	repeatFactor := 1 + float64(minInteractionInt(count-1, 6))*0.08
	waitSeconds := 0.0
	if !window.FirstPending.IsZero() && now.After(window.FirstPending) {
		waitSeconds = now.Sub(window.FirstPending).Seconds()
	}
	waitFactor := 1.0
	if rule.MaxWait > 0 {
		ratio := waitSeconds / rule.MaxWait.Seconds()
		if ratio > 2 {
			ratio = 2
		}
		if ratio > 0 {
			waitFactor += ratio * 0.22
		}
	}
	recentFactor := 1.0
	if !window.LastEmittedAt.IsZero() && rule.MinInterval > 0 && now.Sub(window.LastEmittedAt) < rule.MinInterval*2 {
		recentFactor = 0.82
	}
	weightFactor := 0.75 + float64(maxInteractionInt(effectiveWeight, 1))/400
	value := interactionBaseEventValue(rule.Key) * preference * interactionHeatFactor(rule.Key, heat) * repeatFactor * waitFactor * recentFactor * weightFactor
	if value > 160 {
		value = 160
	}
	if value < 1 {
		value = 1
	}
	deadline := time.Time{}
	if !window.FirstPending.IsZero() && rule.MaxWait > 0 {
		deadline = window.FirstPending.Add(rule.MaxWait)
	}
	reason := fmt.Sprintf(
		"%s累计%d个事件，热度=%s，用户偏好系数=%.2f，等待=%ds",
		rule.Title, count, heat, preference, int(waitSeconds),
	)
	if rule.BatchThreshold > 0 && count >= rule.BatchThreshold {
		reason += "，已达到聚合阈值"
	} else if !deadline.IsZero() && !now.Before(deadline) {
		reason += "，已到最晚处理时间"
	}
	return agentdecision.InteractionDecision{
		Handle:           true,
		PrimaryEvent:     rule.Key,
		MergedEventIDs:   append([]int64(nil), window.EventIDs...),
		EventValue:       value,
		ValueLevel:       interactionValueLevel(value),
		Reason:           reason,
		DeadlineAt:       deadline,
		BudgetAllowed:    true,
		Heat:             heat,
		PreferenceFactor: preference,
	}
}

func questionBusinessValue(signal basepipeline.Signal) float64 {
	topic := strings.ToLower(strings.TrimSpace(signal.Topic))
	content := strings.ToLower(strings.TrimSpace(signal.Content))
	if signal.IsOrderHint {
		return 1
	}
	for _, keyword := range []string{"价格", "多少钱", "规格", "库存", "发货", "物流", "售后", "退换", "优惠", "怎么买", "下单"} {
		if strings.Contains(topic, keyword) || strings.Contains(content, keyword) {
			return 0.92
		}
	}
	if signal.IsNegative {
		return 0.86
	}
	return 0.64
}

func (p *Processor) updateQuestionDebt(event model.RoomEvent, signal basepipeline.Signal, basePriority int, now time.Time) agentdecision.QuestionDebt {
	if now.IsZero() {
		now = p.clock()
	}
	topic := strings.TrimSpace(signal.Topic)
	if topic == "" {
		topic = strings.TrimSpace(signal.Content)
	}
	if topic == "" {
		topic = "QUESTION:UNKNOWN"
	}
	userID := strings.TrimSpace(signal.UserID)
	if userID == "" {
		userID = strings.TrimSpace(event.UserID)
	}
	businessValue := questionBusinessValue(signal)

	p.questionMu.Lock()
	byTopic := p.questionDebts[event.RoomID]
	if byTopic == nil {
		byTopic = make(map[string]*questionDebtState)
		p.questionDebts[event.RoomID] = byTopic
	}
	state := byTopic[topic]
	if state == nil || (!state.LastSeenAt.IsZero() && now.Sub(state.LastSeenAt) > 5*time.Minute) {
		state = &questionDebtState{
			FirstSeenAt: now,
			Users:       make(map[string]struct{}),
		}
		byTopic[topic] = state
	}
	state.LastSeenAt = now
	state.RepeatCount++
	if userID != "" {
		state.Users[userID] = struct{}{}
	}
	if businessValue > state.BusinessValue {
		state.BusinessValue = businessValue
	}
	waitingSeconds := 0
	if !state.FirstSeenAt.IsZero() && now.After(state.FirstSeenAt) {
		waitingSeconds = int(now.Sub(state.FirstSeenAt).Seconds())
	}
	currentPriority := float64(basePriority)
	currentPriority += float64(minInteractionInt(state.RepeatCount-1, 6)) * 7
	currentPriority += float64(minInteractionInt(waitingSeconds/15, 6)) * 4
	currentPriority += state.BusinessValue * 12
	if currentPriority > 100 {
		currentPriority = 100
	}
	result := agentdecision.QuestionDebt{
		Topic:           topic,
		FirstSeenAt:     state.FirstSeenAt,
		RepeatCount:     state.RepeatCount,
		UniqueUsers:     len(state.Users),
		WaitingSeconds:  waitingSeconds,
		BusinessValue:   state.BusinessValue,
		CurrentPriority: currentPriority,
	}
	p.questionMu.Unlock()
	return result
}

func questionHeatFactor(raw string) float64 {
	switch normalizedInteractionHeat(raw) {
	case "COLD":
		return 1.08
	case "BUSY":
		return 1.08
	case "HOT":
		return 1.18
	case "OVERHEATED":
		return 1.28
	default:
		return 1
	}
}

func (p *Processor) questionInteractionDecision(event model.RoomEvent, signal basepipeline.Signal, basePriority int, now time.Time) agentdecision.InteractionDecision {
	heat := normalizedInteractionHeat(p.interactionSignals(event.RoomID).Heat)
	preference := 1.0
	if p != nil && p.policies != nil {
		preference = p.policies.InteractionPreferenceFactor(event.RoomID, "question")
	}
	debt := p.updateQuestionDebt(event, signal, basePriority, now)
	repeatFactor := 1 + float64(minInteractionInt(debt.RepeatCount-1, 6))*0.10
	waitFactor := 1 + float64(minInteractionInt(debt.WaitingSeconds/15, 6))*0.06
	businessFactor := 0.85 + debt.BusinessValue*0.35
	value := 68 * preference * questionHeatFactor(heat) * repeatFactor * waitFactor * businessFactor
	if signal.IsNegative {
		value *= 1.12
	}
	if signal.IsOrderHint {
		value *= 1.18
	}
	if value > 180 {
		value = 180
	}
	if value < 1 {
		value = 1
	}
	deadline := now.Add(20 * time.Second)
	if interactionValueLevel(value) == "HIGH" {
		deadline = now.Add(10 * time.Second)
	}
	budget := interactionBudgetForHeat(heat)
	reason := fmt.Sprintf(
		"观众问题进入高价值保留通道；热度=%s，重复=%d，独立用户=%d，已等待=%ds，业务价值=%.2f",
		heat, debt.RepeatCount, debt.UniqueUsers, debt.WaitingSeconds, debt.BusinessValue,
	)
	return agentdecision.InteractionDecision{
		Handle:           true,
		PrimaryEvent:     "question",
		MergedEventIDs:   []int64{event.ID},
		EventValue:       value,
		ValueLevel:       interactionValueLevel(value),
		Reason:           reason,
		DeadlineAt:       deadline,
		BudgetLevel:      budget.Level,
		BudgetAllowed:    true,
		Heat:             heat,
		PreferenceFactor: preference,
		QuestionDebt:     &debt,
	}
}

func (p *Processor) conversionInteractionDecision(event model.RoomEvent, signal basepipeline.Signal, now time.Time) agentdecision.InteractionDecision {
	if now.IsZero() {
		now = p.clock()
	}
	heat := normalizedInteractionHeat(p.interactionSignals(event.RoomID).Heat)
	preference := 1.0
	if p != nil && p.policies != nil {
		preference = p.policies.InteractionPreferenceFactor(event.RoomID, "conversion")
	}
	value := 100 * preference
	switch heat {
	case "HOT":
		value *= 1.15
	case "OVERHEATED":
		value *= 1.25
	}
	if value > 180 {
		value = 180
	}
	return agentdecision.InteractionDecision{
		Handle:           true,
		PrimaryEvent:     "conversion",
		MergedEventIDs:   []int64{event.ID},
		EventValue:       value,
		ValueLevel:       "HIGH",
		Reason:           "检测到明确成交或下单信号，使用高价值保留通道，不与普通欢迎/点赞竞争常规预算",
		DeadlineAt:       now.Add(8 * time.Second),
		BudgetLevel:      interactionBudgetForHeat(heat).Level,
		BudgetAllowed:    true,
		Heat:             heat,
		PreferenceFactor: preference,
	}
}

func (p *Processor) FlushInteractionWindows(now time.Time) {
	if p == nil || p.decisions == nil || p.runtime == nil {
		return
	}
	if now.IsZero() {
		now = p.clock().UTC()
	} else {
		now = now.UTC()
	}
	type dueMission struct {
		roomID       int64
		key          string
		topicHint    string
		pendingCount int
		candidate    agentdecision.Candidate
		eventValue   float64
		valueLevel   string
		highValue    bool
	}
	due := make([]dueMission, 0, 8)
	roomsToPublish := make(map[int64]struct{}, 8)

	p.interactionMu.Lock()
	for roomID, byKey := range p.interactionWindows {
		roomsToPublish[roomID] = struct{}{}
		if p.runtime.Get(roomID).State != agentwork.StateWorking {
			continue
		}
		for key, window := range byKey {
			rule, ok := defaultInteractionRules[key]
			if !ok || window == nil {
				continue
			}
			_, effectiveWeight, enabled := p.interactionRuleWeight(roomID, window.TenantID, key)
			if !enabled {
				window.PendingCount = 0
				window.FirstPending = time.Time{}
				window.RetryAfter = time.Time{}
				continue
			}
			if !interactionWindowDue(rule, window, now) {
				continue
			}
			snapshot := *window
			candidate := interactionMissionCandidate(rule, snapshot, now)
			if strings.TrimSpace(candidate.Topic) == "" {
				continue
			}
			decision := p.interactionWindowDecision(rule, snapshot, effectiveWeight, now)
			candidate.InteractionDecision = decision
			candidate.Priority = interactionEffectivePriority(rule.Priority, effectiveWeight)
			if eventPriority := int(decision.EventValue + 0.5); eventPriority > candidate.Priority {
				candidate.Priority = eventPriority
			}
			due = append(due, dueMission{
				roomID:       roomID,
				key:          key,
				topicHint:    snapshot.TopicHint,
				pendingCount: snapshot.PendingCount,
				candidate:    candidate,
				eventValue:   decision.EventValue,
				valueLevel:   decision.ValueLevel,
				highValue:    strings.EqualFold(decision.ValueLevel, "HIGH"),
			})
		}
	}
	p.interactionMu.Unlock()

	sort.SliceStable(due, func(i, j int) bool {
		if due[i].eventValue != due[j].eventValue {
			return due[i].eventValue > due[j].eventValue
		}
		if due[i].candidate.Priority != due[j].candidate.Priority {
			return due[i].candidate.Priority > due[j].candidate.Priority
		}
		return due[i].candidate.Topic < due[j].candidate.Topic
	})

	for i := range due {
		mission := &due[i]
		if mission.key == "reply_chat" && p.topicResolver != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			resolution := p.topicResolver.Resolve(ctx, mission.roomID, mission.candidate.Question, mission.topicHint)
			cancel()
			if strings.TrimSpace(resolution.ClusterKey) != "" {
				mission.candidate.Topic = resolution.ClusterKey
				mission.candidate.InteractionDecision.Reason = strings.TrimSpace(
					mission.candidate.InteractionDecision.Reason +
						fmt.Sprintf("；语义主题=%s，来源=%s，相似度=%.2f", resolution.ClusterKey, resolution.Source, resolution.Similarity),
				)
			}
		}

		heat := mission.candidate.InteractionDecision.Heat
		budget, allowed, reason := p.checkInteractionBudget(mission.roomID, heat, mission.highValue, now)
		mission.candidate.InteractionDecision.BudgetLevel = budget.Level
		mission.candidate.InteractionDecision.BudgetAllowed = allowed
		mission.candidate.InteractionDecision.Handle = allowed
		mission.candidate.InteractionDecision.Reason = strings.TrimSpace(
			mission.candidate.InteractionDecision.Reason + "；" + reason,
		)

		p.interactionMu.Lock()
		if current := p.interactionWindows[mission.roomID][mission.key]; current != nil {
			current.LastEventValue = mission.eventValue
			current.LastValueLevel = mission.valueLevel
			current.LastDecisionReason = mission.candidate.InteractionDecision.Reason
			current.LastBudgetLevel = budget.Level
			current.LastBudgetAllowed = allowed
		}
		p.interactionMu.Unlock()
		if !allowed {
			continue
		}

		result := p.decisions.Enqueue(mission.roomID, mission.candidate)
		if result.Suppressed {
			retryAt := now.Add(10 * time.Second)
			if result.RecentlyAnswered != nil && result.RecentlyAnswered.CooldownUntil.After(retryAt) {
				retryAt = result.RecentlyAnswered.CooldownUntil
			}
			p.interactionMu.Lock()
			if current := p.interactionWindows[mission.roomID][mission.key]; current != nil {
				current.RetryAfter = retryAt
				current.LastDecisionReason = strings.TrimSpace(
					current.LastDecisionReason + "；同语义主题仍在冷却，窗口保留到冷却结束后重试",
				)
			}
			p.interactionMu.Unlock()
			continue
		}
		if result.Item == nil {
			continue
		}
		if !result.Merged {
			p.recordInteractionBudgetUse(mission.roomID, heat, mission.highValue, now)
		}

		p.interactionMu.Lock()
		if current := p.interactionWindows[mission.roomID][mission.key]; current != nil {
			consumed := mission.pendingCount
			if consumed > current.PendingCount {
				consumed = current.PendingCount
			}
			current.LastMissionEventCount = consumed
			current.EmittedCount++
			current.LastEmittedAt = now
			current.RetryAfter = time.Time{}
			current.PendingCount -= consumed
			if current.PendingCount <= 0 {
				current.PendingCount = 0
				current.FirstPending = time.Time{}
				current.EventIDs = nil
				current.UserIDs = nil
				current.Names = nil
			} else {
				current.FirstPending = now
			}
		}
		p.interactionMu.Unlock()
		p.logInteractionMission(mission.roomID, mission.candidate)
	}

	for roomID := range roomsToPublish {
		p.publishInteractionStats(roomID, now)
	}
}

func (p *Processor) clock() time.Time {
	if p != nil && p.now != nil {
		return p.now().UTC()
	}
	return time.Now().UTC()
}

func (p *Processor) clearInteractionRoom(roomID int64) {
	if p == nil || roomID <= 0 {
		return
	}
	p.interactionMu.Lock()
	delete(p.interactionWindows, roomID)
	p.interactionMu.Unlock()
	p.interactionBudgetMu.Lock()
	delete(p.interactionBudgetUsed, roomID)
	p.interactionBudgetMu.Unlock()
	p.questionMu.Lock()
	delete(p.questionDebts, roomID)
	p.questionMu.Unlock()
	p.debtMu.Lock()
	delete(p.debtRetryAfter, debtBacklogKey(roomID, string(timeline.DebtQuestion)))
	delete(p.debtRetryAfter, debtBacklogKey(roomID, string(timeline.DebtInteraction)))
	delete(p.debtLastEventID, debtBacklogKey(roomID, string(timeline.DebtQuestion)))
	delete(p.debtLastEventID, debtBacklogKey(roomID, string(timeline.DebtInteraction)))
	p.debtMu.Unlock()
	if p.topicResolver != nil {
		p.topicResolver.ResetRoom(roomID)
	}
	p.publishInteractionStats(roomID, p.clock())
}

func (p *Processor) RefreshInteractionStats(roomID int64) {
	if p == nil || roomID <= 0 {
		return
	}
	p.publishInteractionStats(roomID, p.clock())
}

func (p *Processor) accumulateInteraction(event model.RoomEvent, key, topicHint string) {
	if p == nil || event.RoomID <= 0 {
		return
	}
	rule, ok := defaultInteractionRules[key]
	if !ok {
		return
	}
	_, _, enabled := p.interactionRuleWeight(event.RoomID, event.TenantID, key)
	if !enabled {
		return
	}
	now := event.OccurredAt.UTC()
	if now.IsZero() {
		now = p.clock()
	}
	p.interactionMu.Lock()
	byKey := p.interactionWindows[event.RoomID]
	if byKey == nil {
		byKey = make(map[string]*interactionWindow)
		p.interactionWindows[event.RoomID] = byKey
	}
	window := byKey[key]
	if window == nil {
		window = &interactionWindow{RoomID: event.RoomID, Key: key}
		byKey[key] = window
	}
	window.TenantID = event.TenantID
	window.PendingCount++
	window.TotalEvents++
	if window.FirstPending.IsZero() {
		window.FirstPending = now
	}
	window.LastEventAt = now
	window.LatestEventID = event.ID
	window.LatestUserID = strings.TrimSpace(event.UserID)
	window.LatestName = strings.TrimSpace(event.Nickname)
	window.LatestContent = strings.TrimSpace(event.Content)
	if topicHint = strings.TrimSpace(topicHint); topicHint != "" {
		window.TopicHint = topicHint
	}
	appendInteractionName(&window.Names, event.Nickname, 12)
	appendInteractionName(&window.UserIDs, event.UserID, 24)
	appendInteractionEventID(&window.EventIDs, event.ID, 32)
	due := interactionWindowDue(rule, window, now)
	p.interactionMu.Unlock()
	p.publishInteractionStats(event.RoomID, now)
	if due {
		p.FlushInteractionWindows(now)
	}
}

func interactionWindowDue(rule interactionRule, window *interactionWindow, now time.Time) bool {
	if window != nil && !window.RetryAfter.IsZero() && now.Before(window.RetryAfter) {
		return false
	}
	if window == nil || window.PendingCount < maxInteractionInt(rule.MinPending, 1) || window.FirstPending.IsZero() {
		return false
	}
	if !window.LastEmittedAt.IsZero() && now.Sub(window.LastEmittedAt) < rule.MinInterval {
		return false
	}
	if rule.BatchThreshold > 0 && window.PendingCount >= rule.BatchThreshold {
		return true
	}
	return rule.MaxWait <= 0 || now.Sub(window.FirstPending) >= rule.MaxWait
}

func interactionMissionCandidate(rule interactionRule, window interactionWindow, now time.Time) agentdecision.Candidate {
	windowSeconds := 0
	if !window.FirstPending.IsZero() && now.After(window.FirstPending) {
		windowSeconds = int(now.Sub(window.FirstPending).Seconds())
	}
	count := maxInteractionInt(window.PendingCount, 1)
	question := ""
	summary := ""
	title := rule.Title
	switch rule.Key {
	case "reply_chat":
		question = strings.TrimSpace(window.LatestContent)
		if question == "" {
			question = "自然回应直播间刚刚出现的有效弹幕"
		}
		title = compactInteractionMissionTitle(question, 22)
		summary = fmt.Sprintf("互动时间窗触发：累计%d条有效弹幕，需要自然回应一次", count)
	case "reply_follow":
		question = "自然感谢刚刚新增的关注，不要机械报数"
		summary = fmt.Sprintf("互动时间窗触发：最近累计%d次关注，需要主播自然回应", count)
	case "reply_like":
		question = "自然感谢大家刚才的点赞支持，不逐个报名字"
		summary = fmt.Sprintf("互动时间窗触发：最近累计%d次点赞事件，需要主播自然回应", count)
	case "welcome_named":
		if name := strings.TrimSpace(window.LatestName); name != "" {
			question = "自然欢迎刚进入直播间的 " + name
		} else {
			question = "自然点名欢迎一位刚进入直播间的新朋友"
		}
		summary = "互动时间窗触发：安排一次低频、自然的点名欢迎"
	case "welcome_batch":
		question = "自然欢迎刚刚进入直播间的一批新朋友，不逐个报名字"
		summary = fmt.Sprintf("互动时间窗触发：累计%d次进房事件，需要做一次打包欢迎", count)
	default:
		return agentdecision.Candidate{}
	}
	return agentdecision.Candidate{
		Source:               agentdecision.SourceAgent,
		Topic:                rule.Topic,
		Title:                title,
		Question:             question,
		Summary:              summary,
		ReplyHint:            "这是一次完整口播任务。后续必须结合打断前主线、回归主线和其它策略约束，一次生成完整可播正文。",
		MissionKind:          rule.Key,
		MissionEventCount:    count,
		MissionWindowSeconds: windowSeconds,
		Priority:             rule.Priority,
		EventID:              window.LatestEventID,
		UserID:               window.LatestUserID,
		Nicknames:            append([]string(nil), window.Names...),
		TTLSeconds:           int(rule.TTL.Seconds()),
	}
}

func compactInteractionMissionTitle(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "回复弹幕"
	}
	runes := []rune(text)
	if limit <= 0 || len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

func appendInteractionName(values *[]string, value string, limit int) {
	value = strings.TrimSpace(value)
	if value == "" || values == nil {
		return
	}
	for _, existing := range *values {
		if existing == value {
			return
		}
	}
	*values = append(*values, value)
	if limit > 0 && len(*values) > limit {
		*values = (*values)[len(*values)-limit:]
	}
}

func appendInteractionEventID(values *[]int64, value int64, limit int) {
	if values == nil || value <= 0 {
		return
	}
	for _, existing := range *values {
		if existing == value {
			return
		}
	}
	*values = append(*values, value)
	if limit > 0 && len(*values) > limit {
		*values = (*values)[len(*values)-limit:]
	}
}

func (p *Processor) logInteractionMission(roomID int64, candidate agentdecision.Candidate) {
	if p == nil {
		return
	}
	// Keep this log compact: it is the audit point that proves an interaction
	// event left the scheduler and became a real speech mission.
	log.Printf(
		"interaction mission room=%d kind=%s trigger=%s source_event=%d events=%d window=%ds topic=%s",
		roomID,
		candidate.MissionKind,
		candidate.InteractionDecision.PrimaryEvent,
		candidate.EventID,
		candidate.MissionEventCount,
		candidate.MissionWindowSeconds,
		candidate.Topic,
	)
}

func (p *Processor) publishInteractionStats(roomID int64, now time.Time) {
	if p == nil || p.policies == nil || roomID <= 0 {
		return
	}
	p.policies.UpdateInteractionStats(roomID, p.interactionStats(roomID, now))
}

func (p *Processor) interactionStats(roomID int64, now time.Time) []strategycenter.InteractionWindowStat {
	if p == nil || roomID <= 0 {
		return []strategycenter.InteractionWindowStat{}
	}
	if now.IsZero() {
		now = p.clock()
	} else {
		now = now.UTC()
	}
	p.interactionMu.Lock()
	defer p.interactionMu.Unlock()

	byKey := p.interactionWindows[roomID]
	tenantID := int64(0)
	for _, window := range byKey {
		if window != nil && window.TenantID > 0 {
			tenantID = window.TenantID
			break
		}
	}
	items := make([]strategycenter.InteractionWindowStat, 0, len(defaultInteractionRules))
	for key, rule := range defaultInteractionRules {
		configuredWeight, effectiveWeight, enabled := p.interactionRuleWeight(roomID, tenantID, key)
		stat := strategycenter.InteractionWindowStat{
			Key:                key,
			Name:               rule.Title,
			State:              "idle",
			MinIntervalSeconds: int64(rule.MinInterval.Seconds()),
			MaxWaitSeconds:     int64(rule.MaxWait.Seconds()),
			ConfiguredWeight:   configuredWeight,
			EffectiveWeight:    effectiveWeight,
			EffectivePriority:  interactionEffectivePriority(rule.Priority, effectiveWeight),
		}
		window := byKey[key]
		if !enabled {
			stat.State = "disabled"
		}
		if window != nil {
			stat.PendingCount = window.PendingCount
			stat.TotalEvents = window.TotalEvents
			stat.EmittedCount = window.EmittedCount
			stat.LastMissionEventCount = window.LastMissionEventCount
			stat.FirstPendingAt = window.FirstPending
			stat.LastEventAt = window.LastEventAt
			stat.LastEmittedAt = window.LastEmittedAt
			stat.LastEventValue = window.LastEventValue
			stat.LastValueLevel = window.LastValueLevel
			stat.LastDecisionReason = window.LastDecisionReason
			stat.LastBudgetLevel = window.LastBudgetLevel
			stat.LastBudgetAllowed = window.LastBudgetAllowed
			if enabled {
				if window.PendingCount > 0 {
					stat.State = "pending"
					stat.NextDueAt = interactionNextDueAt(rule, window, now)
				} else if !window.LastEmittedAt.IsZero() && now.Sub(window.LastEmittedAt) < rule.MinInterval {
					stat.State = "cooldown"
					stat.NextDueAt = window.LastEmittedAt.Add(rule.MinInterval)
				}
			}
		}
		items = append(items, stat)
	}
	return items
}

func interactionNextDueAt(rule interactionRule, window *interactionWindow, now time.Time) time.Time {
	if window == nil || window.PendingCount <= 0 || window.FirstPending.IsZero() {
		return time.Time{}
	}
	dueAt := window.FirstPending.Add(rule.MaxWait)
	if rule.MaxWait <= 0 {
		dueAt = now
	}
	if !window.LastEmittedAt.IsZero() {
		cooldownUntil := window.LastEmittedAt.Add(rule.MinInterval)
		if cooldownUntil.After(dueAt) {
			dueAt = cooldownUntil
		}
	}
	if rule.BatchThreshold > 0 &&
		window.PendingCount >= rule.BatchThreshold &&
		(window.LastEmittedAt.IsZero() || !now.Before(window.LastEmittedAt.Add(rule.MinInterval))) {
		return now
	}
	return dueAt
}

func maxInteractionInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInteractionInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
