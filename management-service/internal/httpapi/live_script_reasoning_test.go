package httpapi

import (
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"livecompanion/management/internal/agentgateway"
)

func TestLiveScriptReasoningRequestLimits(t *testing.T) {
	for _, tc := range []struct {
		effort  string
		session int64
		limit   int
		valid   bool
	}{
		{"high", 1, 0, true},
		{"high", 1, 9500, true},
		{"high", 1, 12000, true},
		{"high", 1, 12001, false},
		{"high", 1, -1, false},
		{"high", 0, 12000, false},
		{"low", 1, 12000, false},
	} {
		in := liveScriptReasoningRequest{SessionID: tc.session, ReasoningEffort: tc.effort, MaxTokens: tc.limit}
		if in.validate() != tc.valid || tc.valid && tc.limit == 0 && in.MaxTokens != 12000 {
			t.Fatalf("invalid validation for %+v", tc)
		}
	}
}

func TestLiveScriptReasoningClaimOnce(t *testing.T) {
	socket := os.Getenv("LIVE_SCRIPT_TEST_REDIS_SOCKET")
	if socket == "" {
		t.Skip("set LIVE_SCRIPT_TEST_REDIS_SOCKET to verify against isolated Redis")
	}
	client := redis.NewClient(&redis.Options{Network: "unix", Addr: socket,
		ContextTimeoutEnabled: true, MaxRetries: -1})
	defer client.Close()
	s := &Server{liveScriptReasoning: liveScriptReasoningQueue{client: client}}
	scope := agentgateway.RoomModelScope{TenantID: 11, RoomID: 21, RuntimeID: time.Now().UnixNano(), RequireLease: true}
	ctx := agentgateway.WithRoomModelScope(t.Context(), scope)
	raw, err := json.Marshal(liveScriptReasoningRequest{SessionID: scope.RuntimeID, ReasoningEffort: "high", MaxTokens: 9500})
	if err != nil {
		t.Fatal(err)
	}
	key := liveScriptReasoningKey(scope)
	if ok, err := client.SetNX(ctx, key, raw, time.Minute).Result(); err != nil || !ok {
		t.Fatalf("queue result = %v, %v", ok, err)
	}
	for _, wrongScope := range []agentgateway.RoomModelScope{
		{TenantID: 11, RoomID: 22, RuntimeID: scope.RuntimeID, RequireLease: true},
		{TenantID: 11, RoomID: 21, RuntimeID: scope.RuntimeID + 1, RequireLease: true},
		{TenantID: 11, RoomID: 21, RuntimeID: scope.RuntimeID},
	} {
		request := agentgateway.Request{MaxTokens: 1800}
		s.applyLiveScriptReasoning(agentgateway.WithRoomModelScope(t.Context(), wrongScope), &request)
		if request.EnableThinking || request.MaxTokens != 1800 {
			t.Fatalf("unrelated request changed: %+v", request)
		}
	}
	var applied atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			request := agentgateway.Request{MaxTokens: 1800}
			s.applyLiveScriptReasoning(ctx, &request)
			if request.ReasoningEffort == "high" && request.EnableThinking && request.MaxTokens == 9500 {
				applied.Add(1)
			} else if request.ReasoningEffort != "" || request.EnableThinking || request.MaxTokens != 1800 {
				t.Errorf("request partially modified: %+v", request)
			}
		})
	}
	wg.Wait()
	if applied.Load() != 1 {
		t.Fatalf("applied %d times, want exactly once", applied.Load())
	}
}
