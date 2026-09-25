package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"livecompanion/core/internal/model"
)

const eventCacheTTL = 24 * time.Hour

type Store struct {
	redis          *redis.Client
	limit          int64
	importantLimit int64

	observerMu  sync.RWMutex
	observer    func(model.RoomEvent)
	archiveSink func(model.RoomEvent)

	recentMu  sync.RWMutex
	recent    map[int64][]model.RoomEvent
	important map[int64]map[string][]model.RoomEvent
	stats     map[int64]SessionStats

	idMu       sync.Mutex
	idMillis   int64
	idSequence int64
}

func NewStore(client *redis.Client, limit int) *Store {
	return NewStoreWithImportantLimit(client, limit, 20000)
}

func NewStoreWithImportantLimit(client *redis.Client, limit int, importantLimit int) *Store {
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}
	if importantLimit <= 0 {
		importantLimit = 20000
	}
	if importantLimit > 100000 {
		importantLimit = 100000
	}

	return &Store{
		redis:          client,
		limit:          int64(limit),
		importantLimit: int64(importantLimit),
		recent:         make(map[int64][]model.RoomEvent),
		important:      make(map[int64]map[string][]model.RoomEvent),
		stats:          make(map[int64]SessionStats),
	}
}

func (s *Store) SetObserver(observer func(model.RoomEvent)) {
	s.observerMu.Lock()
	s.observer = observer
	s.observerMu.Unlock()
}

func (s *Store) SetArchiveSink(sink func(model.RoomEvent)) {
	s.observerMu.Lock()
	s.archiveSink = sink
	s.observerMu.Unlock()
}

func (s *Store) notify(event model.RoomEvent) {
	s.observerMu.RLock()
	observer := s.observer
	s.observerMu.RUnlock()
	if observer == nil {
		return
	}
	func() {
		defer func() { _ = recover() }()
		observer(event)
	}()
}

func (s *Store) Observe(
	tenantID int64,
	roomID int64,
	input model.CreateEventInput,
) {
	occurredAt := input.OccurredAt.UTC()
	if input.OccurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	s.notify(model.RoomEvent{
		TenantID:   tenantID,
		RoomID:     roomID,
		EventType:  input.EventType,
		UserID:     input.UserID,
		Nickname:   input.Nickname,
		Content:    input.Content,
		OccurredAt: occurredAt,
		Payload:    input.Payload,
	})
}

// BuildEvent creates a locally ordered event without touching Redis. Event IDs
// stay inside JavaScript's safe-integer range while allowing up to 1000 events
// per logical millisecond before the local logical clock advances.
func (s *Store) BuildEvent(
	tenantID int64,
	roomID int64,
	input model.CreateEventInput,
) model.RoomEvent {
	occurredAt := input.OccurredAt.UTC()
	if input.OccurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	payload := append(json.RawMessage(nil), input.Payload...)
	return model.RoomEvent{
		ID:         s.nextEventID(),
		TenantID:   tenantID,
		RoomID:     roomID,
		EventType:  input.EventType,
		UserID:     input.UserID,
		Nickname:   input.Nickname,
		Content:    input.Content,
		OccurredAt: occurredAt,
		Payload:    payload,
	}
}

// Accept makes an event visible to the in-process fast path. It deliberately
// does not perform network or database I/O.
func (s *Store) Accept(event model.RoomEvent) {
	s.remember(event)
	s.rememberImportant(event)
	s.updateSessionStatsMemory(event)
	s.observerMu.RLock()
	archiveSink := s.archiveSink
	s.observerMu.RUnlock()
	if archiveSink != nil && event.ID > 0 {
		func() {
			defer func() { _ = recover() }()
			archiveSink(event)
		}()
	}
	s.notify(event)
}

