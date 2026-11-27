package scriptchain

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisKV 用 Redis 存逻辑链缓存，多个实例共享、按 TTL 过期。
type RedisKV struct {
	client *redis.Client
}

// NewRedisKV 构造 Redis 缓存实现。
func NewRedisKV(client *redis.Client) *RedisKV {
	return &RedisKV{client: client}
}

// Get 读键；不存在返回 false。
func (r *RedisKV) Get(ctx context.Context, key string) (string, bool, error) {
	value, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", false, nil
		}
		return "", false, err
	}
	return value, true, nil
}

// Set 写键，ttl>0 时带过期时间。
func (r *RedisKV) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *RedisKV) ClearPrefix(ctx context.Context, prefix string) error {
	var cursor uint64
	for {
		keys, next, err := r.client.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := r.client.Unlink(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		if next == 0 {
			return nil
		}
		cursor = next
	}
}
