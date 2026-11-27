package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"livecompanion/management/internal/scriptchain"
)

const rewriteFactsTTL = 12 * time.Hour

type rewriteFactsCache struct {
	local *scriptchain.MemoryKV
	redis *redis.Client
	locks [64]sync.Mutex
}

var planRewriteFacts = &rewriteFactsCache{local: scriptchain.NewMemoryKV()}

func (s *Server) rewriteFactsJSON(ctx context.Context, tenant, plan int64) (json.RawMessage, error) {
	return planRewriteFacts.get(ctx, tenant, plan, false, s.buildRewriteFactsPack)
}

func (s *Server) rebuildRewriteFacts(tenant, plan int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := planRewriteFacts.get(ctx, tenant, plan, true, s.buildRewriteFactsPack); err != nil {
		log.Printf("rewrite facts build failed tenant=%d plan=%d: %v", tenant, plan, err)
	}
}

func (c *rewriteFactsCache) get(ctx context.Context, tenant, plan int64, refresh bool, build func(context.Context, int64, int64) (json.RawMessage, error)) (json.RawMessage, error) {
	key := fmt.Sprintf("rewritefacts:v1:%d:%d", tenant, plan)
	var kv scriptchain.KV = c.local
	if c.redis != nil {
		kv = scriptchain.NewRedisKV(c.redis)
	}
	read := func() (json.RawMessage, bool) {
		value, ok, err := kv.Get(ctx, key)
		return json.RawMessage(value), err == nil && ok && len(value) > 0 && value[0] == '[' && json.Valid([]byte(value))
	}
	if !refresh {
		if value, ok := read(); ok {
			return value, nil
		}
	}
	lock := &c.locks[(uint64(tenant)*31+uint64(plan))%uint64(len(c.locks))]
	lock.Lock()
	defer lock.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !refresh {
			if value, ok := read(); ok {
				return value, nil
			}
		}
		if c.redis == nil {
			value, err := build(ctx, tenant, plan)
			if err == nil {
				err = kv.Set(ctx, key, string(value), rewriteFactsTTL)
			}
			return value, err
		}

		// A slower rebuild must not overwrite a newer projection from another process.
		var value json.RawMessage
		var buildErr error
		err := c.redis.Watch(ctx, func(tx *redis.Tx) error {
			value, buildErr = build(ctx, tenant, plan)
			if buildErr != nil {
				return buildErr
			}
			_, err := tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, key, string(value), rewriteFactsTTL)
				return nil
			})
			return err
		}, key)
		if errors.Is(err, redis.TxFailedErr) {
			continue
		}
		if buildErr != nil || err == nil {
			return value, buildErr
		}
		log.Printf("rewrite facts cache unavailable tenant=%d plan=%d: %v", tenant, plan, err)
		if refresh {
			return nil, err
		}
		if value != nil {
			return value, nil
		}
		return build(ctx, tenant, plan)
	}
}
