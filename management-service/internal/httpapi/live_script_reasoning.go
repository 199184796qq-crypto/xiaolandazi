package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"livecompanion/management/internal/agentgateway"
)

const (
	liveScriptHighMaxTokens = 12000
	liveScriptReasoningTTL  = 30 * time.Minute
)

type liveScriptReasoningRequest struct {
	SessionID       int64  `json:"session_id"`
	ReasoningEffort string `json:"reasoning_effort"`
	MaxTokens       int    `json:"max_tokens"`
}

// Production Redis 6.0 has no GETDEL. Read and consume in one operation.
var takeLiveScriptReasoning = redis.NewScript(`
	local value = redis.call('GET', KEYS[1])
	if value then redis.call('DEL', KEYS[1]) end
	return value
`)

type liveScriptReasoningQueue struct {
	client *redis.Client
}

func (in *liveScriptReasoningRequest) validate() bool {
	if in.MaxTokens == 0 {
		in.MaxTokens = liveScriptHighMaxTokens
	}
	return in.SessionID > 0 && in.ReasoningEffort == "high" &&
		in.MaxTokens > 0 && in.MaxTokens <= liveScriptHighMaxTokens
}

func liveScriptReasoningKey(scope agentgateway.RoomModelScope) string {
	return fmt.Sprintf("live-script-reasoning:v1:%d:%d:%d", scope.TenantID, scope.RoomID, scope.RuntimeID)
}

func (s *Server) liveNextScriptReasoning(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	room, ok := pathID(w, r)
	if !ok {
		return
	}
	tenant, ok := s.tenantForRoom(w, r, actor, room)
	if !ok || !s.requireLiveStrategyRoomAccess(w, r, actor, tenant, room) {
		return
	}
	var in liveScriptReasoningRequest
	if readJSONWithLimit(w, r, &in, 4096) != nil || !in.validate() {
		writeError(w, http.StatusBadRequest, "请提供当前场次、high 和 1 至 12000 的最大输出 Token")
		return
	}
	ctx := r.Context()
	session, err := s.store.GetLiveRuntimeByRoom(ctx, tenant, room)
	if err != nil || session.ID != in.SessionID || session.Status != "running" || !session.BelongsToExecutionRealm(s.executionRealm) {
		writeError(w, http.StatusConflict, "当前直播场次已变化或未运行")
		return
	}
	version, err := s.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, tenant, room)
	if err != nil || (recordingVoiceMode(version) == recordingStreamMode && !usesScriptFirstParagraphs(version)) {
		writeError(w, http.StatusConflict, "当前方案未启用可指定思考等级的话术生成")
		return
	}
	scope := agentgateway.RoomModelScope{TenantID: tenant, RoomID: room, RuntimeID: session.ID, RequireLease: true}
	profile, err := s.store.ResolveRoomModelProfile(agentgateway.WithRoomModelScope(ctx, scope))
	if err != nil || profile == nil || !agentgateway.SupportsHighReasoning(*profile) {
		writeError(w, http.StatusConflict, "当前直播间分配的模型不支持高思考")
		return
	}
	if in.MaxTokens > profile.MaxTokens {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("本场模型的单次输出上限为 %d Token", profile.MaxTokens))
		return
	}
	if s.liveScriptReasoning.client == nil {
		writeError(w, http.StatusServiceUnavailable, "话术任务暂时无法排队")
		return
	}
	raw, _ := json.Marshal(in)
	queued, err := s.liveScriptReasoning.client.SetNX(ctx, liveScriptReasoningKey(scope), raw, liveScriptReasoningTTL).Result()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "话术任务暂时无法排队")
		return
	}
	if !queued {
		writeError(w, http.StatusConflict, "当前场次已有一条等待生成的高思考话术")
		return
	}
	writeJSON(w, http.StatusAccepted, in)
}

func (s *Server) applyLiveScriptReasoning(ctx context.Context, request *agentgateway.Request) {
	scope := agentgateway.ContextRoomModelScope(ctx)
	if s.liveScriptReasoning.client == nil || !scope.RequireLease || scope.RuntimeID <= 0 || ctx.Err() != nil {
		return
	}
	readCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	raw, err := takeLiveScriptReasoning.Run(readCtx, s.liveScriptReasoning.client, []string{liveScriptReasoningKey(scope)}).Text()
	if errors.Is(err, redis.Nil) {
		return
	}
	if err != nil {
		log.Printf("live script reasoning lookup failed room=%d session=%d: %v", scope.RoomID, scope.RuntimeID, err)
		return
	}
	var in liveScriptReasoningRequest
	if json.Unmarshal([]byte(raw), &in) != nil || !in.validate() || in.SessionID != scope.RuntimeID {
		log.Printf("invalid live script reasoning task room=%d session=%d", scope.RoomID, scope.RuntimeID)
		return
	}
	request.EnableThinking = true
	request.ReasoningEffort = in.ReasoningEffort
	request.MaxTokens = in.MaxTokens
}
