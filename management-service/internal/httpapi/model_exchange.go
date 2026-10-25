package httpapi

import (
	"context"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"log"
	"time"
)

type exchangeScopeKey struct{}

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
