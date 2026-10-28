package decisionexecutor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

type paidWorkerTestMarker struct{}
type paidWorkerTrace struct {
	models, voices               int
	modelKeys, voiceKeys, stages []string
	events                       []string
	fail                         bool
}

func (t *paidWorkerTrace) context(ctx context.Context, session model.LiveRuntimeSession, stage string, planID int64, key string) context.Context {
	if session.ID <= 0 || session.TenantID != 7 || session.RoomID != 11 || planID != 3 {
		panic("wrong durable interaction scope")
	}
	t.voices++
	t.voiceKeys = append(t.voiceKeys, key)
	t.stages = append(t.stages, stage)
	t.events = append(t.events, "reserve-voice")
	return context.WithValue(ctx, paidWorkerTestMarker{}, key)
}
func (t *paidWorkerTrace) complete(ctx context.Context, session model.LiveRuntimeSession, request agentgateway.Request, planID int64, key string, next func(context.Context, agentgateway.Request) (agentgateway.Response, error)) (agentgateway.Response, error) {
	if session.ID <= 0 || session.TenantID != 7 || session.RoomID != 11 || planID != 3 {
		panic("wrong durable interaction model scope")
	}
	t.models++
	t.modelKeys = append(t.modelKeys, key)
	t.events = append(t.events, "reserve-model")
	if t.fail {
		return agentgateway.Response{}, errors.New("unknown original receipt; no blind resend")
	}
	return next(ctx, request)
}

