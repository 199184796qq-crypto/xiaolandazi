package httpapi

import (
	"context"
	"fmt"
	appdb "livecompanion/management/internal/db"
	"time"
)

type exchangeScopeKey struct{}

func withExchangeScope(ctx context.Context, scope appdb.ModelExchangeScope) context.Context {
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
	return id, nil
}
func (s *Server) finishModelExchange(ctx context.Context, id int64, response any, text string, started time.Time, in, out int64, callErr error) error {
	if id == 0 {
		return callErr
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	var input, output *int64
	// Current transports do not expose presence flags; leave unreported zero unknown.
	if in > 0 {
		input = &in
	}
	if out > 0 {
		output = &out
	}
	if err := s.store.CompleteModelExchange(persist, id, response, text, time.Since(started).Milliseconds(), input, output, callErr); err != nil {
		return fmt.Errorf("模型已返回，但日志保存失败：%w", err)
	}
	return callErr
}
