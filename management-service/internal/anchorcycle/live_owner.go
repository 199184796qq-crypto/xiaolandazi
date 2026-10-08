package anchorcycle

import (
	"context"
	"fmt"
	"time"
)

// Shared liveness ONLY, no cursor, Agent session, or restoration data. A new
// process cannot recover run progress from this 30-second lease.
type LiveOwnerRepository interface {
	ClaimLiveOwner(context.Context, int64, int64, int64, string) (bool, error)
	RenewLiveOwner(context.Context, int64, int64, int64, string) (bool, error)
	ReleaseLiveOwner(context.Context, int64, int64, int64, string) error
	LiveOwnerActive(context.Context, int64, int64, int64) (bool, error)
}

func (s *RedisStore) liveOwnerKey(tenant, room, runtime int64) string {
	return fmt.Sprintf("%s:%d:%d:%d", s.ownerPrefix, tenant, room, runtime)
}
func (s *RedisStore) ClaimLiveOwner(ctx context.Context, tenant, room, runtime int64, token string) (bool, error) {
	return s.client.SetNX(ctx, s.liveOwnerKey(tenant, room, runtime), token, 30*time.Second).Result()
}
func (s *RedisStore) RenewLiveOwner(ctx context.Context, tenant, room, runtime int64, token string) (bool, error) {
	n, err := s.client.Eval(ctx, `if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end redis.call('EXPIRE',KEYS[1],30);return 1`, []string{s.liveOwnerKey(tenant, room, runtime)}, token).Int()
	return n == 1, err
}
func (s *RedisStore) ReleaseLiveOwner(ctx context.Context, tenant, room, runtime int64, token string) error {
	return s.client.Eval(ctx, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, []string{s.liveOwnerKey(tenant, room, runtime)}, token).Err()
}
func (s *RedisStore) LiveOwnerActive(ctx context.Context, tenant, room, runtime int64) (bool, error) {
	n, err := s.client.Exists(ctx, s.liveOwnerKey(tenant, room, runtime)).Result()
	return n == 1, err
}