func TestPaidWorkerPreparedReclaimHasZeroAdditionalPaidCalls(t *testing.T) {
	w, _, core, agent, tts, session := preparedTestWorker()
	trace := &paidWorkerTrace{}
	w.SetPaidRequestAdapters(trace.context, trace.complete)
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if trace.models != 1 || trace.voices != 1 || agent.calls != 1 || tts.calls != 1 {
		t.Fatal(trace, agent.calls, tts.calls)
	}
	core.dispatchStatus = 0
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if trace.models != 1 || trace.voices != 1 || agent.calls != 1 || tts.calls != 1 {
		t.Fatal("prepared cache hit incurred new payment", trace, agent.calls, tts.calls)
	}
	if !strings.Contains(trace.modelKeys[0], "interaction:9:") || !strings.Contains(trace.voiceKeys[0], "interaction_tts") {
		t.Fatal(trace)
	}
}
func TestPaidWorkerUnknownModelReceiptDoesNotRecallProviderAndCanSpeakSafeFallback(t *testing.T) {
	w, _, _, agent, tts, session := preparedTestWorker()
	trace := &paidWorkerTrace{fail: true}
	w.SetPaidRequestAdapters(trace.context, trace.complete)
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal("safe existing fallback should remain available", err)
	}
	if trace.models != 1 || trace.voices != 1 || agent.calls != 0 || tts.calls != 1 {
		t.Fatal("unknown model request was resent or safe fallback bypassed TTS accounting", trace, agent.calls, tts.calls)
	}
}
func TestPaidWorkerSimulationAndAbsentLiveSessionDoNotEnterLiveLedger(t *testing.T) {
	w, _, _, _, _, session := preparedTestWorker()
	trace := &paidWorkerTrace{}
	w.SetPaidRequestAdapters(trace.context, trace.complete)
	request := agentgateway.Request{Stage: "speech_generation", Messages: []agentgateway.Message{{Content: "测试内容"}}}
	preview := session
	preview.ID = 0
	if _, err := w.completePaidModel(context.Background(), preview, request, "simulation"); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), workerPaidOptOutKey{}, true)
	if _, err := w.completePaidModel(ctx, session, request, "simulation"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.synthesizePaidAudio(ctx, session, "simulation", "same", ttsgateway.SynthesizeRequest{Text: "测试"}); err != nil {
		t.Fatal(err)
	}
	if trace.models != 0 || trace.voices != 0 {
		t.Fatal("non-live preview entered live budget", trace)
	}
}
func TestPaidWorkerChangedSessionCannotReuseAnotherPlansContext(t *testing.T) {
	w, store, _, _, _, session := preparedTestWorker()
	ctx := withWorkerPaidScope(context.Background(), session, 999, "unrelated")
	changed := session
	changed.ID++
	scope, err := w.workerPaidScope(ctx, changed, "new")
	if err != nil || scope.planID != store.plan.ID || scope.planID == 999 || scope.session.ID != changed.ID {
		t.Fatal(scope, err)
	}
}
func TestPaidWorkerExplicitFactsOrVoiceRevisionUsesDifferentRequestKey(t *testing.T) {
	w, _, _, _, _, session := preparedTestWorker()
	trace := &paidWorkerTrace{}
	w.SetPaidRequestAdapters(trace.context, trace.complete)
	ctx := withWorkerPaidScope(context.Background(), session, 3, "decision-facts-1")
	request := ttsgateway.SynthesizeRequest{Text: "当前已核实的事实。", VoiceID: "voice", Model: "model", Rate: 1}
	if _, err := w.synthesizePaidAudio(ctx, session, "interaction_tts", "d-1", request); err != nil {
		t.Fatal(err)
	}
	request.Text = "修改后的事实。"
	ctx = withWorkerPaidScope(ctx, session, 3, "decision-facts-2")
	if _, err := w.synthesizePaidAudio(ctx, session, "interaction_tts", "d-1", request); err != nil {
		t.Fatal(err)
	}
	request.VoiceID = "new-voice"
	ctx = withWorkerPaidScope(ctx, session, 3, "decision-facts-2-voice-2")
	if _, err := w.synthesizePaidAudio(ctx, session, "interaction_tts", "d-1", request); err != nil {
		t.Fatal(err)
	}
	request.Instruction = "更自然"
	ctx = withWorkerPaidScope(ctx, session, 3, "decision-facts-2-voice-2-rules-2")
	if _, err := w.synthesizePaidAudio(ctx, session, "interaction_tts", "d-1", request); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, key := range trace.voiceKeys {
		if seen[key] {
			t.Fatal("changed scope reused previous paid key")
		}
		seen[key] = true
	}
	if len(seen) != 4 {
		t.Fatal(trace)
	}
}
func TestPaidWorkerNicknameProgramPathNeedsNoModel(t *testing.T) {
	agent := &nicknameFixtureModel{identity: "existing-role"}
	w := New(readyVoiceStore(), nil, agent, nil)
	trace := &paidWorkerTrace{}
	w.SetPaidRequestAdapters(trace.context, trace.complete)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}
	for index := 0; index < 2; index++ {
		item := decisionItem{ID: "nickname-decision", Nicknames: []string{"原昵称123456789"}}
		w.planNicknameAddresses(context.Background(), session, &item)
		if item.PromptNicknames[0] != "原昵称" {
			t.Fatal(item.PromptNicknames)
		}
	}
	if trace.models != 2 || trace.voices != 0 || agent.calls != 2 {
		t.Fatal(trace, agent.calls)
	}
}
func TestPaidWorkerBridgeModelUsesSharedBudgetBeforeDuplicateSuppression(t *testing.T) {
	restore := bridgeDiceAllow
	bridgeDiceAllow = func() bool { return true }
	defer func() { bridgeDiceAllow = restore }()
	store := &bridgeProfileStore{fakeStore: readyVoiceStore()}
	agent := &fakeAgent{responses: []string{`{"type":"none","text":""}`}}
	tts := &fakeTTS{}
	w := New(store, &fakeCore{}, agent, tts)
	trace := &paidWorkerTrace{}
	w.SetPaidRequestAdapters(trace.context, trace.complete)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}
	if err := w.generateAndStageBridge(context.Background(), session, decisionItem{ID: "d1"}, returnBridgePlan{ReplyText: "已经回答"}, "facts-fence", "memory-fence"); err != nil {
		t.Fatal(err)
	}
	if trace.models != 1 || trace.voices != 0 || agent.calls != 1 || tts.calls != 0 || !strings.Contains(trace.modelKeys[0], ":bridge:") {
		t.Fatal(trace, agent.calls, tts.calls)
	}
}

func TestPaidWorkerUnknownModelRandomRewriteCannotCreateNewPaidOperation(t *testing.T) {
	w, _, _, agent, _, session := preparedTestWorker()
	agent.err = errors.New("connection lost after request")
	policy := livecost.DefaultPolicy()
	policy.SpoolDir = t.TempDir()
	controller := livecost.New(nil, policy)
	var keys []string
	w.SetPaidRequestAdapters(nil, func(ctx context.Context, s model.LiveRuntimeSession, request agentgateway.Request, planID int64, key string, next func(context.Context, agentgateway.Request) (agentgateway.Response, error)) (agentgateway.Response, error) {
		keys = append(keys, key)
		ticket := interactionDigest(key)
		reservation, err := controller.Begin(ctx, livecost.Request{Key: ticket, TenantID: s.TenantID, RoomID: s.RoomID, SessionID: s.ID, PlanID: planID, Kind: livecost.KindModel, Provider: "stub-model", Priority: livecost.PriorityNecessary, Reason: "互动回答", Fingerprint: interactionDigest(request), EstimatedMicros: 100})
		if !reservation.Allowed {
			return agentgateway.Response{}, errors.New("original receipt pending")
		}
		if !errors.Is(err, livecost.ErrAccountingUnavailable) {
			t.Fatal(err)
		}
		result, callErr := next(ctx, request)
		_ = controller.Finish(ctx, ticket, livecost.Receipt{TenantID: s.TenantID, RoomID: s.RoomID, Status: livecost.ReceiptUnknown})
		return result, callErr
	})
	ctx := withWorkerPaidScope(context.Background(), session, 3, "same-decision-facts-v1")
	request := agentgateway.Request{Stage: "speech_generation", Messages: []agentgateway.Message{{Content: "第一次随机称呼"}}}
	if _, err := w.completePaidModel(ctx, session, request, "answer"); err == nil {
		t.Fatal("expected lost receipt")
	}
	request.Messages[0].Content = "第二次随机改写了称呼和衔接句"
	if _, err := w.completePaidModel(ctx, session, request, "answer"); err == nil {
		t.Fatal("must wait for original receipt")
	}
	if agent.calls != 1 || len(keys) != 2 || keys[0] != keys[1] {
		t.Fatal("random rewrite bypassed unknown fence", agent.calls, keys)
	}
}

