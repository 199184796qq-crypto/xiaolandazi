package httpapi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/ttsscheduler"
)

type liveRoomBudgetKey struct{}

func roomBudgetInContext(ctx context.Context) *livecost.RoomBudget {
	b, _ := ctx.Value(liveRoomBudgetKey{}).(*livecost.RoomBudget)
	return b
}

// The room's saved TTS pool chooses its allowance, never the language model
// currently being invoked. A new session does not reset the room cost ledger.
func (s *Server) withRoomCostBudget(ctx context.Context, scope liveCostCallScope) (context.Context, error) {
	if roomBudgetInContext(ctx) != nil || scope.SessionID <= 0 || scope.TenantID <= 0 || scope.RoomID <= 0 {
		return ctx, nil
	}
	load := s.liveRoomBudgetLoader
	if load == nil && s.store != nil {
		load = func(c context.Context, tenant, room, plan, session int64) (string, error) {
			runtime, err := s.store.GetLiveRuntimeByRoom(c, tenant, room)
			if err != nil || runtime.ID != session || runtime.Status != "running" || !runtime.BelongsToExecutionRealm(s.executionRealm) {
				return "", errors.New("直播费用场次已变化或暂停")
			}
			v, err := s.store.GetPublishedLiveAgentPlanVersionForRoom(c, tenant, room)
			if err != nil || (plan > 0 && v.PlanID != plan) {
				return "", errors.New("直播费用方案身份已变化")
			}
			pool := ttsscheduler.PoolForProvider(v.VoiceIdentity.Provider)
			if v.VoiceIdentity.TTSPool != "" && v.VoiceIdentity.TTSPool != pool {
				return "", errors.New("直播费用语音池身份不一致")
			}
			return pool, nil
		}
	}
	if load == nil {
		return ctx, nil
	} // Isolated tests without a production Store.
	pool, err := load(ctx, scope.TenantID, scope.RoomID, scope.PlanID, scope.SessionID)
	if err != nil {
		return ctx, err
	}
	b, err := livecost.RoomBudgetForPool(pool)
	if err != nil {
		return ctx, err
	}
	ctx = context.WithValue(ctx, liveRoomBudgetKey{}, &b)
	return livecost.WithPolicy(ctx, b.Policy()), nil
}

// Qwen's HTTP TTS has no documented max-output-token control. Reserve above
// the measured average, then settle supplier usage. This is a local admission
// ceiling, not a promise about an unbounded or delayed supplier invoice.
func strictLiveTTSQuote(a ttsgateway.SynthesisAttempt, pricing livecost.PricingSnapshot, ordinary int64) (int64, error) {
	if a.SentTextCharacters < 0 || a.SentTextCharacters > 1_000_000 {
		return 0, errors.New("声音费用文本长度无效")
	}
	if a.Provider == ttsgateway.ProviderVolcengine && a.Model == ttsgateway.VolcengineICL2 {
		price := int64(300) // 3 CNY / 10,000 Unicode characters, including punctuation.
		if r := pricing.Contract; r != nil && r.Kind == livecost.KindTTS && r.Unit == "characters" {
			price = r.CharacterMicros
		}
		return livecost.EstimateTTS(a.SentTextCharacters, price)
	}
	if strings.HasPrefix(strings.ToLower(a.Model), "qwen-audio-3.1-tts-flash") {
		in, out := int64(1_500_000), int64(12_000_000)
		if r := pricing.Contract; r != nil && r.Kind == livecost.KindTTS && r.Unit == "tokens" {
			in, out = r.InputMicrosPerMillion, r.OutputMicrosPerMillion
		}
		return livecost.EstimateTokens(a.SentTextCharacters*4+512, 0, a.SentTextCharacters*8+512, in, 0, out)
	}
	if ordinary <= 0 || ordinary > math.MaxInt64/2 {
		return 0, errors.New("声音费用预留越界")
	}
	return ordinary * 2, nil
}

