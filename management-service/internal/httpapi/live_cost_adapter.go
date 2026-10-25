package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

type liveCostCallScope struct {
	TenantID    int64
	RoomID      int64
	PlanID      int64
	SessionID   int64
	RunID       string
	Priority    string
	Reason      string
	Key         string
	Attribution *livecost.Attribution
	Events      *liveCostCallEvents
}

type liveCostCallEvents struct {
	OptionalDeclined      atomic.Bool
	Uncertain             atomic.Bool
	AccountingUnavailable atomic.Bool
	ChargedMicros         atomic.Int64
}

// Local scheduling observes the same successful-request receipt as the ledger;
// it never refunds paid work merely because the new expression was not used.
func (events *liveCostCallEvents) observeReceipt(receipt livecost.Receipt, fallbackEstimate int64, persistErr error) {
	if events == nil {
		return
	}
	if persistErr != nil {
		events.AccountingUnavailable.Store(true)
	}
	if receipt.Status == livecost.ReceiptUnknown {
		events.Uncertain.Store(true)
		return
	}
	if receipt.Status != livecost.ReceiptSuccess {
		return
	}
	amount := fallbackEstimate
	if receipt.ActualKnown {
		amount = receipt.ActualMicros
	} else if receipt.EstimatedMicros != nil {
		amount = *receipt.EstimatedMicros
	}
	if amount > 0 {
		events.ChargedMicros.Add(amount)
	}
}

type liveCostScopeKey struct{}

func withLiveCostScope(ctx context.Context, scope liveCostCallScope) context.Context {
	return context.WithValue(ctx, liveCostScopeKey{}, scope)
}

func liveCostScope(ctx context.Context) liveCostCallScope {
	scope, _ := ctx.Value(liveCostScopeKey{}).(liveCostCallScope)
	exchange, _ := ctx.Value(exchangeScopeKey{}).(appdb.ModelExchangeScope)
	room := agentgateway.ContextRoomModelScope(ctx)
	if scope.TenantID == 0 {
		scope.TenantID = exchange.TenantID
		if scope.TenantID == 0 {
			scope.TenantID = room.TenantID
		}
	}
	if scope.RoomID == 0 {
		scope.RoomID = exchange.RoomID
		if scope.RoomID == 0 {
			scope.RoomID = room.RoomID
		}
	}
	if scope.PlanID == 0 {
		scope.PlanID = exchange.PlanID
	}
	if scope.SessionID == 0 {
		scope.SessionID = room.RuntimeID
	}
	if scope.RunID == "" {
		scope.RunID = exchange.RunID
	}
	if scope.Priority == "" {
		scope.Priority = livecost.PriorityNecessary
	}
	if scope.Reason == "" {
		scope.Reason = "直播实际模型调用"
	}
	return scope
}

// Contracts are operator-provided reference prices for exactly one provider,
// model and unit. Defaults below remain estimates, even with supplier usage.
// Environment format: LIVE_COST_SUPPLIER_PRICE_CONTRACTS=[{"provider":"...",
// "model":"...","kind":"tts","unit":"characters","character_micros":80}].
type liveCostPriceRule struct {
	Provider                    string `json:"provider"`
	Model                       string `json:"model"`
	Kind                        string `json:"kind"`
	Unit                        string `json:"unit"`
	CharacterMicros             int64  `json:"character_micros"`
	InputMicrosPerMillion       int64  `json:"input_micros_per_million"`
	CachedInputMicrosPerMillion int64  `json:"cached_input_micros_per_million"`
	OutputMicrosPerMillion      int64  `json:"output_micros_per_million"`
}

func (s *Server) liveCostController() *livecost.Controller {
	if s == nil {
		return nil
	}
	s.liveCostMu.Lock()
	defer s.liveCostMu.Unlock()
	if s.liveCostRequests != nil {
		return s.liveCostRequests
	}
	if s.store == nil {
		return nil
	} // Isolated tests supply an explicit controller.
	if !s.liveCostInitialized {
		policy := livecost.DefaultPolicy()
		policy.SpoolDir = filepath.Join(recordingCacheMediaRoot(), "live-cost-spool")
		s.liveCostRequests = livecost.New(s.store, policy)
		s.liveCostInitialized = true
		if raw := strings.TrimSpace(os.Getenv("LIVE_COST_SUPPLIER_PRICE_CONTRACTS")); raw != "" {
			var rules []liveCostPriceRule
			if json.Unmarshal([]byte(raw), &rules) != nil {
				log.Printf("[live-cost] 合约价格配置无效，仅按参考价估计")
			} else {
				for _, rule := range rules {
					if validLiveCostPriceRule(rule) {
						s.liveCostPrices = append(s.liveCostPrices, rule)
					} else {
						log.Printf("[live-cost] 合约价格身份或单位无效，仅按参考价估计")
					}
				}
			}
		}
	}
	return s.liveCostRequests
}