func (s *Store) ListRecent(
	ctx context.Context,
	tenantID *int64,
	roomID int64,
	limit int,
) ([]model.RoomEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	if int64(limit) > s.limit {
		limit = int(s.limit)
	}
	if s.redis == nil {
		return s.listMemory(tenantID, roomID, limit), nil
	}

	values, err := s.redis.LRange(
		ctx,
		eventsKey(roomID),
		0,
		int64(limit-1),
	).Result()
	if err != nil {
		return s.listMemory(tenantID, roomID, limit), nil
	}

	items := make([]model.RoomEvent, 0, len(values)+limit)
	seen := make(map[int64]struct{}, len(values)+limit)
	for _, item := range s.listMemory(tenantID, roomID, limit) {
		items = append(items, item)
		seen[item.ID] = struct{}{}
	}
	for _, raw := range values {
		var item model.RoomEvent
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
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
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID > items[j].ID
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Store) Create(
	ctx context.Context,
	tenantID int64,
	roomID int64,
	input model.CreateEventInput,
) (model.RoomEvent, error) {
	event := s.BuildEvent(tenantID, roomID, input)
	if err := s.PersistBatch(ctx, []model.RoomEvent{event}); err != nil {
		return model.RoomEvent{}, err
	}
	s.Accept(event)
	return event, nil
}

// PersistBatch writes a room-local event batch with one Redis pipeline round
// trip. Events are already assigned local IDs before reaching this method.
func (s *Store) PersistBatch(ctx context.Context, items []model.RoomEvent) error {
	if len(items) == 0 {
		return nil
	}
	if s.redis == nil {
		return fmt.Errorf("redis is not configured")
	}
	roomID := items[0].RoomID
	values := make([]any, 0, len(items))
	for _, item := range items {
		if item.RoomID != roomID {
			return fmt.Errorf("mixed room event batch")
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return err
		}
		values = append(values, raw)
	}

	pipe := s.redis.Pipeline()
	pipe.LPush(ctx, eventsKey(roomID), values...)
	pipe.LTrim(ctx, eventsKey(roomID), 0, s.limit-1)
	pipe.Expire(ctx, eventsKey(roomID), eventCacheTTL)
	s.persistExtraChannels(ctx, pipe, items, values)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Store) ClearRoom(ctx context.Context, roomID int64) error {
	if roomID <= 0 {
		return fmt.Errorf("invalid room id")
	}
	s.recentMu.Lock()
	delete(s.recent, roomID)
	delete(s.important, roomID)
	delete(s.stats, roomID)
	s.recentMu.Unlock()
	if s.redis == nil {
		return nil
	}
	keys := []string{eventsKey(roomID), sequenceKey(roomID), agentModeKey(roomID)}
	keys = append(keys, s.extraRedisKeys(roomID)...)
	return s.redis.Del(ctx, keys...).Err()
}

func eventsKey(roomID int64) string {
	return "livecompanion:room:" + strconv.FormatInt(roomID, 10) + ":events"
}

func sequenceKey(roomID int64) string {
	return "livecompanion:room:" + strconv.FormatInt(roomID, 10) + ":event_seq"
}

func (s *Store) nextEventID() int64 {
	nowMillis := time.Now().UTC().UnixMilli()
	s.idMu.Lock()
	defer s.idMu.Unlock()

	if nowMillis > s.idMillis {
		s.idMillis = nowMillis
		s.idSequence = 0
	} else {
		s.idSequence++
		if s.idSequence >= 1000 {
			s.idMillis++
			s.idSequence = 0
		}
	}
	return s.idMillis*1000 + s.idSequence
}

func (s *Store) remember(event model.RoomEvent) {
	s.recentMu.Lock()
	items := append(s.recent[event.RoomID], event)
	if int64(len(items)) > s.limit {
		items = items[len(items)-int(s.limit):]
	}
	s.recent[event.RoomID] = items
	s.recentMu.Unlock()
}

func (s *Store) listMemory(
	tenantID *int64,
	roomID int64,
	limit int,
) []model.RoomEvent {
	s.recentMu.RLock()
	source := s.recent[roomID]
	result := make([]model.RoomEvent, 0, min(limit, len(source)))
	for i := len(source) - 1; i >= 0 && len(result) < limit; i-- {
		item := source[i]
		if tenantID != nil && item.TenantID != *tenantID {
			continue
		}
		result = append(result, item)
	}
	s.recentMu.RUnlock()
	return result
}
