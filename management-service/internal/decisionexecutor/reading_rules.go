package decisionexecutor

import (
	"context"
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
		w.readingRules = map[int64]readingRulesCacheEntry{}
	}
	now := time.Now()
	if w.now != nil {
		now = w.now()
	}
	if entry, ok := w.readingRules[session.RoomID]; ok && entry.expires.After(now) {
		w.addressMu.Unlock()
		return entry.rules
	}
	w.addressMu.Unlock()

	var text string
	if source, ok := w.store.(roomReadingRulesSource); ok {
		if item, err := source.GetRoomReadingRules(ctx, session.TenantID, session.RoomID); err == nil {
			text = item.RulesText
		}
	}
	rules := readingrules.Parse(text)

	w.addressMu.Lock()
	w.readingRules[session.RoomID] = readingRulesCacheEntry{rules: rules, expires: now.Add(2 * time.Minute)}
	w.addressMu.Unlock()
	return rules
}

type roomReadingRulesSource interface {
	GetRoomReadingRules(context.Context, int64, int64) (model.RoomReadingRules, error)
}
