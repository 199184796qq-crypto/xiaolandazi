package anchorcycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Failure is session-scoped, including failures before a cycle state exists.
// It shares the process namespace and cannot restore a run after restart.
type LiveFailure struct {
	SessionID  int64     `json:"session_id"`
	Code       string    `json:"code"`
	Message    string    `json:"message"`
	Stage      string    `json:"stage,omitempty"`
	Action     string    `json:"action,omitempty"`
	RequestID  string    `json:"request_id,omitempty"`
	HTTPStatus int       `json:"http_status,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

type LiveFailureRepository interface {
	SaveLiveFailure(context.Context, int64, int64, LiveFailure) error
	GetLiveFailure(context.Context, int64, int64, int64) (*LiveFailure, error)
}

func (s *RedisStore) liveFailureKey(tenant, room, session int64) string {
	return fmt.Sprintf("%s:%d:%d:failure:%d", s.prefix, tenant, room, session)
}

func (s *RedisStore) SaveLiveFailure(ctx context.Context, tenant, room int64, failure LiveFailure) error {
	if tenant <= 0 || room <= 0 || failure.SessionID <= 0 || failure.Code == "" || failure.Message == "" {
		return errors.New("invalid live failure scope")
	}
	raw, err := json.Marshal(failure)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.liveFailureKey(tenant, room, failure.SessionID), raw, TTL).Err()
}

func (s *RedisStore) GetLiveFailure(ctx context.Context, tenant, room, session int64) (*LiveFailure, error) {
	raw, err := s.client.Get(ctx, s.liveFailureKey(tenant, room, session)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var failure LiveFailure
	if err = json.Unmarshal(raw, &failure); err != nil {
		return nil, err
	}
	if failure.SessionID != session {
		return nil, errors.New("live failure session mismatch")
	}
	return &failure, nil
}
