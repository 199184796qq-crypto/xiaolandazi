package paidpipeline

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/strategycenter"
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
}

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
		roomID    int64
		candidate agentdecision.Candidate
	}
	due := make([]dueMission, 0, 8)
	roomsToPublish := make([]int64, 0, 8)

	p.interactionMu.Lock()
	for roomID, byKey := range p.interactionWindows {
		roomsToPublish = append(roomsToPublish, roomID)
		if p.runtime.Get(roomID).State != agentwork.StateWorking {
			continue
		}
		for key, window := range byKey {
			rule, ok := defaultInteractionRules[key]
			if !ok {
				continue
			}
			_, effectiveWeight, enabled := p.interactionRuleWeight(roomID, window.TenantID, key)
			if !enabled {
				window.PendingCount = 0
				window.FirstPending = time.Time{}
				continue
			}
			if !interactionWindowDue(rule, window, now) {
				continue
			}
			candidate := interactionMissionCandidate(rule, *window, now)
			candidate.Priority = interactionEffectivePriority(rule.Priority, effectiveWeight)
			if strings.TrimSpace(candidate.Topic) == "" {
				continue
			}
			window.LastMissionEventCount = window.PendingCount
			window.EmittedCount++
			window.PendingCount = 0
			window.FirstPending = time.Time{}
			window.LastEmittedAt = now
			due = append(due, dueMission{roomID: roomID, candidate: candidate})
		}
	}
	p.interactionMu.Unlock()
	for _, roomID := range roomsToPublish {
		p.publishInteractionStats(roomID, now)
	}

	for _, mission := range due {
		result := p.decisions.Enqueue(mission.roomID, mission.candidate)
		if result.Suppressed {
			continue
		}
		p.logInteractionMission(mission.roomID, mission.candidate)
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
	p.publishInteractionStats(roomID, p.clock())
}

func (p *Processor) RefreshInteractionStats(roomID int64) {
	if p == nil || roomID <= 0 {
		return
	}
	p.publishInteractionStats(roomID, p.clock())
}

func (p *Processor) accumulateInteraction(event model.RoomEvent, key string) {
	if p == nil || event.RoomID <= 0 {
		return
	}
	rule, ok := defaultInteractionRules[key]
	if !ok {
		return
	}
	_, effectiveWeight, enabled := p.interactionRuleWeight(event.RoomID, event.TenantID, key)
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
	due := interactionWindowDue(rule, window, now)
	var candidate agentdecision.Candidate
	if due {
		candidate = interactionMissionCandidate(rule, *window, now)
		candidate.Priority = interactionEffectivePriority(rule.Priority, effectiveWeight)
		window.LastMissionEventCount = window.PendingCount
		window.EmittedCount++
		window.PendingCount = 0
		window.FirstPending = time.Time{}
		window.LastEmittedAt = now
	}
	p.interactionMu.Unlock()
	p.publishInteractionStats(event.RoomID, now)
	if strings.TrimSpace(candidate.Topic) == "" {
		return
	}
	result := p.decisions.Enqueue(event.RoomID, candidate)
	if !result.Suppressed {
		p.logInteractionMission(event.RoomID, candidate)
	}
}

func interactionWindowDue(rule interactionRule, window *interactionWindow, now time.Time) bool {
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
	switch rule.Key {
	case "reply_chat":
		question = strings.TrimSpace(window.LatestContent)
		if question == "" {
			question = "自然回应直播间刚刚出现的有效弹幕"
		}
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
		Title:                rule.Title,
		Question:             question,
		Summary:              summary,
		ReplyHint:            "这是一次完整口播任务。后续必须结合打断前主线、回归主线和其它策略约束，一次生成完整可播正文。",
		MissionKind:          rule.Key,
		MissionEventCount:    count,
		MissionWindowSeconds: windowSeconds,
		Priority:             rule.Priority,
		EventID:              window.LatestEventID,
		UserID:               window.LatestUserID,
		ForceReopen:          true,
		TTLSeconds:           int(rule.TTL.Seconds()),
	}
}

func (p *Processor) logInteractionMission(roomID int64, candidate agentdecision.Candidate) {
	if p == nil {
		return
	}
	// Keep this log compact: it is the audit point that proves an interaction
	// event left the scheduler and became a real speech mission.
	log.Printf(
		"interaction mission room=%d kind=%s events=%d window=%ds topic=%s",
		roomID,
		candidate.MissionKind,
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