func validLiveCostPriceRule(rule liveCostPriceRule) bool {
	if strings.TrimSpace(rule.Provider) == "" || strings.TrimSpace(rule.Model) == "" || rule.CharacterMicros < 0 || rule.InputMicrosPerMillion < 0 || rule.CachedInputMicrosPerMillion < 0 || rule.OutputMicrosPerMillion < 0 {
		return false
	}
	if rule.Unit == "characters" {
		return rule.Kind == livecost.KindTTS && rule.CharacterMicros > 0
	}
	return rule.Unit == "tokens" && (rule.Kind == livecost.KindTTS || rule.Kind == livecost.KindModel) && rule.InputMicrosPerMillion > 0 && rule.OutputMicrosPerMillion > 0
}

func (s *Server) liveCostPrice(provider, model, kind, unit string) (liveCostPriceRule, bool) {
	s.liveCostMu.Lock()
	defer s.liveCostMu.Unlock()
	for _, rule := range s.liveCostPrices {
		if validLiveCostPriceRule(rule) && rule.Provider == provider && rule.Model == model && rule.Kind == kind && rule.Unit == unit {
			return rule, true
		}
	}
	return liveCostPriceRule{}, false
}

// Replay repairs only the ledger. It never invokes a supplier or holds up a
// successful media response, and a per-server throttle avoids per-sentence IO.
func (s *Server) replayLiveCostSpool(ctx context.Context, controller *livecost.Controller) {
	if controller == nil {
		return
	}
	s.liveCostMu.Lock()
	if time.Since(s.liveCostReplayAt) < 30*time.Second {
		s.liveCostMu.Unlock()
		return
	}
	s.liveCostReplayAt = time.Now()
	s.liveCostMu.Unlock()
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if _, err := controller.ReplaySpool(work); err != nil {
		log.Printf("[live-cost] 请求费用补记暂未完成，将继续保留耐久记录")
	}
}

var errLiveCostOptionalDeclined = errors.New("本轮可选更新暂缓，继续使用有效缓存")
var errLiveCostRequestRecorded = errors.New("该费用请求已发送或结果待核对，不重复生成")
var errLiveCostModelNotSent = errors.New("模型请求在发送前取消")

func (s *Server) reserveLiveCost(ctx context.Context, request livecost.Request) error {
	controller := s.liveCostController()
	if controller == nil {
		return nil
	}
	s.replayLiveCostSpool(ctx, controller)
	reservation, err := controller.Begin(ctx, request)
	if reservation.Existing {
		if events := liveCostScope(ctx).Events; events != nil {
			events.Uncertain.Store(true)
		}
		return errLiveCostRequestRecorded
	}
	if reservation.Allowed {
		if reservation.AccountingUnavailable && !reservation.DegradedRecorded {
			log.Printf("[live-cost] 必要供料继续，但数据库与耐久费用补记均未确认；仅保留当前进程诊断，需人工核对 tenant=%d room=%d request=%s", request.TenantID, request.RoomID, request.Key)
		} else if err != nil || reservation.AccountingUnavailable || reservation.OverBudget {
			log.Printf("[live-cost] 必要供料按业务优先执行，费用预留或超用已留痕 tenant=%d room=%d request=%s", request.TenantID, request.RoomID, request.Key)
		}
		return nil
	}
	if request.Priority == livecost.PriorityOptional {
		if events := liveCostScope(ctx).Events; events != nil {
			if reservation.AccountingUnavailable || errors.Is(err, livecost.ErrAccountingUnavailable) {
				events.AccountingUnavailable.Store(true)
			}
			events.OptionalDeclined.Store(true)
		}
		return errLiveCostOptionalDeclined
	}
	if err != nil {
		return fmt.Errorf("请求费用身份或耐久预留失败：%w", err)
	}
	return errLiveCostRequestRecorded
}

func liveCostDigest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func liveCostKey(kind, key string) string { return kind + "-" + liveCostDigest(key) }

type liveTTSObserver struct {
	s     *Server
	scope liveCostCallScope
}

