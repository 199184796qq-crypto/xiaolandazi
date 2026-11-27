package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
)

type exchangeScopeKey struct{}

// modelExchangeRedis 让每次模型池调用的请求与返回都留一份到 Redis，24 小时可查。
var modelExchangeRedis *redis.Client

// SetModelExchangeRedis 注入 Redis 客户端；nil 时只保留数据库交换记录。
func (s *Server) SetModelExchangeRedis(client *redis.Client) {
	if client == nil {
		return
	}
	modelExchangeRedis = client
	log.Printf("model exchange cache backend: redis")
}

// cacheModelExchange 异步写一条交换缓存，失败只记日志，不影响调用链。
func cacheModelExchange(ctx context.Context, id int64, part string, payload any) {
	if modelExchangeRedis == nil || id <= 0 {
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	key := fmt.Sprintf("modelexchange:v1:%d:%s", id, part)
	write := func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if err := modelExchangeRedis.Set(c, key, string(raw), 24*time.Hour).Err(); err != nil {
			log.Printf("model exchange cache write failed id=%d part=%s: %v", id, part, err)
		}
	}
	go write()
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func withExchangeScope(ctx context.Context, scope appdb.ModelExchangeScope) context.Context {
	if scope.TenantID > 0 && scope.RoomID > 0 {
		room := agentgateway.ContextRoomModelScope(ctx)
		if room.TenantID != scope.TenantID || room.RoomID != scope.RoomID {
			room = agentgateway.RoomModelScope{TenantID: scope.TenantID, RoomID: scope.RoomID}
		}
		ctx = agentgateway.WithRoomModelScope(ctx, room)
	}
	return context.WithValue(ctx, exchangeScopeKey{}, scope)
}
func (s *Server) beginModelExchange(ctx context.Context, stage, profile string, request any) (int64, error) {
	if err := waitProductionGate(ctx); err != nil {
		return 0, err
	}
	if s.store == nil {
		return 0, nil
	} // Isolated tests only.
	scope, _ := ctx.Value(exchangeScopeKey{}).(appdb.ModelExchangeScope)
	id, err := s.store.BeginModelExchange(ctx, scope, stage, profile, request)
	if err != nil {
		return 0, fmt.Errorf("模型请求日志保存失败，未发送：%w", err)
	}
	cacheModelExchange(ctx, id, "req", map[string]any{"stage": stage, "profile": profile, "request": request})
	if err := s.beginLiveModelCost(ctx, id, stage, profile, request); err != nil {
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		// This request has definitely not reached its supplier. Close only the
		// journal row; the cost controller retains its own decline/unknown state.
		_ = s.store.CompleteModelExchange(persist, id, nil, "", 0, nil, nil, nil, err)
		return 0, err
	}
	return id, nil
}
func (s *Server) finishModelExchange(ctx context.Context, id int64, response any, text string, started time.Time, in, out int64, callErr error) error {
	if id == 0 {
		return callErr
	}
	s.finishLiveModelCost(ctx, id, response, in, out, callErr)
	cacheModelExchange(ctx, id, "resp", map[string]any{"text": text, "input": in, "output": out, "call_error": errorText(callErr), "response": response})
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	var input, output, cached *int64
	// Current transports do not expose presence flags; leave unreported zero unknown.
	if in > 0 {
		input = &in
	}
	if out > 0 {
		output = &out
	}
	// Audit transports report cached prefix tokens inside the gateway response.
	if r, ok := response.(agentgateway.Response); ok && r.CachedInputTokens > 0 {
		v := r.CachedInputTokens
		cached = &v
	}
	if s.store != nil {
		if err := s.store.CompleteModelExchange(persist, id, response, text, time.Since(started).Milliseconds(), input, output, cached, callErr); err != nil {
			// Paid success must not become a generation failure: callers otherwise
			// send the same content again and pay twice merely to repair a journal.
			log.Printf("[model-exchange] 模型请求已经返回，日志暂未保存；保留原结果，不重新发送 exchange=%d", id)
		}
	}
	return callErr
}
