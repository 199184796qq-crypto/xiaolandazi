package agentgateway

import "context"

// This scope is established by the application, never by model output.
// RuntimeID fences late work from a stopped or replaced AI session.
type RoomModelScope struct {
	TenantID     int64
	RoomID       int64
	RuntimeID    int64
	RequireLease bool
}
type roomModelScopeKey struct{}

func WithRoomModelScope(ctx context.Context, scope RoomModelScope) context.Context {
	return context.WithValue(ctx, roomModelScopeKey{}, scope)
}
func ContextRoomModelScope(ctx context.Context) RoomModelScope {
	scope, _ := ctx.Value(roomModelScopeKey{}).(RoomModelScope)
	return scope
}
