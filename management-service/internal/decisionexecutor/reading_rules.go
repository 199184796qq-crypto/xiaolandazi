package decisionexecutor

import (
	"context"
	"fmt"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/readingrules"
)

type readingRulesCacheEntry struct {
	rules   []readingrules.Rule
	expires time.Time
}

// readingRulesFor loads the room's 读写规则 with a short cache; TTS-only.
func (w *Worker) readingRulesFor(ctx context.Context, session model.LiveRuntimeSession) []readingrules.Rule {
	if w == nil || w.store == nil || session.RoomID <= 0 {
		return nil
	}
	w.addressMu.Lock()
	if w.readingRules == nil {
		w.readingRules = map[string]readingRulesCacheEntry{}
	}
	w.addressMu.Unlock()
	version, err := w.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return nil
	}
	key := fmt.Sprintf("%d:%d:%d", session.TenantID, version.PlanID, version.ID)
	w.addressMu.Lock()
	now := time.Now()
	if w.now != nil {
		now = w.now()
	}
	if entry, ok := w.readingRules[key]; ok && entry.expires.After(now) {
		w.addressMu.Unlock()
		return entry.rules
	}
	w.addressMu.Unlock()

	text, hasSnapshot := version.GenerationContext["reading_rules"].(string)
	if source, ok := w.store.(planReadingRulesSource); ok && !hasSnapshot {
		if item, err := source.GetPlanReadingRules(ctx, session.TenantID, version.PlanID); err == nil {
			text = item.RulesText
		}
	}
	rules := readingrules.Parse(text)

	w.addressMu.Lock()
	w.readingRules[key] = readingRulesCacheEntry{rules: rules, expires: now.Add(2 * time.Minute)}
	w.addressMu.Unlock()
	return rules
}

type planReadingRulesSource interface {
	GetPlanReadingRules(context.Context, int64, int64) (model.RoomReadingRules, error)
}
