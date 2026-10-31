package paidpipeline

import (
	"context"
	"livecompanion/core/internal/strategycenter"
	"strings"
	"time"
)

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
			p.tickInteractionV2(ctx, now.UTC())
			p.flushTimeReports(now.UTC())
		}
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
	p.scheduleMu.Lock()
	delete(p.schedules, roomID)
	p.scheduleMu.Unlock()
	if v := p.interactionV2.Load(); v != nil {
		v.engine.Reset(roomID)
		v.mu.Lock()
		delete(v.rooms, roomID)
		v.mu.Unlock()
	}
	p.publishInteractionStats(roomID, p.clock())
}

func (p *Processor) RefreshInteractionStats(roomID int64) {
	if p == nil || roomID <= 0 {
		return
	}
	p.publishInteractionStats(roomID, p.clock())
}
func (p *Processor) publishInteractionStats(roomID int64, now time.Time) {
	if p == nil || p.policies == nil || roomID <= 0 {
		return
	}
	p.policies.UpdateInteractionStats(roomID, p.interactionStats(roomID, now))
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

// The UI reads the same tasks that the running scheduler owns. There are no
// legacy aggregation windows or independent budget counters behind this view.
func (p *Processor) interactionStats(roomID int64, now time.Time) []strategycenter.InteractionWindowStat {
	out := make([]strategycenter.InteractionWindowStat, 0, 7)
	v := p.interactionV2.Load()
	if v == nil {
		return out
	}
	for _, row := range []struct{ kind, key, name string }{{"semantic", "reply_question", "语义回答"}, {"welcome", "welcome_named", "欢迎"}, {"like", "reply_like", "点赞"}, {"follow", "reply_follow", "关注"}, {"order", "conversion_signal", "下单"}, {"engagement", "reply_chat", "主动互动"}, {"guide", "warmup", "暖场"}} {
		stat := strategycenter.InteractionWindowStat{Key: row.key, Name: row.name, State: "idle"}
		if row.kind == "guide" {
			stat.MinIntervalSeconds = 180
		}
		out = append(out, stat)
	}
	// Slice capacity is fixed before pointers are retained.
	byKind := map[string]*strategycenter.InteractionWindowStat{}
	kinds := []string{"semantic", "welcome", "like", "follow", "order", "engagement", "guide"}
	for i, k := range kinds {
		byKind[k] = &out[i]
	}
	for _, task := range v.engine.Snapshot(roomID) {
		kind := task.Work.Kind
		if kind == "dialogue" || kind == "urgent" {
			kind = "semantic"
		}
		stat := byKind[kind]
		if stat == nil {
			continue
		}
		stat.TotalEvents += int64(task.Work.Count)
		if task.Started.After(stat.LastEmittedAt) {
			stat.LastEmittedAt = task.Started
		}
		if !task.Started.IsZero() {
			stat.EmittedCount++
		}
		switch task.State {
		case "completed", "cancelled", "expired", "failed":
			continue
		}
		stat.PendingCount += max(1, task.Work.Count)
		stat.State = "pending"
		stat.LastDecisionReason = task.State
		if stat.FirstPendingAt.IsZero() || task.Work.At.Before(stat.FirstPendingAt) {
			stat.FirstPendingAt = task.Work.At
		}
		if task.Work.At.After(stat.LastEventAt) {
			stat.LastEventAt = task.Work.At
		}
		if stat.NextDueAt.IsZero() || task.Work.Due.Before(stat.NextDueAt) {
			stat.NextDueAt = task.Work.Due
		}
	}
	return out
}
