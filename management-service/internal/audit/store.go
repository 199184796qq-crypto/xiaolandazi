package audit

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"livecompanion/management/internal/model"
)

const streamKey = "livecompanion:mgmt:audit"

type Store struct {
	client *redis.Client
	limit  int64
}

func New(
	addr string,
	password string,
	db int,
	limit int,
) *Store {
	if limit <= 0 {
		limit = 10000
	}
	return &Store{
		client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		}),
		limit: int64(limit),
	}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *Store) Close() error {
	return s.client.Close()
}

func (s *Store) Begin(
	ctx context.Context,
	entry model.AdminAuditLog,
) (string, error) {
	if entry.OccurredAt.IsZero() {
		entry.OccurredAt = time.Now().UTC()
	}

	values := map[string]any{
		"occurred_at":      entry.OccurredAt.UTC().Format(time.RFC3339Nano),
		"actor_user_id":    entry.ActorUserID,
		"actor_username":   entry.ActorUsername,
		"action":           entry.Action,
		"target_user_id":   entry.TargetUserID,
		"target_username":  entry.TargetUsername,
		"target_tenant_id": entry.TargetTenantID,
		"http_method":      entry.HTTPMethod,
		"path":             entry.Path,
		"client_ip":        entry.ClientIP,
		"result":           entry.Result,
	}

	messageID, err := s.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		MaxLen: s.limit,
		Approx: true,
		Values: values,
	}).Result()
	return messageID, err
}

func (s *Store) Record(
	ctx context.Context,
	entry model.AdminAuditLog,
) error {
	_, err := s.Begin(ctx, entry)
	return err
}
func (s *Store) Complete(
	ctx context.Context,
	pendingID string,
	entry model.AdminAuditLog,
) error {
	entry.OccurredAt = time.Now().UTC()

	finalID, err := s.Begin(ctx, entry)
	if err != nil {
		return err
	}
	if pendingID == "" || finalID == pendingID {
		return nil
	}
	return s.client.XDel(ctx, streamKey, pendingID).Err()
}

func (s *Store) List(
	ctx context.Context,
	limit int64,
) ([]model.AdminAuditLog, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	messages, err := s.client.XRevRangeN(
		ctx,
		streamKey,
		"+",
		"-",
		limit,
	).Result()
	if err != nil {
		return nil, err
	}

	items := make([]model.AdminAuditLog, 0, len(messages))
	for _, message := range messages {
		item := model.AdminAuditLog{
			ID: message.ID,
		}

		item.OccurredAt, _ = time.Parse(
			time.RFC3339Nano,
			valueString(message.Values["occurred_at"]),
		)
		item.ActorUserID = valueInt64(message.Values["actor_user_id"])
		item.ActorUsername = valueString(message.Values["actor_username"])
		item.Action = valueString(message.Values["action"])
		item.TargetUserID = valueInt64(message.Values["target_user_id"])
		item.TargetUsername = valueString(message.Values["target_username"])
		item.TargetTenantID = valueInt64(message.Values["target_tenant_id"])
		item.HTTPMethod = valueString(message.Values["http_method"])
		item.Path = valueString(message.Values["path"])
		item.ClientIP = valueString(message.Values["client_ip"])
		item.Result = valueString(message.Values["result"])
		items = append(items, item)
	}
	return items, nil
}

func valueString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func valueInt64(value any) int64 {
	parsed, _ := strconv.ParseInt(valueString(value), 10, 64)
	return parsed
}