func (s *Server) withLiveCostTTS(ctx context.Context, scope liveCostCallScope) context.Context {
	ctx = withLiveCostScope(ctx, scope)
	scope = liveCostScope(ctx)
	if scope.TenantID <= 0 || scope.RoomID <= 0 || s.liveCostController() == nil {
		return ctx
	}
	if scope.Reason == "直播实际模型调用" {
		scope.Reason = "直播必要声音供料"
	}
	return ttsgateway.WithSynthesisObserver(ctx, liveTTSObserver{s: s, scope: scope})
}

func (o liveTTSObserver) BeforeSynthesis(ctx context.Context, attempt ttsgateway.SynthesisAttempt) (string, error) {
	if strings.TrimSpace(o.scope.Key) == "" {
		return "", errors.New("声音费用请求缺少稳定标识")
	}
	estimate, reason, err := o.s.estimateLiveTTS(attempt, o.scope)
	if err != nil {
		return "", err
	}
	if estimate <= 0 {
		estimate = 1
	}
	key := liveCostKey(livecost.KindTTS, o.scope.Key)
	fingerprint := liveCostDigest(struct {
		Provider, Model, Voice, Text, Instruction string
		Rate                                      float64
	}{attempt.Provider, attempt.Model, attempt.VoiceID, attempt.TextSHA, attempt.InstructionSHA, attempt.Rate})
	request := livecost.Request{Key: key, TenantID: o.scope.TenantID, RoomID: o.scope.RoomID, PlanID: o.scope.PlanID, SessionID: o.scope.SessionID, RunID: o.scope.RunID, Kind: livecost.KindTTS, Provider: attempt.Provider, Model: attempt.Model, Priority: o.scope.Priority, Reason: reason, Fingerprint: fingerprint, EstimatedMicros: estimate, Characters: attempt.SentTextCharacters, BillableCharacters: attempt.BillableCharacters, CharacterRule: attempt.CharacterRule, Attribution: o.scope.Attribution}
	if err = o.s.reserveLiveCost(ctx, request); err != nil {
		return "", err
	}
	return key, nil
}

func (s *Server) estimateLiveTTS(attempt ttsgateway.SynthesisAttempt, scope liveCostCallScope) (int64, string, error) {
	if attempt.CharacterRule != ttsgateway.BillableCharacterRule || attempt.BillableCharacters < 0 {
		return 0, scope.Reason, errors.New("声音费用缺少实际发送文本的计费字符口径")
	}
	estimate, err := livecost.EstimateTTS(attempt.BillableCharacters, livecost.ReferenceTTSMicrosPerBillableCharacter)
	if err != nil {
		return 0, scope.Reason, err
	}
	if rule, ok := s.liveCostPrice(attempt.Provider, attempt.Model, livecost.KindTTS, "characters"); ok {
		// Before the supplier returns its own units, reserve against the full
		// weighted reference count. This is a quote, not supplier measurement.
		quoted, e := livecost.EstimateTTS(attempt.BillableCharacters, rule.CharacterMicros)
		if e != nil {
			return 0, scope.Reason, e
		}
		if quoted > estimate {
			estimate = quoted
		}
	}
	rule, tokenContract := s.liveCostPrice(attempt.Provider, attempt.Model, livecost.KindTTS, "tokens")
	knownTokenModel := strings.HasPrefix(strings.ToLower(strings.TrimSpace(attempt.Model)), "qwen-audio-3.1-tts-flash")
	reason := scope.Reason
	if tokenContract || knownTokenModel {
		// Token output cannot be quoted exactly before synthesis. Reserve the
		// same conservative estimate for a bounded optional attempt and settle
		// the returned usage. The shared rolling budget still gates every call;
		// an uncertain unit price must not permanently disable all refreshes.
		reason += "；Token计费声音按保守用量预估，实际用量回执后结算，允许必要超用"
		if tokenContract {
			if attempt.SentTextCharacters > math.MaxInt64/16 {
				return 0, reason, errors.New("声音用量预估越界")
			}
			quoted, e := livecost.EstimateTokens(attempt.SentTextCharacters*4, 0, attempt.SentTextCharacters*16, rule.InputMicrosPerMillion, liveCostCachedRate(rule), rule.OutputMicrosPerMillion)
			if e != nil {
				return 0, reason, e
			}
			if quoted > estimate {
				estimate = quoted
			}
		} else {
			reason += "；尚未配置Token合约价，当前金额仅为用户参考价估计"
		}
	}
	return estimate, reason, nil
}