func requireStrictModelBound(ctx context.Context, maxTokens int, images int) error {
	if roomBudgetInContext(ctx) != nil && (maxTokens <= 0 || images > 0) {
		return fmt.Errorf("直播模型费用缺少可核对的输出上限或图片计价，未发送")
	}
	return nil
}

type scriptModelCostStepsKey struct{}

func withScriptModelCostSteps(ctx context.Context) context.Context {
	return context.WithValue(ctx, scriptModelCostStepsKey{}, &atomic.Int64{})
}

// Rewrites used to call the gateway directly, outside the exchange logger.
// Freeze the same resolved profile and request it would send, then reserve
// before transport. This adds accounting, not prompts or extra model calls.
func (s *Server) completePaidScriptModel(ctx context.Context, request agentgateway.Request) (agentgateway.Response, error) {
	gateway := s.agentGateway
	if gateway == nil {
		gateway = s.speechGateway()
	}
	call := gateway.Complete
	if s.store != nil && s.agentGateway == nil {
		id, err := s.store.ResolveTextRole(ctx, request.Stage)
		if err != nil {
			return agentgateway.Response{}, err
		}
		// All script rewrite stages are non-mainline_text stages. An empty
		// role therefore uses the gateway's original environment fallback.
		if id != "" {
			profile, err := s.store.ResolveSpeechModel(ctx, id)
			if err != nil || profile == nil || !profile.Enabled {
				return agentgateway.Response{}, errors.New("改写模型配置不可用，未发送")
			}
			p := *profile
			request.Provider, request.Model = "anchor:"+p.ID, p.Model
			if request.MaxTokens <= 0 || request.MaxTokens > p.MaxTokens {
				request.MaxTokens = p.MaxTokens
			}
			limit := time.Duration(p.TimeoutMS) * time.Millisecond
			if request.Timeout <= 0 || request.Timeout > limit {
				request.Timeout = limit
			}
			request.Messages = append([]agentgateway.Message(nil), request.Messages...)
			if strings.TrimSpace(p.SystemPrompt) != "" {
				request.Messages = append([]agentgateway.Message{{Role: "system", Content: p.SystemPrompt}}, request.Messages...)
			}
			call = func(c context.Context, r agentgateway.Request) (agentgateway.Response, error) {
				result, err := agentgateway.CompleteSpeechEndpoint(c, p, r)
				result.Provider, result.Model = "anchor:"+p.ID, p.Model
				return result, err
			}
		}
	}
	scope := liveCostScope(ctx)
	if scope.TenantID <= 0 || scope.RoomID <= 0 || s.liveCostController() == nil {
		return call(ctx, request)
	}
	step := int64(1)
	if steps, ok := ctx.Value(scriptModelCostStepsKey{}).(*atomic.Int64); ok {
		step = steps.Add(1)
	}
	if scope.Key == "" {
		return agentgateway.Response{}, errors.New("改写费用请求缺少任务标识，未发送")
	}
	scope.Key = fmt.Sprintf("%s:step:%d", scope.Key, step)
	scope.Attribution = &livecost.Attribution{Source: "script_rewrite"}
	ctx = withLiveCostScope(ctx, scope)
	return s.completeScopedPaidModel(ctx, request, call)
}

func (s *Server) completeScopedPaidModel(ctx context.Context, request agentgateway.Request, call func(context.Context, agentgateway.Request) (agentgateway.Response, error)) (result agentgateway.Response, err error) {
	if err = ctx.Err(); err != nil {
		return result, err
	}
	s.liveCostMu.Lock()
	s.liveModelCostSequence--
	id := s.liveModelCostSequence
	s.liveCostMu.Unlock()
	if err = s.beginLiveModelCost(ctx, id, request.Stage, "", request); err != nil {
		return result, err
	}
	completed, sent := false, false
	defer func() {
		costErr := err
		if !sent {
			costErr = errLiveCostModelNotSent
		} else if !completed {
			costErr = errors.New("改写模型调用异常，回执未知")
		}
		s.finishLiveModelCost(ctx, id, result, result.InputTokens, result.OutputTokens, costErr)
	}()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	sent = true
	result, err = call(ctx, request)
	completed = true
	return result, err
}
