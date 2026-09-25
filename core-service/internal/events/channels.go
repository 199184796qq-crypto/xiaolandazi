package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"livecompanion/core/internal/model"
)

type SessionStats struct {
	StartedAt          time.Time  `json:"started_at"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
	ResumePending      bool       `json:"resume_pending"`
	ReopenedAt         *time.Time `json:"reopened_at,omitempty"`
	InterruptedSeconds int64      `json:"interrupted_seconds"`
	LiveSeconds        int64      `json:"live_seconds"`
	EventCount         int64      `json:"event_count"`
	Entries            int64      `json:"entries"`
	Chats              int64      `json:"chats"`
	Likes              int64      `json:"likes"`
	Follows            int64      `json:"follows"`
	Gifts              int64      `json:"gifts"`
	OrderSignals       int64      `json:"order_signals"`
}

func (s *Store) ListImportant(ctx context.Context, tenantID *int64, roomID int64, eventType string, beforeID int64, limit int) ([]model.RoomEvent, error) {
	if _, err := s.EnsureSessionState(ctx, roomID); err != nil {
		return nil, err
	}
	eventType = normalizeImportantType(eventType)
	if !isImportantType(eventType) {
		return []model.RoomEvent{}, nil
	}
	if limit <= 0 {
		limit = 300
	}
	if int64(limit) > s.importantLimit {
		limit = int(s.importantLimit)
	}
	if s.redis == nil {
		return s.listImportantMemory(tenantID, roomID, eventType, beforeID, limit), nil
	}
	max := "+inf"
	if beforeID > 0 {
		max = strconv.FormatInt(beforeID-1, 10)
	}
	values, err := s.redis.ZRevRangeByScore(ctx, importantKey(roomID, eventType), &redis.ZRangeBy{Max: max, Min: "-inf", Offset: 0, Count: int64(limit)}).Result()
	if err != nil {
		return s.listImportantMemory(tenantID, roomID, eventType, beforeID, limit), nil
	}
	items := make([]model.RoomEvent, 0, len(values)+limit)
	seen := make(map[int64]struct{}, len(values)+limit)
	for _, item := range s.listImportantMemory(tenantID, roomID, eventType, beforeID, limit) {
		items = append(items, item)
		seen[item.ID] = struct{}{}
	}
	for _, raw := range values {
		var item model.RoomEvent
		if json.Unmarshal([]byte(raw), &item) != nil {
			continue
		}
		if tenantID != nil && item.TenantID != *tenantID {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		items = append(items, item)
		seen[item.ID] = struct{}{}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Store) GetSessionStats(ctx context.Context, roomID int64) (SessionStats, error) {
	stats, err := s.EnsureSessionState(ctx, roomID)
	if err != nil {
		return SessionStats{}, err
	}
	return stats.withLiveSecondsAt(time.Now().UTC()), nil
}

func (stats SessionStats) withLiveSecondsAt(now time.Time) SessionStats {
	stats.LiveSeconds = 0
	if stats.StartedAt.IsZero() {
		return stats
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	end := now
	if stats.EndedAt != nil && !stats.EndedAt.IsZero() {
		end = stats.EndedAt.UTC()
	} else if stats.ResumePending && stats.ReopenedAt != nil && !stats.ReopenedAt.IsZero() {
		// A reopened room is deliberately frozen until the user chooses merge
		// or fresh. The browser must not silently advance the old session.
		end = stats.ReopenedAt.UTC()
	}
	if end.Before(stats.StartedAt) {
		return stats
	}
	seconds := int64(end.Sub(stats.StartedAt.UTC()).Seconds()) - stats.InterruptedSeconds
	if seconds < 0 {
		seconds = 0
	}
	stats.LiveSeconds = seconds
	return stats
}

func (s *Store) ResolveSessionDecision(
	ctx context.Context,
	tenantID, roomID int64,
	action string,
	now time.Time,
) (SessionStats, error) {
	stats, err := s.EnsureSessionState(ctx, roomID)
	if err != nil {
		return SessionStats{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if !stats.ResumePending {
		return stats.withLiveSecondsAt(now), nil
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "merge":
		if stats.EndedAt != nil && now.After(*stats.EndedAt) {
			stats.InterruptedSeconds += int64(now.Sub(*stats.EndedAt).Seconds())
		}
		if _, err := s.Create(ctx, tenantID, roomID, model.CreateEventInput{
			EventType:  "session_resume",
			OccurredAt: now,
		}); err != nil {
			return SessionStats{}, err
		}
		stats.EndedAt = nil
		stats.ResumePending = false
		stats.ReopenedAt = nil
		if err := s.storeSessionStats(ctx, roomID, stats); err != nil {
			return SessionStats{}, err
		}
		return stats.withLiveSecondsAt(now), nil
	case "fresh":
		startAt := now
		if stats.ReopenedAt != nil && !stats.ReopenedAt.IsZero() {
			startAt = stats.ReopenedAt.UTC()
		}
		if _, err := s.Create(ctx, tenantID, roomID, model.CreateEventInput{
			EventType:  "session_start",
			OccurredAt: startAt,
		}); err != nil {
			return SessionStats{}, err
		}
		items, err := s.ListRecent(ctx, nil, roomID, int(s.limit))
		if err != nil {
			return SessionStats{}, err
		}
		rebuilt := SessionStats{StartedAt: startAt}
		for _, item := range items {
			if item.OccurredAt.Before(startAt) {
				continue
			}
			eventType := normalizeImportantType(item.EventType)
			if eventType == "order_signal" {
				rebuilt.OrderSignals++
				continue
			}
			if !isCountedSessionEvent(item.EventType) {
				continue
			}
			rebuilt.EventCount++
			switch eventType {
			case "chat":
				rebuilt.Chats++
			case "like":
				rebuilt.Likes += eventLikeCount(item)
			case "follow":
				rebuilt.Follows++
			case "gift":
				rebuilt.Gifts++
			default:
				if strings.EqualFold(strings.TrimSpace(item.EventType), "member") {
					rebuilt.Entries++
				}
			}
		}
		if err := s.storeSessionStats(ctx, roomID, rebuilt); err != nil {
			return SessionStats{}, err
		}
		return rebuilt.withLiveSecondsAt(now), nil
	default:
		return SessionStats{}, fmt.Errorf("session action must be merge or fresh")
	}
}

func (s *Store) storeSessionStats(ctx context.Context, roomID int64, stats SessionStats) error {
	s.recentMu.Lock()
	s.stats[roomID] = stats
	s.recentMu.Unlock()
	if s.redis == nil {
		return nil
	}
	values := map[string]any{
		"started_at":          stats.StartedAt.UTC().Format(time.RFC3339Nano),
		"resume_pending":      0,
		"event_count":         stats.EventCount,
		"entries":             stats.Entries,
		"chats":               stats.Chats,
		"likes":               stats.Likes,
		"follows":             stats.Follows,
		"gifts":               stats.Gifts,
		"order_signals":       stats.OrderSignals,
		"interrupted_seconds": stats.InterruptedSeconds,
	}
	pipe := s.redis.Pipeline()
	pipe.Del(ctx, sessionStatsKey(roomID))
	pipe.HSet(ctx, sessionStatsKey(roomID), values)
	pipe.Expire(ctx, sessionStatsKey(roomID), eventCacheTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// EnsureSessionState upgrades an already-running room from the legacy single
// recent list into the session stats + important-event channels. New sessions
// have an exact persisted session_start; legacy sessions fall back to the
// oldest retained public event once, then remain stable across Core restarts.
func (s *Store) EnsureSessionState(ctx context.Context, roomID int64) (SessionStats, error) {
	if s.redis == nil {
		s.recentMu.RLock()
		current := s.stats[roomID]
		s.recentMu.RUnlock()
		if !current.StartedAt.IsZero() {
			return current, nil
		}
		items, err := s.ListRecent(ctx, nil, roomID, int(s.limit))
		if err != nil {
			return SessionStats{}, err
		}
		stats, _ := deriveSessionStats(items)
		s.recentMu.Lock()
		s.stats[roomID] = stats
		s.recentMu.Unlock()
		return stats, nil
	}

	hasStart, err := s.redis.HExists(ctx, sessionStatsKey(roomID), "started_at").Result()
	if err != nil {
		return SessionStats{}, err
	}
	if hasStart {
		values, err := s.redis.HGetAll(ctx, sessionStatsKey(roomID)).Result()
		if err != nil {
			return SessionStats{}, err
		}
		return parseSessionStats(values), nil
	}

	items, err := s.ListRecent(ctx, nil, roomID, int(s.limit))
	if err != nil {
		return SessionStats{}, err
	}
	stats, sessionItems := deriveSessionStats(items)
	if stats.StartedAt.IsZero() {
		values, err := s.redis.HGetAll(ctx, sessionStatsKey(roomID)).Result()
		if err != nil {
			return SessionStats{}, err
		}
		return parseSessionStats(values), nil
	}

	pipe := s.redis.Pipeline()
	key := sessionStatsKey(roomID)
	pipe.HSet(ctx, key, map[string]any{
		"started_at":    stats.StartedAt.UTC().Format(time.RFC3339Nano),
		"event_count":   stats.EventCount,
		"entries":       stats.Entries,
		"chats":         stats.Chats,
		"likes":         stats.Likes,
		"follows":       stats.Follows,
		"gifts":         stats.Gifts,
		"order_signals": stats.OrderSignals,
	})
	pipe.Expire(ctx, key, eventCacheTTL)
	for _, item := range sessionItems {
		eventType := normalizeImportantType(item.EventType)
		if !isImportantType(eventType) {
			continue
		}
		raw, marshalErr := json.Marshal(item)
		if marshalErr != nil {
			continue
		}
		important := importantKey(roomID, eventType)
		pipe.ZAdd(ctx, important, redis.Z{Score: float64(item.ID), Member: raw})
		pipe.ZRemRangeByRank(ctx, important, 0, -s.importantLimit-1)
		pipe.Expire(ctx, important, eventCacheTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return SessionStats{}, err
	}
	return stats, nil
}

func (s *Store) persistExtraChannels(ctx context.Context, pipe redis.Pipeliner, items []model.RoomEvent, values []any) {
	for index, item := range items {
		if eventType := normalizeImportantType(item.EventType); isImportantType(eventType) {
			key := importantKey(item.RoomID, eventType)
			pipe.ZAdd(ctx, key, redis.Z{Score: float64(item.ID), Member: values[index]})
			pipe.ZRemRangeByRank(ctx, key, 0, -s.importantLimit-1)
			pipe.Expire(ctx, key, eventCacheTTL)
		}
		statsKey := sessionStatsKey(item.RoomID)
		eventType := strings.ToLower(strings.TrimSpace(item.EventType))
		switch eventType {
		case "session_start":
			pipe.Del(ctx, statsKey)
			pipe.HSet(ctx, statsKey, map[string]any{
				"started_at":     item.OccurredAt.UTC().Format(time.RFC3339Nano),
				"resume_pending": 0,
			})
			pipe.Expire(ctx, statsKey, eventCacheTTL)
			continue
		case "session_end":
			pipe.HSet(ctx, statsKey, map[string]any{
				"ended_at":       item.OccurredAt.UTC().Format(time.RFC3339Nano),
				"resume_pending": 0,
			})
			pipe.HDel(ctx, statsKey, "reopened_at")
			pipe.Expire(ctx, statsKey, eventCacheTTL)
			continue
		case "session_reopen":
			pipe.HSet(ctx, statsKey, map[string]any{
				"resume_pending": 1,
				"reopened_at":    item.OccurredAt.UTC().Format(time.RFC3339Nano),
			})
			pipe.Expire(ctx, statsKey, eventCacheTTL)
			continue
		case "session_resume":
			pipe.HSet(ctx, statsKey, "resume_pending", 0)
			pipe.HDel(ctx, statsKey, "ended_at", "reopened_at")
			pipe.Expire(ctx, statsKey, eventCacheTTL)
			continue
		}
		if normalizeImportantType(item.EventType) == "order_signal" {
			pipe.HIncrBy(ctx, statsKey, "order_signals", 1)
			pipe.Expire(ctx, statsKey, eventCacheTTL)
			continue
		}
		if !isCountedSessionEvent(item.EventType) {
			continue
		}
		pipe.HIncrBy(ctx, statsKey, "event_count", 1)
		switch normalizeImportantType(item.EventType) {
		case "chat":
			pipe.HIncrBy(ctx, statsKey, "chats", 1)
		case "like":
			pipe.HIncrBy(ctx, statsKey, "likes", eventLikeCount(item))
		case "follow":
			pipe.HIncrBy(ctx, statsKey, "follows", 1)
		case "gift":
			pipe.HIncrBy(ctx, statsKey, "gifts", 1)
		default:
			if strings.EqualFold(strings.TrimSpace(item.EventType), "member") {
				pipe.HIncrBy(ctx, statsKey, "entries", 1)
			}
		}
		pipe.Expire(ctx, statsKey, eventCacheTTL)
	}
}

func (s *Store) rememberImportant(event model.RoomEvent) {
	eventType := normalizeImportantType(event.EventType)
	if !isImportantType(eventType) {
		return
	}
	s.recentMu.Lock()
	roomItems := s.important[event.RoomID]
	if roomItems == nil {
		roomItems = make(map[string][]model.RoomEvent)
		s.important[event.RoomID] = roomItems
	}
	items := append(roomItems[eventType], event)
	if int64(len(items)) > s.importantLimit {
		items = items[len(items)-int(s.importantLimit):]
	}
	roomItems[eventType] = items
	s.recentMu.Unlock()
}

func (s *Store) updateSessionStatsMemory(event model.RoomEvent) {
	s.recentMu.Lock()
	defer s.recentMu.Unlock()
	eventType := strings.ToLower(strings.TrimSpace(event.EventType))
	if eventType == "session_start" {
		s.stats[event.RoomID] = SessionStats{StartedAt: event.OccurredAt.UTC()}
		return
	}
	stats := s.stats[event.RoomID]
	switch eventType {
	case "session_end":
		endedAt := event.OccurredAt.UTC()
		stats.EndedAt = &endedAt
		stats.ResumePending = false
		stats.ReopenedAt = nil
		s.stats[event.RoomID] = stats
		return
	case "session_reopen":
		reopenedAt := event.OccurredAt.UTC()
		stats.ResumePending = true
		stats.ReopenedAt = &reopenedAt
		s.stats[event.RoomID] = stats
		return
	case "session_resume":
		if stats.EndedAt != nil && event.OccurredAt.After(*stats.EndedAt) {
			stats.InterruptedSeconds += int64(event.OccurredAt.Sub(*stats.EndedAt).Seconds())
		}
		stats.EndedAt = nil
		stats.ResumePending = false
		stats.ReopenedAt = nil
		s.stats[event.RoomID] = stats
		return
	}
	if normalizeImportantType(event.EventType) == "order_signal" {
		stats.OrderSignals++
		s.stats[event.RoomID] = stats
		return
	}
	if !isCountedSessionEvent(event.EventType) {
		return
	}
	stats.EventCount++
	switch normalizeImportantType(event.EventType) {
	case "chat":
		stats.Chats++
	case "like":
		stats.Likes += eventLikeCount(event)
	case "follow":
		stats.Follows++
	case "gift":
		stats.Gifts++
	default:
		if strings.EqualFold(strings.TrimSpace(event.EventType), "member") {
			stats.Entries++
		}
	}
	s.stats[event.RoomID] = stats
}

func (s *Store) listImportantMemory(tenantID *int64, roomID int64, eventType string, beforeID int64, limit int) []model.RoomEvent {
	s.recentMu.RLock()
	roomItems := s.important[roomID]
	source := roomItems[eventType]
	result := make([]model.RoomEvent, 0, min(limit, len(source)))
	for i := len(source) - 1; i >= 0 && len(result) < limit; i-- {
		item := source[i]
		if beforeID > 0 && item.ID >= beforeID {
			continue
		}
		if tenantID != nil && item.TenantID != *tenantID {
			continue
		}
		result = append(result, item)
	}
	s.recentMu.RUnlock()
	return result
}

func deriveSessionStats(items []model.RoomEvent) (SessionStats, []model.RoomEvent) {
	if len(items) == 0 {
		return SessionStats{}, nil
	}
	ordered := append([]model.RoomEvent(nil), items...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	start := 0
	stats := SessionStats{}
	for index, item := range ordered {
		switch strings.ToLower(strings.TrimSpace(item.EventType)) {
		case "session_start":
			stats = SessionStats{StartedAt: item.OccurredAt.UTC()}
			start = index + 1
		case "session_end":
			if !stats.StartedAt.IsZero() {
				value := item.OccurredAt.UTC()
				stats.EndedAt = &value
				stats.ResumePending = false
				stats.ReopenedAt = nil
			}
		case "session_reopen":
			if !stats.StartedAt.IsZero() {
				value := item.OccurredAt.UTC()
				stats.ResumePending = true
				stats.ReopenedAt = &value
			}
		case "session_resume":
			if !stats.StartedAt.IsZero() {
				if stats.EndedAt != nil && item.OccurredAt.After(*stats.EndedAt) {
					stats.InterruptedSeconds += int64(item.OccurredAt.Sub(*stats.EndedAt).Seconds())
				}
				stats.EndedAt = nil
				stats.ResumePending = false
				stats.ReopenedAt = nil
			}
		}
	}
	if stats.StartedAt.IsZero() {
		for index, item := range ordered {
			if isCountedSessionEvent(item.EventType) || normalizeImportantType(item.EventType) == "order_signal" {
				stats.StartedAt = item.OccurredAt.UTC()
				start = index
				break
			}
		}
	}
	if stats.StartedAt.IsZero() {
		return SessionStats{}, nil
	}
	sessionItems := ordered[start:]
	for _, item := range sessionItems {
		eventType := normalizeImportantType(item.EventType)
		if eventType == "order_signal" {
			stats.OrderSignals++
			continue
		}
		if !isCountedSessionEvent(item.EventType) {
			continue
		}
		stats.EventCount++
		switch eventType {
		case "chat":
			stats.Chats++
		case "like":
			stats.Likes += eventLikeCount(item)
		case "follow":
			stats.Follows++
		case "gift":
			stats.Gifts++
		default:
			if strings.EqualFold(strings.TrimSpace(item.EventType), "member") {
				stats.Entries++
			}
		}
	}
	return stats, sessionItems
}

func normalizeImportantType(eventType string) string {
	value := strings.ToLower(strings.TrimSpace(eventType))
	if value == "comment" {
		return "chat"
	}
	return value
}

func isImportantType(eventType string) bool {
	switch normalizeImportantType(eventType) {
	case "chat", "like", "follow", "gift", "order_signal", "order":
		return true
	default:
		return false
	}
}

func isCountedSessionEvent(eventType string) bool {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "member", "chat", "comment", "like", "follow", "gift", "order":
		return true
	default:
		return false
	}
}

func eventLikeCount(event model.RoomEvent) int64 {
	if len(event.Payload) > 0 {
		var payload map[string]any
		if json.Unmarshal(event.Payload, &payload) == nil {
			if value, ok := payload["count"].(float64); ok && value > 0 {
				return int64(value)
			}
		}
	}
	return 1
}

func importantKey(roomID int64, eventType string) string {
	return "livecompanion:room:" + strconv.FormatInt(roomID, 10) + ":important:" + eventType
}

func sessionStatsKey(roomID int64) string {
	return "livecompanion:room:" + strconv.FormatInt(roomID, 10) + ":session_stats"
}

func (s *Store) extraRedisKeys(roomID int64) []string {
	keys := []string{sessionStatsKey(roomID)}
	for _, eventType := range []string{"chat", "like", "follow", "gift", "order_signal", "order"} {
		keys = append(keys, importantKey(roomID, eventType))
	}
	return keys
}

func parseSessionStats(values map[string]string) SessionStats {
	parse := func(key string) int64 {
		n, _ := strconv.ParseInt(values[key], 10, 64)
		return n
	}
	var startedAt time.Time
	if raw := strings.TrimSpace(values["started_at"]); raw != "" {
		startedAt, _ = time.Parse(time.RFC3339Nano, raw)
	}
	var endedAt *time.Time
	if raw := strings.TrimSpace(values["ended_at"]); raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			endedAt = &parsed
		}
	}
	var reopenedAt *time.Time
	if raw := strings.TrimSpace(values["reopened_at"]); raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			reopenedAt = &parsed
		}
	}
	resumePending := strings.TrimSpace(values["resume_pending"]) == "1"
	return SessionStats{StartedAt: startedAt, EndedAt: endedAt, ResumePending: resumePending, ReopenedAt: reopenedAt, InterruptedSeconds: parse("interrupted_seconds"), EventCount: parse("event_count"), Entries: parse("entries"), Chats: parse("chats"), Likes: parse("likes"), Follows: parse("follows"), Gifts: parse("gifts"), OrderSignals: parse("order_signals")}
}
