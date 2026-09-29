package decisionexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/voicecatalog"
)

func TestAdaptiveAnswerLengthGuidanceUsesComplexityAndHardCap(t *testing.T) {
	short := adaptiveAnswerLengthGuidance(decisionItem{SampleQuestions: []string{"多少钱"}})
	if !strings.Contains(short, "20–80字") || !strings.Contains(short, "绝不能超过300字") {
		t.Fatalf("unexpected short guidance: %s", short)
	}
	complex := adaptiveAnswerLengthGuidance(decisionItem{SampleQuestions: []string{
		"这个商品适合什么人群，规格怎么选？",
		"活动怎么算，赠品有什么条件？",
		"发货和售后分别怎么处理？",
	}})
	if !strings.Contains(complex, "80–220字") || !strings.Contains(complex, "300字是硬上限，不是目标字数") {
		t.Fatalf("unexpected complex guidance: %s", complex)
	}
}

func TestAgentInputPreviewUsesOperatorReviewPrompt(t *testing.T) {
	store := readyVoiceStore()
	worker := New(store, nil, &fakeAgent{}, nil)
	prompt, err := worker.answerPrompt(
		context.Background(),
		model.LiveRuntimeSession{TenantID: 7, RoomID: 11},
		decisionItem{
			ManualOrigin:    "agent_input_preview",
			SampleQuestions: []string{"欢迎新进来的朋友"},
			ReplyHint:       "原文合适就保留，必要时优化",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "操作者指令：欢迎新进来的朋友") {
		t.Fatalf("preview must use operator instruction prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "这是播出前预生成审核") {
		t.Fatalf("preview review requirement missing: %s", prompt)
	}
	if strings.Contains(prompt, "观众原话：") {
		t.Fatalf("operator preview must not be treated as an audience question: %s", prompt)
	}
}

func TestHiddenStrategyGuidanceControlsModelWithoutBecomingReplyHint(t *testing.T) {
	store := readyVoiceStore()
	worker := New(store, nil, &fakeAgent{}, nil)
	item := decisionItem{
		SampleQuestions: []string{"这个怎么选？"},
		ReplyHint:       "先把规格区别讲清楚",
	}
	item.addHiddenStrategyGuidance("称呼偏好：语境自然时可以称呼“宝子”，不要生硬插入。")
	item.addHiddenStrategyGuidance("切入方式：先轻声接住问题，再自然回答。")
	prompt, err := worker.answerPrompt(context.Background(), model.LiveRuntimeSession{TenantID: 7, RoomID: 11}, item)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "【仅供模型内部执行的生成控制】") ||
		!strings.Contains(prompt, "称呼偏好：语境自然时可以称呼“宝子”") ||
		!strings.Contains(prompt, "切入方式：先轻声接住问题") {
		t.Fatalf("hidden strategy guidance missing from model prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "回答提示：先把规格区别讲清楚") {
		t.Fatalf("ordinary reply hint should remain separate: %s", prompt)
	}
	for _, leaked := range []string{"【本次称呼】", "【本次打断行为】", "【本次回归策略】"} {
		if strings.Contains(prompt, leaked) {
			t.Fatalf("legacy strategy label must not be used in model prompt: %s", leaked)
		}
	}
}

func TestStripInternalSpeechLeakRemovesControlMetadata(t *testing.T) {
	got := stripInternalSpeechLeak("【本次称呼】宝子，这个规格你这样选就行。\n策略名称：桥接恢复\n别着急，我接着给你讲。")
	if strings.Contains(got, "本次称呼") || strings.Contains(got, "策略名称") || strings.Contains(got, "桥接恢复") {
		t.Fatalf("internal control metadata leaked: %q", got)
	}
	if !strings.Contains(got, "宝子，这个规格你这样选就行") || !strings.Contains(got, "别着急，我接着给你讲") {
		t.Fatalf("natural speech should be preserved: %q", got)
	}
}

func TestExtractFinalSpeechSentenceReturnsBridgeTailOnly(t *testing.T) {
	got := extractFinalSpeechSentence("老乡，这款是低芥酸非转基因，先把你问的规格讲清楚。这个点说明白了，咱们接着看刚才那款油。")
	if got != "这个点说明白了，咱们接着看刚才那款油。" {
		t.Fatalf("bridge tail=%q", got)
	}
	if got := extractFinalSpeechSentence("只有一句话没有可分离桥接。 "); got != "" {
		t.Fatalf("single sentence must not be marked as bridge, got %q", got)
	}
}

func TestOperatorVerbatimStillRequiresModelReview(t *testing.T) {
	store := readyVoiceStore()
	agent := &fakeAgent{responses: []string{"欢迎新来的朋友们，先看看今天直播间正在讲的内容。"}}
	worker := New(store, nil, agent, nil)
	got, err := worker.generateDecisionText(
		context.Background(),
		model.LiveRuntimeSession{TenantID: 7, RoomID: 11},
		decisionItem{
			ManualOrigin:  "agent_input",
			ExecutionMode: "verbatim",
			FixedText:     "欢迎新来的朋友",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if agent.calls != 1 {
		t.Fatalf("operator verbatim text must pass model review, calls=%d", agent.calls)
	}
	if got != "欢迎新来的朋友们，先看看今天直播间正在讲的内容" {
		t.Fatalf("expected reviewed text, got %q", got)
	}
	if len(agent.requests) != 1 || !strings.Contains(agent.requests[0].Messages[0].Content, "操作者上行播出前审核") {
		t.Fatalf("operator review guard missing: %#v", agent.requests)
	}
}

type fakeStore struct {
	profiles         []model.VoiceProfile
	versions         []model.AgentConfigVersion
	plan             model.LiveAgentPlan
	facts            []model.LiveAgentPlanFact
	scripts          []model.LiveAgentPlanScript
	publishedVersion *model.LiveAgentPlanVersion
}

func (f *fakeStore) RecordGeneratedSpeechHistory(context.Context, model.GeneratedSpeechHistoryInput) error {
	return nil
}

func (f *fakeStore) ListRunningLiveRuntimeSessions(context.Context) ([]model.LiveRuntimeSession, error) {
	return nil, nil
}

func (f *fakeStore) ListVoiceProfiles(context.Context, int64) ([]model.VoiceProfile, error) {
	return f.profiles, nil
}

func (f *fakeStore) ListLiveAgentConfigVersions(context.Context, int64) ([]model.AgentConfigVersion, error) {
	return f.versions, nil
}

func (f *fakeStore) GetVoiceProfile(_ context.Context, _ int64, profileID int64) (model.VoiceProfile, error) {
	for _, profile := range f.profiles {
		if profile.ID == profileID {
			return profile, nil
		}
	}
	return model.VoiceProfile{}, errors.New("voice profile not found")
}

func (f *fakeStore) GetPublishedLiveAgentPlanVersionForRoom(context.Context, int64, int64) (model.LiveAgentPlanVersion, error) {
	if f.publishedVersion == nil {
		return model.LiveAgentPlanVersion{}, appdb.ErrLiveAgentPlanVersionNotFound
	}
	return *f.publishedVersion, nil
}

func (f *fakeStore) LoadLivePolicyLayers(context.Context, int64, int64) (string, *model.LivePolicyVersion, *model.LivePolicyVersion, *model.LivePolicyVersion, error) {
	return "general", nil, nil, nil, nil
}

func (f *fakeStore) GetLiveAgentPlanForRoom(context.Context, int64, int64) (model.LiveAgentPlan, error) {
	if f.plan.ID == 0 {
		return model.LiveAgentPlan{}, errors.New("plan missing")
	}
	return f.plan, nil
}

func (f *fakeStore) ListLiveAgentPlanFacts(context.Context, int64, int64) ([]model.LiveAgentPlanFact, error) {
	return f.facts, nil
}

func (f *fakeStore) ListLiveAgentPlanScripts(context.Context, int64, int64) ([]model.LiveAgentPlanScript, error) {
	return f.scripts, nil
}

func (f *fakeStore) ListActiveAgentMemories(context.Context, int64, int64) ([]model.AgentMemoryItem, error) {
	return nil, nil
}

func (f *fakeStore) AgentPromptValue(_ context.Context, _ string, fallback string) string {
	return fallback
}

func (f *fakeStore) RenderAgentPrompt(_ context.Context, _ string, fallback string, variables map[string]string) string {
	value := fallback
	for key, replacement := range variables {
		value = strings.ReplaceAll(value, "{{"+key+"}}", replacement)
	}
	return value
}

type fakeCore struct {
	releases   int
	dispatches int
	dispatch   map[string]any
	claimRaw   string
	strategies map[string]coreStrategySelection
}

func (f *fakeCore) DoRoom(
	_ context.Context,
	_, _ int64,
	_ string,
	path string,
	_ url.Values,
	body any,
) (*http.Response, error) {
	status := http.StatusOK
	raw := `{"ok":true}`
	switch {
	case strings.Contains(path, "/strategy-select"):
		category := ""
		if value, ok := body.(map[string]any); ok {
			category = strings.TrimSpace(fmt.Sprint(value["category"]))
		}
		if selected, ok := f.strategies[category]; ok {
			payload, _ := json.Marshal(selected)
			raw = string(payload)
		} else {
			status = http.StatusNotFound
			raw = `{"error":"not configured"}`
		}
	case strings.HasSuffix(path, "/agent-decisions/claim"):
		if strings.TrimSpace(f.claimRaw) != "" {
			raw = f.claimRaw
		} else {
			raw = `{"claimed":true,"item":{"id":"d-1","topic":"FAMILY:发货物流","title":"发货物流","summary":"观众询问发货","sample_questions":["什么时候发货"],"manual_action":"answer"}}`
		}
	case strings.Contains(path, "/audio/interaction"):
		f.dispatches++
		if value, ok := body.(map[string]any); ok {
			f.dispatch = value
		}
		raw = `{"dispatched":true}`
	case strings.HasSuffix(path, "/release"):
		f.releases++
		raw = `{"ok":true}`
	default:
		status = http.StatusNotFound
		raw = `{"error":"not found"}`
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(raw)),
		Header:     make(http.Header),
	}, nil
}

type fakeAgent struct {
	calls     int
	err       error
	responses []string
	requests  []agentgateway.Request
}

func (f *fakeAgent) Complete(_ context.Context, request agentgateway.Request) (agentgateway.Response, error) {
	f.calls++
	f.requests = append(f.requests, request)
	if f.err != nil {
		return agentgateway.Response{}, f.err
	}
	if len(f.responses) >= f.calls {
		return agentgateway.Response{Text: f.responses[f.calls-1]}, nil
	}
	return agentgateway.Response{Text: "叔叔阿姨，这个问题我统一说一下，我们会按照当前直播间已经说明的安排来处理。"}, nil
}

type fakeTTS struct {
	calls int
	err   error
	last  ttsgateway.SynthesizeRequest
}

func (f *fakeTTS) SynthesizeURL(_ context.Context, request ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	f.calls++
	f.last = request
	if f.err != nil {
		return ttsgateway.SynthesizeResponse{}, f.err
	}
	return ttsgateway.SynthesizeResponse{AudioURL: "http://audio.local/reply.wav", VoiceID: "voice-1"}, nil
}

func readyVoiceStore() *fakeStore {
	return &fakeStore{
		plan: model.LiveAgentPlan{
			ID: 3, TenantID: 7, Name: "测试直播方案", Description: "测试回答策略",
			Status: "active",
			Terms:  []model.LiveAgentPlanTerm{{ID: 1, PlanID: 3, CanonicalText: "现做现发", Status: "active"}},
		},
		versions: []model.AgentConfigVersion{{
			ID: 1, LifecycleStatus: "active",
			SpeechConfig: map[string]any{"rooms": map[string]any{"11": map[string]any{
				"selected_voice": map[string]any{"source": "clone", "profile_id": float64(1)},
			}}},
		}},
		profiles: []model.VoiceProfile{{
			ID:          1,
			TenantID:    7,
			Name:        "默认声音",
			VoiceID:     "voice-1",
			CloneStatus: "ready",
			IsDefault:   true,
			Config:      map[string]any{"target_model": "qwen-audio-3.0-tts-plus"},
		}},
	}
}

func TestReadyVoiceFallsBackToSystemDefaultWhenRoomHasNoBinding(t *testing.T) {
	worker := New(&fakeStore{}, &fakeCore{}, &fakeAgent{}, &fakeTTS{})
	voice, ok, err := worker.readyVoice(context.Background(), 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || voice.VoiceID != voicecatalog.DefaultVoiceID {
		t.Fatalf("voice=%#v ok=%v want system default %s", voice, ok, voicecatalog.DefaultVoiceID)
	}
	if voiceModel(voice) != voicecatalog.SystemTTSModel {
		t.Fatalf("model=%q want=%q", voiceModel(voice), voicecatalog.SystemTTSModel)
	}
}

func TestReadyVoiceUsesRoomOfficialBinding(t *testing.T) {
	store := &fakeStore{versions: []model.AgentConfigVersion{{
		LifecycleStatus: "active",
		SpeechConfig: map[string]any{"rooms": map[string]any{"11": map[string]any{
			"selected_voice": map[string]any{"source": "official", "voice_id": "Serena"},
		}}},
	}}}
	worker := New(store, &fakeCore{}, &fakeAgent{}, &fakeTTS{})
	voice, ok, err := worker.readyVoice(context.Background(), 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || voice.VoiceID != "Serena" {
		t.Fatalf("voice=%#v ok=%v want Serena", voice, ok)
	}
}

func TestReadyVoiceUsesBoundCloneBeforeSystemDefault(t *testing.T) {
	store := readyVoiceStore()
	worker := New(store, &fakeCore{}, &fakeAgent{}, &fakeTTS{})
	voice, ok, err := worker.readyVoice(context.Background(), 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || voice.VoiceID != "voice-1" {
		t.Fatalf("voice=%#v ok=%v want room clone", voice, ok)
	}
}

func TestProcessRoomUsesPublishedVersionVoiceIdentity(t *testing.T) {
	store := readyVoiceStore()
	store.publishedVersion = &model.LiveAgentPlanVersion{
		ID:              44,
		TenantID:        7,
		PlanID:          3,
		RoomID:          11,
		VersionNo:       6,
		LifecycleStatus: "published",
		VoiceIdentity: model.LiveAgentVoiceIdentity{
			Name:     "发布主播声音",
			Version:  "V3",
			Source:   "clone",
			Provider: "aliyun_qwen_clone",
			VoiceID:  "published-voice-3",
			Model:    "published-model-3",
			Rate:     1.15,
		},
	}
	core := &fakeCore{}
	agent := &fakeAgent{}
	tts := &fakeTTS{}
	worker := New(store, core, agent, tts)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}

	if err := worker.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if tts.last.VoiceID != "published-voice-3" {
		t.Fatalf("voice_id=%q want published identity", tts.last.VoiceID)
	}
	if tts.last.Model != "published-model-3" {
		t.Fatalf("model=%q want published model", tts.last.Model)
	}
	if tts.last.Rate != 1.15 {
		t.Fatalf("rate=%v want 1.15", tts.last.Rate)
	}
	if tts.last.Provider != ttsgateway.ProviderQwen {
		t.Fatalf("provider=%q want %q", tts.last.Provider, ttsgateway.ProviderQwen)
	}
}

func TestProcessRoomGeneratesTTSAndDispatches(t *testing.T) {
	core := &fakeCore{}
	agent := &fakeAgent{}
	tts := &fakeTTS{}
	worker := New(readyVoiceStore(), core, agent, tts)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}

	if err := worker.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 1 || tts.calls != 1 || core.dispatches != 1 {
		t.Fatalf("calls agent=%d tts=%d dispatch=%d", agent.calls, tts.calls, core.dispatches)
	}
	if core.releases != 0 {
		t.Fatalf("successful dispatch must stay claimed until playback callback, releases=%d", core.releases)
	}
	if got := core.dispatch["decision_id"]; got != "d-1" {
		t.Fatalf("decision_id=%v", got)
	}
	if got := core.dispatch["action"]; got != "answer" {
		t.Fatalf("action=%v", got)
	}
	if got := core.dispatch["audio_url"]; got != "http://audio.local/reply.wav" {
		t.Fatalf("audio_url=%v", got)
	}
}

func TestProcessRoomUsesSameInterruptStrategyForModelTTSAndCore(t *testing.T) {
	core := &fakeCore{strategies: map[string]coreStrategySelection{
		"interrupt":  {Category: "interrupt", Key: "read_comment_softly", Name: "小声读一次弹幕"},
		"addressing": {Category: "addressing", Key: "friend", Name: "朋友"},
		"resume":     {Category: "resume", Key: "DIRECT", Name: "直接恢复"},
	}}
	agent := &fakeAgent{responses: []string{"朋友，你问的是发货时间哈，一般会按当前页面说明安排。"}}
	tts := &fakeTTS{}
	worker := New(readyVoiceStore(), core, agent, tts)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}

	if err := worker.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tts.last.Instruction, "轻声接弹幕") {
		t.Fatalf("TTS instruction did not receive interrupt style: %q", tts.last.Instruction)
	}
	if got := strings.TrimSpace(fmt.Sprint(core.dispatch["interrupt_strategy"])); got != "read_comment_softly" {
		t.Fatalf("core interrupt_strategy=%q", got)
	}
	if len(agent.requests) == 0 || !strings.Contains(agent.requests[0].Messages[1].Content, "轻声、自然地读一下或复述当前弹幕") {
		t.Fatalf("model prompt did not receive hidden interrupt guidance: %#v", agent.requests)
	}
}

func TestProcessRoomRewritesAbsoluteClaimBeforeTTS(t *testing.T) {
	core := &fakeCore{}
	agent := &fakeAgent{responses: []string{
		"放心拍，咱们这个品质保障，绝对不踩雷！",
		"喜欢这类商品的家人可以看看，选品会认真把关，按页面信息放心选择。",
	}}
	tts := &fakeTTS{}
	worker := New(readyVoiceStore(), core, agent, tts)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}

	if err := worker.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 2 {
		t.Fatalf("agent calls=%d want generation + rule-layer review", agent.calls)
	}
	if strings.Contains(tts.last.Text, "绝对") || strings.Contains(tts.last.Text, "不踩雷") {
		t.Fatalf("unsafe absolute claim reached TTS: %q", tts.last.Text)
	}
	if strings.Contains(tts.last.Text, "品质保障") {
		t.Fatalf("unsupported guarantee reached TTS: %q", tts.last.Text)
	}
}

func TestVerbatimModeStillPassesRuleLayerFinalGate(t *testing.T) {
	core := &fakeCore{
		claimRaw: `{"claimed":true,"item":{"id":"d-v","topic":"人工指令","title":"智能体输入抢答","summary":"100%原话","sample_questions":["严格100%原话"],"manual_action":"quick","manual_origin":"agent_input","execution_mode":"verbatim","fixed_text":"这个绝对不踩雷，保证你满意"}}`,
	}
	agent := &fakeAgent{responses: []string{
		"这个可以根据页面信息和自己的需要来选，希望能让你满意。",
	}}
	tts := &fakeTTS{}
	worker := New(readyVoiceStore(), core, agent, tts)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}

	if err := worker.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 1 {
		t.Fatalf("verbatim should skip generation but run one rule-layer review, calls=%d", agent.calls)
	}
	if strings.Contains(tts.last.Text, "绝对") || strings.Contains(tts.last.Text, "保证") {
		t.Fatalf("verbatim bypassed rule-layer gate: %q", tts.last.Text)
	}
	if got := core.dispatch["action"]; got != "quick" {
		t.Fatalf("action=%v want quick", got)
	}
}