func (o liveTTSObserver) AfterSynthesis(ctx context.Context, r ttsgateway.SynthesisReceipt) error {
	receipt := livecost.Receipt{TenantID: o.scope.TenantID, RoomID: o.scope.RoomID, Status: livecost.ReceiptUnknown, ProviderRequestID: r.ProviderRequestID, Provider: r.Provider, Model: r.Model, UsageSource: livecost.SourceSentTextEstimate, SentTextCharacters: r.SentTextCharacters, BillableCharacters: r.BillableCharacters, CharacterRule: r.CharacterRule}
	switch r.Outcome {
	case ttsgateway.SynthesisSucceeded:
		receipt.Status = livecost.ReceiptSuccess
	case ttsgateway.SynthesisNotSent:
		receipt.Status = livecost.ReceiptNotSent
	}
	if !r.Usage.Invalid && r.Usage.Characters != nil {
		receipt.Characters = *r.Usage.Characters
	}
	if !r.Usage.Invalid && r.Usage.InputTokens != nil {
		receipt.InputTokens = *r.Usage.InputTokens
	}
	if !r.Usage.Invalid && r.Usage.OutputTokens != nil {
		receipt.OutputTokens = *r.Usage.OutputTokens
	}
	if receipt.Status == livecost.ReceiptSuccess {
		// Successful API work is payable before download, archive, approval or
		// playback. An unmatched supplier character unit must never replace the
		// user's actual sent-text weighted estimate, even when it reports zero.
		if estimate, err := livecost.EstimateTTS(r.BillableCharacters, livecost.ReferenceTTSMicrosPerBillableCharacter); err == nil {
			receipt.EstimatedMicros = &estimate
		}
	}
	if receipt.Status == livecost.ReceiptSuccess && !r.Usage.Invalid {
		var amount int64
		var err error
		known := false
		if r.Usage.Characters != nil && *r.Usage.Characters > 0 {
			if rule, ok := o.s.liveCostPrice(r.Provider, r.Model, livecost.KindTTS, "characters"); ok {
				amount, err = livecost.EstimateTTS(*r.Usage.Characters, rule.CharacterMicros)
				known = err == nil
			}
		}
		if !known && r.Usage.InputTokens != nil && r.Usage.OutputTokens != nil && (receipt.InputTokens > 0 || receipt.OutputTokens > 0) {
			if rule, ok := o.s.liveCostPrice(r.Provider, r.Model, livecost.KindTTS, "tokens"); ok {
				amount, err = livecost.EstimateTokens(receipt.InputTokens, 0, receipt.OutputTokens, rule.InputMicrosPerMillion, liveCostCachedRate(rule), rule.OutputMicrosPerMillion)
				known = err == nil
			}
		}
		if known {
			receipt.ActualKnown = true
			receipt.ActualMicros = amount
			receipt.EstimatedMicros = nil
			receipt.UsageSource = livecost.SourceSupplierUsage
		}
	}
	err := o.s.liveCostController().Finish(ctx, r.Ticket, receipt)
	fallback, _, quoteErr := o.s.estimateLiveTTS(r.SynthesisAttempt, o.scope)
	if quoteErr != nil && o.scope.Events != nil {
		o.scope.Events.AccountingUnavailable.Store(true)
	}
	o.scope.Events.observeReceipt(receipt, fallback, err)
	if err != nil {
		log.Printf("[live-cost] 声音费用回执尚未落库，成功声音继续使用；费用记录待核对 tenant=%d room=%d request=%s", o.scope.TenantID, o.scope.RoomID, r.Ticket)
	}
	return err
}

type liveModelCostAttempt struct {
	scope                           liveCostCallScope
	key, provider, model            string
	estimatedInput, estimatedOutput int64
	estimatedMicros                 int64
}

func actualLiveModelRequest(request any) (agentgateway.Request, bool) {
	switch r := request.(type) {
	case agentgateway.Request:
		return r, true
	case *agentgateway.Request:
		if r != nil {
			return *r, true
		}
	}
	return agentgateway.Request{}, false
}

