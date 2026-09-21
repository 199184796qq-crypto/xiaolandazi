package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"livecompanion/core/internal/model"
)

const eventCacheTTL = 24 * time.Hour

type Store struct {
	redis *redis.Client
	limit int64
}

func NewStore(client *redis.Client, limit int) *Store {
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}

	return &Store{
		redis: client,
		limit: int64(limit),
	}
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

	values, err := s.redis.LRange(
		ctx,
		eventsKey(roomID),
		0,
		int64(limit-1),
	).Result()
	if err != nil {
		return nil, err
	}

	items := make([]model.RoomEvent, 0, len(values))
	for _, raw := range values {
		var item model.RoomEvent
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			continue
		}
		if tenantID != nil && item.TenantID != *tenantID {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Store) Create(
	ctx context.Context,
	tenantID int64,
	roomID int64,
	input model.CreateEventInput,
) (model.RoomEvent, error) {
	occurredAt := input.OccurredAt.UTC()
	if input.OccurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	eventID, err := s.redis.Incr(ctx, sequenceKey(roomID)).Result()
	if err != nil {
		return model.RoomEvent{}, err
	}

	event := model.RoomEvent{
		ID:         eventID,
		TenantID:   tenantID,
		RoomID:     roomID,
		EventType:  input.EventType,
		UserID:     input.UserID,
		Nickname:   input.Nickname,
		Content:    input.Content,
		OccurredAt: occurredAt,
		Payload:    input.Payload,
	}

	raw, err := json.Marshal(event)
	if err != nil {
		return model.RoomEvent{}, err
	}

	pipe := s.redis.TxPipeline()
	pipe.LPush(ctx, eventsKey(roomID), raw)
	pipe.LTrim(ctx, eventsKey(roomID), 0, s.limit-1)
	pipe.Expire(ctx, eventsKey(roomID), eventCacheTTL)
	pipe.Expire(ctx, sequenceKey(roomID), eventCacheTTL)

	if _, err := pipe.Exec(ctx); err != nil {
		return model.RoomEvent{}, err
	}
	return event, nil
}

func (s *Store) ClearRoom(ctx context.Context, roomID int64) error {
	if roomID <= 0 {
		return fmt.Errorf("invalid room id")
	}
	return s.redis.Del(
		ctx,
		eventsKey(roomID),
		sequenceKey(roomID),
	).Err()
}

func eventsKey(roomID int64) string {
	return "livecompanion:room:" + strconv.FormatInt(roomID, 10) + ":events"
}

func sequenceKey(roomID int64) string {
	return "livecompanion:room:" + strconv.FormatInt(roomID, 10) + ":event_seq"
}