func TestProcessRoomCapsFinalSpeechAtThreeHundredRunes(t *testing.T) {
	core := &fakeCore{}
	longText := strings.Repeat("这是一段直播回答内容。", 80)
	agent := &fakeAgent{responses: []string{longText}}
	tts := &fakeTTS{}
	worker := New(readyVoiceStore(), core, agent, tts)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}

	if err := worker.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if got := len([]rune(tts.last.Text)); got > maxSpeechRunes {
		t.Fatalf("TTS text length=%d want<=%d text=%q", got, maxSpeechRunes, tts.last.Text)
	}
}

func TestLimitSpeechTextPrefersRecentSentenceBoundary(t *testing.T) {
	prefix := strings.Repeat("甲", 250) + "。"
	text := prefix + strings.Repeat("乙", 100)
	got := limitSpeechText(text, 300)
	if got != prefix {
		t.Fatalf("got length=%d want sentence boundary length=%d", len([]rune(got)), len([]rune(prefix)))
	}
}

func TestLiveAgentPlanPromptContextPrioritizesPlanFacts(t *testing.T) {
	plan := model.LiveAgentPlan{
		ID:          8,
		Name:        "跑山鸡直播方案",
		Description: "物流问题统一回答：发圆通快递。",
		Terms: []model.LiveAgentPlanTerm{{
			ID: 1, PlanID: 8, CanonicalText: "圆通快递", TermType: "logistics", Note: "观众问发什么快递时直接回答发圆通", Status: "active",
			Variants: []model.LiveAgentPlanTermVariant{{VariantText: "我们发圆通"}},
		}},
	}
	got := liveAgentPlanPromptContext(plan)
	for _, want := range []string{
		"事实与回答口径，优先执行",
		"物流问题统一回答：发圆通快递。",
		"圆通快递",
		"观众问发什么快递时直接回答发圆通",
		"我们发圆通",
		"不得",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("plan prompt missing %q: %s", want, got)
		}
	}
}