func (s *Server) beginLiveModelCost(ctx context.Context, id int64, stage, profile string, request any) error {
	r, ok := actualLiveModelRequest(request)
	if !ok || strings.EqualFold(strings.TrimSpace(stage), "mainline_tts") {
		return nil
	}
	scope := liveCostScope(ctx)
	if scope.TenantID <= 0 || scope.RoomID <= 0 || s.liveCostController() == nil {
		return nil
	}
	provider := strings.TrimSpace(r.Provider)
	if provider == "" {
		if profile != "" {
			provider = "anchor:" + profile
		} else {
			provider = strings.TrimSpace(os.Getenv("AGENT_PROVIDER"))
			if provider == "" {
				provider = agentgateway.ProviderQwen
			}
		}
	}
	model := strings.TrimSpace(r.Model)
	if model == "" {
		model = strings.TrimSpace(os.Getenv("LIVE_AGENT_MODEL"))
		if model == "" {
			model = agentgateway.DefaultQwenModel
		}
	}
	var input int64
	for _, message := range r.Messages {
		input += int64(utf8.RuneCountInString(message.Content) + 16 + len(message.ImageURLs)*1024)
	}
	if input == 0 {
		input = 1
	}
	output := int64(r.MaxTokens)
	if output <= 0 {
		output = 500
	}
	inputRate, cachedRate, outputRate := liveCostReferenceModelRates(model, input)
	estimate, err := livecost.EstimateTokens(input, 0, output, inputRate, cachedRate, outputRate)
	if err != nil {
		return err
	}
	if rule, ok := s.liveCostPrice(provider, model, livecost.KindModel, "tokens"); ok {
		quoted, e := livecost.EstimateTokens(input, 0, output, rule.InputMicrosPerMillion, liveCostCachedRate(rule), rule.OutputMicrosPerMillion)
		if e != nil {
			return e
		}
		if quoted > estimate {
			estimate = quoted
		}
	}
	if estimate <= 0 {
		estimate = 1
	}
	fingerprint := liveCostDigest(r)
	logical := scope.Key
	if logical == "" {
		logical = "exchange:" + strconv.FormatInt(id, 10)
	}
	// Random prompt changes do not create a new paid attempt after an unknown
	// receipt. A legitimate follow-up requires an explicit new logical key.
	key := liveCostKey(livecost.KindModel, logical+"\x00"+stage)
	if err = s.reserveLiveCost(ctx, livecost.Request{Key: key, TenantID: scope.TenantID, RoomID: scope.RoomID, PlanID: scope.PlanID, SessionID: scope.SessionID, RunID: scope.RunID, Kind: livecost.KindModel, Provider: provider, Model: model, Priority: scope.Priority, Reason: scope.Reason, Fingerprint: fingerprint, EstimatedMicros: estimate, InputTokens: input, OutputTokens: output, Attribution: scope.Attribution}); err != nil {
		return err
	}
	s.liveCostMu.Lock()
	if s.liveModelCostTickets == nil {
		s.liveModelCostTickets = make(map[int64]liveModelCostAttempt)
	}
	s.liveModelCostTickets[id] = liveModelCostAttempt{scope: scope, key: key, provider: provider, model: model, estimatedInput: input, estimatedOutput: output, estimatedMicros: estimate}
	s.liveCostMu.Unlock()
	return nil
}