type workerFenceTTSProvider struct{ calls int }

func (*workerFenceTTSProvider) Name() string { return "worker-fence" }
func (p *workerFenceTTSProvider) SynthesizeURL(context.Context, ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	p.calls++
	return ttsgateway.SynthesizeResponse{}, errors.New("lost synthesis receipt")
}
func (*workerFenceTTSProvider) CloneVoice(context.Context, ttsgateway.CloneRequest) (ttsgateway.CloneResponse, error) {
	return ttsgateway.CloneResponse{}, errors.New("not used")
}

type workerFenceTTSObserver struct {
	controller *livecost.Controller
	session    model.LiveRuntimeSession
	key        string
	planID     int64
}

func (o workerFenceTTSObserver) BeforeSynthesis(ctx context.Context, attempt ttsgateway.SynthesisAttempt) (string, error) {
	ticket := interactionDigest(o.key)
	reservation, err := o.controller.Begin(ctx, livecost.Request{Key: ticket, TenantID: o.session.TenantID, RoomID: o.session.RoomID, SessionID: o.session.ID, PlanID: o.planID, Kind: livecost.KindTTS, Provider: attempt.Provider, Priority: livecost.PriorityNecessary, Reason: "必要互动声音", Fingerprint: attempt.TextSHA + attempt.InstructionSHA, Characters: attempt.SentTextCharacters})
	if !reservation.Allowed {
		return "", errors.New("original synthesis pending")
	}
	if err != nil && !errors.Is(err, livecost.ErrAccountingUnavailable) {
		return "", err
	}
	return ticket, nil
}
func (o workerFenceTTSObserver) AfterSynthesis(ctx context.Context, receipt ttsgateway.SynthesisReceipt) error {
	return o.controller.Finish(ctx, receipt.Ticket, livecost.Receipt{TenantID: o.session.TenantID, RoomID: o.session.RoomID, Status: livecost.ReceiptUnknown})
}
func TestPaidWorkerUnknownTTSRandomTextOrInstructionCannotCreateNewPaidOperation(t *testing.T) {
	w, _, _, _, _, session := preparedTestWorker()
	provider := &workerFenceTTSProvider{}
	gateway := ttsgateway.New("worker-fence", "stub-model")
	gateway.Register(provider)
	w.tts = gateway
	policy := livecost.DefaultPolicy()
	policy.SpoolDir = t.TempDir()
	controller := livecost.New(nil, policy)
	var keys []string
	w.SetPaidRequestAdapters(func(ctx context.Context, s model.LiveRuntimeSession, stage string, planID int64, key string) context.Context {
		keys = append(keys, key)
		return ttsgateway.WithSynthesisObserver(ctx, workerFenceTTSObserver{controller: controller, session: s, key: key, planID: planID})
	}, nil)
	ctx := withWorkerPaidScope(context.Background(), session, 3, "same-decision-facts-v1")
	request := ttsgateway.SynthesizeRequest{Text: "第一段随机话术。", Instruction: "自然一点", VoiceID: "same-voice", Rate: 1}
	if _, err := w.synthesizePaidAudio(ctx, session, "interaction_tts", "d-1", request); err == nil {
		t.Fatal("expected lost receipt")
	}
	request.Text = "第二次随机生成了不同话术。"
	request.Instruction = "轻松一点"
	if _, err := w.synthesizePaidAudio(ctx, session, "interaction_tts", "d-1", request); err == nil {
		t.Fatal("must not resynthesize unknown operation")
	}
	if provider.calls != 1 || len(keys) != 2 || keys[0] != keys[1] {
		t.Fatal("random TTS payload bypassed unknown fence", provider.calls, keys)
	}
}