func TestAnswerPromptReadsLatestDynamicFactsAndHostStyle(t *testing.T) {
	store := readyVoiceStore()
	store.facts = []model.LiveAgentPlanFact{
		{
			ID: 21, PlanID: 3, TenantID: 7, Category: "fulfillment", Key: "快递",
			Value: "统一发圆通快递", Status: "active", VersionNo: 7,
		},
	}
	store.scripts = []model.LiveAgentPlanScript{
		{
			ID: 8, TenantID: 7, PlanID: 3, Status: "active", AnalysisStatus: "analyzed",
			Analysis: model.LiveAgentPlanScriptAnalysis{AnchorStyle: model.LiveAgentPlanAnchorStyleProfile{
				Summary: "像真人主播一样短句、自然承接",
				Dimensions: []model.LiveAgentPlanAnchorStyleDimension{
					{Key: "pace", Label: "节奏", Rule: "短句为主，关键事实稍微放慢"},
				},
				ReusableRules: []string{"先接住观众，再给明确答案"},
			}},
		},
	}
	worker := New(store, nil, &fakeAgent{}, nil)
	prompt, err := worker.answerPrompt(
		context.Background(),
		model.LiveRuntimeSession{TenantID: 7, RoomID: 11},
		decisionItem{SampleQuestions: []string{"你们发什么快递？"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"当前方案动态事实：实时热更新",
		"快递：统一发圆通快递（V7）",
		"当前主播风格：实时热更新",
		"像真人主播一样短句、自然承接",
		"短句为主，关键事实稍微放慢",
		"先接住观众，再给明确答案",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("hot prompt missing %q: %s", want, prompt)
		}
	}
}

func TestFinalSpeechRisksFlagsUnsupportedShippingClaims(t *testing.T) {
	text := "咱们默认安排合作的主流快递，一般48小时内会发出，具体由系统匹配和仓库实际发货为准。"
	_, reasons := finalSpeechRisks(text, "观众问：发什么快递", model.LiveEffectivePolicy{})
	joined := strings.Join(reasons, "；")
	for _, want := range []string{"主流快递", "48小时内", "系统匹配", "仓库实际发货"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("shipping risk %q not detected: %s", want, joined)
		}
	}
}

func TestFinalSpeechRisksAllowsShippingFactWhenPlanProvidesIt(t *testing.T) {
	text := "我们发圆通快递。"
	_, reasons := finalSpeechRisks(text, "方案说明：物流问题统一回答：发圆通快递。", model.LiveEffectivePolicy{})
	if len(reasons) != 0 {
		t.Fatalf("plan-backed shipping fact should not be flagged: %#v", reasons)
	}
}

func TestProcessRoomReleasesClaimWhenTTSFails(t *testing.T) {
	core := &fakeCore{}
	agent := &fakeAgent{}
	tts := &fakeTTS{err: errors.New("tts unavailable")}
	worker := New(readyVoiceStore(), core, agent, tts)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}

	if err := worker.processRoom(context.Background(), session); err == nil {
		t.Fatal("expected TTS failure")
	}
	if core.dispatches != 0 {
		t.Fatalf("failed TTS must not dispatch, dispatches=%d", core.dispatches)
	}
	if core.releases != 1 {
		t.Fatalf("failed execution must release claim once, releases=%d", core.releases)
	}
}