func (s *Server) finishLiveModelCost(ctx context.Context, id int64, response any, in, out int64, callErr error) {
	s.liveCostMu.Lock()
	attempt, ok := s.liveModelCostTickets[id]
	delete(s.liveModelCostTickets, id)
	s.liveCostMu.Unlock()
	if !ok {
		return
	}
	receipt := livecost.Receipt{TenantID: attempt.scope.TenantID, RoomID: attempt.scope.RoomID, Status: livecost.ReceiptUnknown, UsageSource: livecost.SourceSentTextEstimate}
	if callErr == nil {
		receipt.Status = livecost.ReceiptSuccess
	} else if errors.Is(callErr, errLiveCostModelNotSent) {
		receipt.Status = livecost.ReceiptNotSent
	}
	if in > 0 {
		receipt.InputTokens = in
	}
	if out > 0 {
		receipt.OutputTokens = out
	}
	identityKnown := false
	billingModel := attempt.model
	if r, ok := response.(agentgateway.Response); ok {
		identityKnown = r.Provider == attempt.provider && r.Model == attempt.model
		receipt.Provider, receipt.Model = r.Provider, r.Model
		if strings.TrimSpace(r.Model) != "" {
			billingModel = r.Model
		}
		if r.CachedInputTokens >= 0 && r.CachedInputTokens <= receipt.InputTokens {
			receipt.CachedInputTokens = r.CachedInputTokens
		}
	}
	if receipt.Status == livecost.ReceiptSuccess && (in > 0 || out > 0) {
		// Reported cached tokens may use the user-facing reference discount, but
		// this remains an estimate and never means finished-content reuse.
		inputEstimate, outputEstimate := receipt.InputTokens, receipt.OutputTokens
		if in <= 0 {
			inputEstimate = attempt.estimatedInput
		}
		if out <= 0 {
			outputEstimate = attempt.estimatedOutput
		}
		cachedEstimate := int64(0)
		if receipt.CachedInputTokens > 0 {
			cachedEstimate = receipt.CachedInputTokens
		}
		inputRate, cachedRate, outputRate := liveCostReferenceModelRates(billingModel, inputEstimate)
		if estimate, err := livecost.EstimateTokens(inputEstimate, cachedEstimate, outputEstimate, inputRate, cachedRate, outputRate); err == nil {
			if rule, ok := s.liveCostPrice(attempt.provider, attempt.model, livecost.KindModel, "tokens"); ok && identityKnown {
				if quoted, e := livecost.EstimateTokens(inputEstimate, cachedEstimate, outputEstimate, rule.InputMicrosPerMillion, liveCostCachedRate(rule), rule.OutputMicrosPerMillion); e == nil && quoted > estimate {
					estimate = quoted
				}
			}
			receipt.EstimatedMicros = &estimate
		}
	}
	// Transport zeroes lack presence flags. No response usage, no supplier claim.
	if receipt.Status == livecost.ReceiptSuccess && identityKnown && in > 0 && out > 0 {
		if rule, ok := s.liveCostPrice(attempt.provider, attempt.model, livecost.KindModel, "tokens"); ok {
			if amount, err := livecost.EstimateTokens(receipt.InputTokens, receipt.CachedInputTokens, receipt.OutputTokens, rule.InputMicrosPerMillion, liveCostCachedRate(rule), rule.OutputMicrosPerMillion); err == nil {
				receipt.ActualKnown = true
				receipt.ActualMicros = amount
				receipt.EstimatedMicros = nil
				receipt.UsageSource = livecost.SourceSupplierUsage
			}
		}
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := s.liveCostController().Finish(persist, attempt.key, receipt)
	attempt.scope.Events.observeReceipt(receipt, attempt.estimatedMicros, err)
	if err != nil {
		log.Printf("[live-cost] 模型费用回执尚未落库，成功结果继续使用；费用记录待核对 tenant=%d room=%d request=%s", attempt.scope.TenantID, attempt.scope.RoomID, attempt.key)
	}
}

func liveCostCachedRate(rule liveCostPriceRule) int64 {
	if rule.CachedInputMicrosPerMillion > 0 {
		return rule.CachedInputMicrosPerMillion
	}
	// Missing cached pricing never means free cached tokens.
	return rule.InputMicrosPerMillion
}

// LivePaidContext lets the independent interaction worker use the very same
// ledger as the mainline. The worker supplies its authenticated session/plan
// and stable decision key; no database writes or supplier calls occur here.
func (s *Server) LivePaidContext(ctx context.Context, session model.LiveRuntimeSession, stage string, planID int64, key string) context.Context {
	if session.ID <= 0 {
		ctx = withLiveCostScope(ctx, liveCostCallScope{TenantID: -1, RoomID: -1, SessionID: -1})
		return ttsgateway.WithSynthesisObserver(ctx, nil)
	}
	return s.withLiveCostTTS(ctx, liveCostCallScope{TenantID: session.TenantID, RoomID: session.RoomID, PlanID: planID, SessionID: session.ID, RunID: session.ExternalID, Priority: livecost.PriorityNecessary, Reason: "直播必要互动：" + stage, Key: key, Attribution: &livecost.Attribution{Source: "interaction"}})
}

// CompleteLivePaidModel wraps actual worker model transports, without relying
// on media archives or pretending a successful-but-unlogged response failed.
func (s *Server) CompleteLivePaidModel(ctx context.Context, session model.LiveRuntimeSession, request agentgateway.Request, planID int64, key string, call func(context.Context, agentgateway.Request) (agentgateway.Response, error)) (result agentgateway.Response, err error) {
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if session.ID <= 0 || s.liveCostController() == nil {
		return call(ctx, request)
	}
	ctx = withLiveCostScope(ctx, liveCostCallScope{TenantID: session.TenantID, RoomID: session.RoomID, PlanID: planID, SessionID: session.ID, RunID: session.ExternalID, Priority: livecost.PriorityNecessary, Reason: "直播必要互动模型调用", Key: key, Attribution: &livecost.Attribution{Source: "interaction"}})
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
			costErr = errors.New("模型请求发生异常，回执不明")
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
