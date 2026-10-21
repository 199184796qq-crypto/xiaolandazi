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
	"time"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechmission"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/voicecatalog"
)

func TestStrategyStageOrderCanBeReorderedWithoutDroppingRegisteredStages(t *testing.T) {
	registry := map[string]strategyStageFunc{
		"interrupt":  nil,
		"addressing": nil,
		"resume":     nil,
		"humanize":   nil,
	}
	got := normalizeStrategyStageOrder("resume,addressing,interrupt", registry)
	want := []string{"resume", "addressing", "interrupt", "humanize"}
	if len(got) != len(want) {
		t.Fatalf("order=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order=%v want=%v", got, want)
		}
	}
}

func TestOpeningIntentSeparatesSimpleAndComplexQuestions(t *testing.T) {
	simpleKey, _, simpleGuidance := openingIntentForMission(decisionItem{
		MissionKind:     "reply_chat",
		SampleQuestions: []string{"多少钱"},
	})
	if simpleKey != "direct_answer" || !strings.Contains(simpleGuidance, "直接接住") {
		t.Fatalf("simple opening=%q guidance=%q", simpleKey, simpleGuidance)
	}

	complexKey, _, complexGuidance := openingIntentForMission(decisionItem{
		MissionKind: "reply_chat",
		SampleQuestions: []string{
			"这个规格怎么选，家里三个人吃应该买哪种？",
			"另外发货和售后分别是什么规则？",
		},
	})
	if complexKey != "light_restate" || !strings.Contains(complexGuidance, "复述确认重点") {
		t.Fatalf("complex opening=%q guidance=%q", complexKey, complexGuidance)
	}
}

func TestAddressingModeUsesMissionScopeWithoutForcingNames(t *testing.T) {
	tests := []struct {
		name      string
		item      decisionItem
		candidate string
		want      string
	}{
		{name: "none without candidate", item: decisionItem{MissionKind: "reply_chat"}, candidate: "", want: "NONE"},
		{name: "single welcome", item: decisionItem{MissionKind: "welcome_named", MissionEventCount: 1}, candidate: "朋友", want: "SINGLE"},
		{name: "batch welcome", item: decisionItem{MissionKind: "welcome_batch", MissionEventCount: 12}, candidate: "朋友们", want: "GROUP"},
		{name: "batched chat", item: decisionItem{MissionKind: "reply_chat", MissionEventCount: 3}, candidate: "大家", want: "GROUP"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := addressingModeForMission(tt.item, tt.candidate); got != tt.want {
				t.Fatalf("mode=%q want=%q", got, tt.want)
			}
		})
	}

	if guidance := addressingGuidance("NONE", ""); !strings.Contains(guidance, "不要裸奔") {
		t.Fatalf("NONE guidance must avoid bare addressing: %s", guidance)
	}
	if guidance := addressingGuidance("GROUP", "朋友们"); !strings.Contains(guidance, "不要逐个报名字") {
		t.Fatalf("GROUP guidance must suppress name listing: %s", guidance)
	}
}

func TestOpeningPlanIsFrozenIntoSpeechMission(t *testing.T) {
	worker := New(readyVoiceStore(), nil, &fakeAgent{}, nil)
	item := &decisionItem{
		ID:              "decision-opening-1",
		MissionKind:     "reply_chat",
		SampleQuestions: []string{"这个规格怎么选？"},
	}
	session := model.LiveRuntimeSession{ID: 3, TenantID: 7, RoomID: 11}
	worker.ensureMission(session, item)
	worker.applyOpeningStrategyStage(context.Background(), session, "seed", item)
	mission, ok := worker.MissionSnapshot(item.MissionID)
	if !ok {
		t.Fatal("mission snapshot missing")
	}
	if mission.Opening.Intent != "direct_answer" || !mission.Opening.Required {
		t.Fatalf("unexpected opening plan: %#v", mission.Opening)
	}
}

func TestInteractionDecisionIsFrozenIntoSpeechMission(t *testing.T) {
	worker := New(readyVoiceStore(), nil, &fakeAgent{}, nil)
	item := &decisionItem{
		ID:                   "decision-value-1",
		MissionKind:          "reply_chat",
		MissionEventCount:    2,
		MissionWindowSeconds: 18,
		InteractionDecision: speechmission.InteractionDecisionPlan{
			Handle:           true,
			PrimaryEvent:     "question",
			MergedEventIDs:   []int64{101, 102},
			EventValue:       96.5,
			ValueLevel:       "HIGH",
			Reason:           "重复问题进入高价值保留通道",
			BudgetLevel:      "LOW",
			BudgetAllowed:    true,
			Heat:             "HOT",
			PreferenceFactor: 1.38,
			QuestionDebt: &speechmission.QuestionDebtPlan{
				Topic:           "FAMILY:价格费用",
				RepeatCount:     2,
				UniqueUsers:     2,
				WaitingSeconds:  18,
				BusinessValue:   0.92,
				CurrentPriority: 88,
			},
		},
	}
	session := model.LiveRuntimeSession{ID: 3, TenantID: 7, RoomID: 11}
	worker.ensureMission(session, item)
	worker.applyInteractionStrategyStage(context.Background(), session, "seed", item)
	mission, ok := worker.MissionSnapshot(item.MissionID)
	if !ok {
		t.Fatal("mission snapshot missing")
	}
	decision := mission.Interaction.Decision
	if decision.EventValue != 96.5 || decision.Heat != "HOT" || !decision.BudgetAllowed || decision.BudgetLevel != "LOW" {
		t.Fatalf("interaction decision not frozen: %#v", decision)
	}
	if decision.QuestionDebt == nil || decision.QuestionDebt.RepeatCount != 2 || decision.QuestionDebt.UniqueUsers != 2 {
		t.Fatalf("question debt not frozen: %#v", decision.QuestionDebt)
	}
	if len(decision.MergedEventIDs) != 2 {
		t.Fatalf("merged events not frozen: %#v", decision.MergedEventIDs)
	}
}

func TestNormalizePlayableNicknameFiltersUnsafeOrUnreadableNames(t *testing.T) {
	accepted := []string{"小陈", "阿芳88", "回忆哥"}
	for _, raw := range accepted {
		if got, ok := normalizePlayableNickname(raw); !ok || got == "" {
			t.Fatalf("expected playable nickname %q, got %q ok=%v", raw, got, ok)
		}
	}
	rejected := []string{
		"微信加我123",
		"www.test.com",
		"！！！！",
		"这是一个特别特别特别特别长的昵称",
	}
	for _, raw := range rejected {
		if got, ok := normalizePlayableNickname(raw); ok {
			t.Fatalf("expected nickname %q rejected, got %q", raw, got)
		}
	}
}

func TestBuildAddressingPlanUsesMixedForBatchWelcome(t *testing.T) {
	worker := New(readyVoiceStore(), nil, &fakeAgent{}, nil)
	worker.now = func() time.Time { return time.Date(2026, 9, 29, 18, 40, 0, 0, time.UTC) }
	plan := worker.buildAddressingPlan(11, decisionItem{
		MissionKind:       "welcome_batch",
		MissionEventCount: 12,
		Nicknames:         []string{"小陈", "阿芳", "老周"},
	}, "friend", "朋友们", model.RoomAddressingPreferences{NamingPreference: "natural"}, nil)
	if plan.Mode != "MIXED" {
		t.Fatalf("mode=%q want MIXED, plan=%#v", plan.Mode, plan)
	}
	if len(plan.SelectedNames) != 3 || plan.SelectedNames[0] != "小陈" || plan.SelectedNames[1] != "阿芳" || plan.SelectedNames[2] != "老周" {
		t.Fatalf("selected names=%v want first three playable names", plan.SelectedNames)
	}
	if plan.MaxNamedCount != 3 || plan.GroupLabel != "朋友们" {
		t.Fatalf("unexpected addressing bounds: %#v", plan)
	}
	if guidance := addressingPlanGuidance(plan); !strings.Contains(guidance, "小陈、阿芳、老周") || !strings.Contains(guidance, "朋友们") {
		t.Fatalf("mixed guidance missing selected names/group: %s", guidance)
	}
}

func TestAddressingPreferenceLessAvoidsNamesForAggregateMission(t *testing.T) {
	worker := New(readyVoiceStore(), nil, &fakeAgent{}, nil)
	plan := worker.buildAddressingPlan(11, decisionItem{
		MissionKind:       "welcome_batch",
		MissionEventCount: 8,
		Nicknames:         []string{"小陈", "阿芳"},
	}, "friend", "朋友们", model.RoomAddressingPreferences{NamingPreference: "less"}, nil)
	if plan.Mode != "GROUP" || len(plan.SelectedNames) != 0 || plan.MaxNamedCount != 1 {
		t.Fatalf("less preference should reduce naming: %#v", plan)
	}
}

func TestAddressingPreferenceMoreUsesMixedWhenAggregateQuestionHasNames(t *testing.T) {
	worker := New(readyVoiceStore(), nil, &fakeAgent{}, nil)
	plan := worker.buildAddressingPlan(11, decisionItem{
		MissionKind:       "reply_chat",
		MissionEventCount: 3,
		Nicknames:         []string{"小陈", "阿芳"},
		SampleQuestions:   []string{"多少钱", "怎么发货"},
	}, "friends", "朋友们", model.RoomAddressingPreferences{NamingPreference: "more"}, nil)
	if plan.Mode != "MIXED" || len(plan.SelectedNames) != 2 {
		t.Fatalf("more preference should use safe mixed addressing: %#v", plan)
	}
}

func TestAddressingFrequencyDistribution(t *testing.T) {
	cases := []struct {
		preference string
		minCount   int
		maxCount   int
	}{
		{preference: "less", minCount: 5, maxCount: 25},
		{preference: "natural", minCount: 25, maxCount: 45},
		{preference: "more", minCount: 60, maxCount: 80},
	}
	for _, tc := range cases {
		count := 0
		for i := 0; i < 100; i++ {
			plan := speechmission.AddressingPlan{
				Mode:       "SINGLE",
				Candidate:  "老哥",
				GroupLabel: "老哥",
				Preference: tc.preference,
				Optional:   true,
			}
			item := decisionItem{
				ID:          fmt.Sprintf("decision-%03d", i),
				MissionKind: "reply_chat",
			}
			got := applyAddressingFrequency(plan, item, fmt.Sprintf("seed-%03d", i))
			if got.SelectedByRate {
				count++
				if got.Optional || got.Mode == "NONE" {
					t.Fatalf("%s selected round must be required: %#v", tc.preference, got)
				}
			}
		}
		t.Logf("addressing frequency %s: %d/100", tc.preference, count)
		if count < tc.minCount || count > tc.maxCount {
			t.Fatalf("%s frequency count=%d want within [%d,%d]", tc.preference, count, tc.minCount, tc.maxCount)
		}
	}
}

func TestPreferredAddressingTermsOverrideSystemCandidate(t *testing.T) {
	worker := New(readyVoiceStore(), nil, &fakeAgent{}, nil)
	plan := worker.buildAddressingPlan(11, decisionItem{
		ID:          "decision-preferred-term",
		MissionKind: "reply_chat",
	}, "friend", "朋友", model.RoomAddressingPreferences{
		NamingPreference: "more",
		PreferredTerms:   []string{"老哥", "朋友", "姐妹", "老妹"},
		BlockedTerms:     []string{"老板", "宝子"},
	}, nil)
	if plan.Candidate == "" {
		t.Fatal("preferred addressing candidate should not be empty")
	}
	if !containsString([]string{"老哥", "朋友", "姐妹", "老妹"}, plan.Candidate) {
		t.Fatalf("candidate=%q not selected from preferred terms", plan.Candidate)
	}
	if plan.Key != "preferred" {
		t.Fatalf("preferred term should mark key=preferred, got %q", plan.Key)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestRoomAddressingPreferencesIgnoreBlockedBackendCandidate(t *testing.T) {
	worker := New(readyVoiceStore(), nil, &fakeAgent{}, nil)
	plan := worker.buildAddressingPlan(11, decisionItem{
		ID:                "dynamic-room-addressing",
		MissionKind:       "welcome_batch",
		MissionEventCount: 3,
		Nicknames:         []string{"小陈"},
	}, "backend_babies", "宝子们", model.RoomAddressingPreferences{
		NamingPreference: "natural",
		PreferredTerms:   []string{"朋友"},
		BlockedTerms:     []string{"宝子"},
	}, nil)
	if plan.Candidate != "朋友" || plan.Key != "preferred" {
		t.Fatalf("room preference must replace backend candidate: %#v", plan)
	}
	guidance := addressingPlanGuidance(plan)
	if !strings.Contains(guidance, "朋友") || !strings.Contains(guidance, "禁止使用") || !strings.Contains(guidance, "宝子") {
		t.Fatalf("guidance must carry room preferred and blocked terms: %s", guidance)
	}
}

func TestAddressingPreferredTermIsAudienceOnlyGuidance(t *testing.T) {
	guidance := addressingPlanGuidance(speechmission.AddressingPlan{
		Mode:           "SINGLE",
		PreferredTerms: []string{"四川老妹"},
		Optional:       true,
	})
	for _, expected := range []string{"称呼观众/对方", "不代表主播身份", "主播自称"} {
		if !strings.Contains(guidance, expected) {
			t.Fatalf("addressing guidance missing %q: %s", expected, guidance)
		}
	}
}

func TestHostStateDeliveryGuidanceConvertsSymptomsToBehavior(t *testing.T) {
	guidance := hostStateDeliveryGuidance("今天有点不舒服，有点咳嗽")
	for _, expected := range []string{"语速稍慢", "句子缩短", "声音略轻", "停顿更自然"} {
		if !strings.Contains(guidance, expected) {
			t.Fatalf("state guidance missing %q: %s", expected, guidance)
		}
	}
	for _, forbidden := range []string{"今天有点不舒服", "咳嗽", "感冒"} {
		if strings.Contains(guidance, forbidden) {
			t.Fatalf("raw symptom text must not be used as delivery guidance: %q in %s", forbidden, guidance)
		}
	}
}

func TestRecentAddressingSuppressionOnlyRecordsActuallySpokenNames(t *testing.T) {
	worker := New(readyVoiceStore(), nil, &fakeAgent{}, nil)
	now := time.Date(2026, 9, 29, 18, 40, 0, 0, time.UTC)
	worker.now = func() time.Time { return now }

	worker.recordUsedAddressingNames(11, []string{"小陈", "阿芳"}, "小陈，欢迎你，咱们继续看今天这个产品。")
	names, penalty := worker.playableAddressingNames(11, []string{"小陈", "阿芳"})
	if len(names) != 1 || names[0] != "阿芳" {
		t.Fatalf("recent-name suppression=%v want [阿芳]", names)
	}
	if penalty <= 0 {
		t.Fatalf("expected positive recent-name penalty, got %v", penalty)
	}

	now = now.Add(addressingNameCooldown + time.Second)
	names, penalty = worker.playableAddressingNames(11, []string{"小陈", "阿芳"})
	if len(names) != 2 || penalty != 0 {
		t.Fatalf("cooldown should expire, names=%v penalty=%v", names, penalty)
	}
}

func TestBuildHumanStyleMissionPlanSeparatesTraitStateReaction(t *testing.T) {
	plan := buildHumanStyleMissionPlan(
		decisionItem{MissionKind: "reply_chat"},
		selectedHumanizationPlan{
			Strategy:    "humanization.repeat_fragment",
			Enabled:     true,
			Kind:        "REPEAT_FRAGMENT",
			Delivery:    "TEXT_DIRECTIVE",
			Instruction: "允许自然重复一个短关键词一次",
			MaxCount:    1,
			Reason:      "anchor style prefers occasional natural repetition",
			Heat:        "warm",
			Progress:    "CONVERSION",
			Atmosphere:  "ACTIVE",
		},
		model.RoomHumanBehaviorProfile{TraitText: "旧版直播间习惯不再参与生成", StateText: "今天声音偏轻"},
		"保持真人直播临场感",
		true,
	)
	if plan.Trait.Persona != "natural_live_anchor" || plan.Trait.MaxReactionCount != 1 || plan.Trait.Instruction != "" {
		t.Fatalf("trait not frozen correctly: %#v", plan.Trait)
	}
	if plan.State.Heat != "warm" || plan.State.Progress != "CONVERSION" || plan.State.MissionKind != "reply_chat" || plan.State.HostState != "今天声音偏轻" {
		t.Fatalf("state not frozen correctly: %#v", plan.State)
	}
	if !plan.Reaction.Enabled || plan.Reaction.Kind != "REPEAT_FRAGMENT" || plan.Reaction.MaxCount != 1 {
		t.Fatalf("reaction not frozen correctly: %#v", plan.Reaction)
	}
}

func TestBuildHumanStyleMissionPlanDisablesReactionInFallback(t *testing.T) {
	plan := buildHumanStyleMissionPlan(
		decisionItem{MissionKind: "reply_follow"},
		selectedHumanizationPlan{Strategy: "fallback.natural", Reason: "core_humanization_unavailable"},
		model.RoomHumanBehaviorProfile{},
		"保持干净自然",
		false,
	)
	if plan.Reaction.Enabled || plan.Reaction.MaxCount != 0 || plan.Reaction.Kind != "NONE" {
		t.Fatalf("fallback must not invent reaction: %#v", plan.Reaction)
	}
	if plan.Trait.Persona == "" || plan.State.MissionKind != "reply_follow" {
		t.Fatalf("trait/state should remain available in fallback: %#v", plan)
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

func TestOperatorVerbatimSkipsReviewFixedTextReachesTTS(t *testing.T) {
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
	// TTS 前审核已去掉：操作者 verbatim 文字不再过模型审核，直接返回固定文字。
	if agent.calls != 0 {
		t.Fatalf("operator verbatim text must skip model review now, calls=%d", agent.calls)
	}
	if got != "欢迎新来的朋友" {
		t.Fatalf("expected fixed text unchanged, got %q", got)
	}
}

type fakeStore struct {
	profiles         []model.VoiceProfile
	versions         []model.AgentConfigVersion
	plan             model.LiveAgentPlan
	facts            []model.LiveAgentPlanFact
	scripts          []model.LiveAgentPlanScript
	publishedVersion *model.LiveAgentPlanVersion
	humanProfile     model.RoomHumanBehaviorProfile
	addressingPrefs  model.RoomAddressingPreferences
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

func (f *fakeStore) GetRoomHumanBehaviorProfile(context.Context, int64, int64) (model.RoomHumanBehaviorProfile, error) {
	return f.humanProfile, nil
}

func (f *fakeStore) GetRoomAddressingPreferences(context.Context, int64, int64) (model.RoomAddressingPreferences, error) {
	if strings.TrimSpace(f.addressingPrefs.NamingPreference) == "" {
		return model.RoomAddressingPreferences{NamingPreference: "natural"}, nil
	}
	return f.addressingPrefs, nil
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
	releases       int
	dispatches     int
	dispatch       map[string]any
	dispatchStatus int
	dispatchRaw    string
	claimRaw       string
	runtimeRaw     string
	releaseReasons []string
	strategies     map[string]coreStrategySelection
	strategyCalls  map[string]int
}

func (f *fakeCore) DoRoom(
	_ context.Context,
	_, _ int64,
	_ string,
	path string,
	query url.Values,
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
		if f.strategyCalls == nil {
			f.strategyCalls = make(map[string]int)
		}
		f.strategyCalls[category]++
		if selected, ok := f.strategies[category]; ok {
			payload, _ := json.Marshal(selected)
			raw = string(payload)
		} else {
			status = http.StatusNotFound
			raw = `{"error":"not configured"}`
		}
	case strings.HasSuffix(path, "/speech-runtime"):
		if strings.TrimSpace(f.runtimeRaw) != "" {
			raw = f.runtimeRaw
		} else {
			raw = `{"interrupt":{"status":"idle"}}`
		}
	case strings.HasSuffix(path, "/agent-decisions/claim"):
		if strings.TrimSpace(f.claimRaw) != "" {
			raw = f.claimRaw
		} else {
			raw = `{"claimed":true,"item":{"id":"d-1","topic":"FAMILY:发货物流","title":"发货物流","summary":"观众询问发货","sample_questions":["什么时候发货"],"manual_action":"answer"}}`
		}
	case strings.Contains(path, "/audio/interaction"):
		f.dispatches++
		if f.dispatchStatus != 0 {
			status = f.dispatchStatus
			raw = f.dispatchRaw
			if strings.TrimSpace(raw) == "" {
				raw = `{"error":"dispatch failed"}`
			}
			break
		}
		if value, ok := body.(map[string]any); ok {
			f.dispatch = value
			payload := map[string]any{
				"mission_id":       strings.TrimSpace(fmt.Sprint(value["mission_id"])),
				"dispatched":       true,
				"action":           strings.TrimSpace(fmt.Sprint(value["action"])),
				"switch_at_ms":     1234,
				"resume_offset_ms": 2345,
				"resume_strategy":  strings.TrimSpace(fmt.Sprint(value["resume_strategy"])),
				"bridge_used":      strings.EqualFold(strings.TrimSpace(fmt.Sprint(value["resume_strategy"])), "BRIDGE"),
				"dedup_triggered":  false,
				"duplicate_score":  0,
			}
			encoded, _ := json.Marshal(payload)
			raw = string(encoded)
		} else {
			raw = `{"dispatched":true}`
		}
	case strings.HasSuffix(path, "/release"):
		f.releases++
		f.releaseReasons = append(f.releaseReasons, query.Get("reason"))
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
	legacyEmotionEnabled := true
	store.publishedVersion = &model.LiveAgentPlanVersion{
		ID:              44,
		TenantID:        7,
		PlanID:          3,
		RoomID:          11,
		VersionNo:       6,
		LifecycleStatus: "published",
		VoiceIdentity: model.LiveAgentVoiceIdentity{
			EmotionEnabled: &legacyEmotionEnabled,
			Name:           "发布主播声音",
			Version:        "V3",
			Source:         "clone",
			Provider:       "aliyun_qwen_clone",
			VoiceID:        "published-voice-3",
			Model:          "published-model-3",
			Rate:           1.15,
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
	if tts.last.Instruction != "" {
		t.Fatalf("legacy enabled flag restored emotion instructions: %q", tts.last.Instruction)
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
	if tts.last.Instruction != "" {
		t.Fatalf("unconfigured room enabled emotion instructions: %q", tts.last.Instruction)
	}
}

func TestProcessRoomUsesSameInterruptStrategyForModelTTSAndCore(t *testing.T) {
	core := &fakeCore{strategies: map[string]coreStrategySelection{
		"interrupt": {Category: "interrupt", Key: "read_comment_softly", Name: "小声读一次弹幕"},
		"resume":    {Category: "resume", Key: "DIRECT", Name: "直接恢复"},
	}}
	agent := &fakeAgent{responses: []string{"朋友，你问的是发货时间哈，一般会按当前页面说明安排。"}}
	tts := &fakeTTS{}
	worker := New(readyVoiceStore(), core, agent, tts)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}

	if err := worker.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if core.strategyCalls["addressing"] != 0 {
		t.Fatalf("dynamic room reply must not call backend/core addressing strategy, calls=%d", core.strategyCalls["addressing"])
	}
	if tts.last.Instruction != "" {
		t.Fatalf("disabled emotion still received interrupt delivery style: %q", tts.last.Instruction)
	}
	if got := strings.TrimSpace(fmt.Sprint(core.dispatch["interrupt_strategy"])); got != "read_comment_softly" {
		t.Fatalf("core interrupt_strategy=%q", got)
	}
}

func TestProcessRoomBuildsSpeechMissionBlackboard(t *testing.T) {
	core := &fakeCore{
		claimRaw: `{"claimed":true,"item":{"id":"d-mission","topic":"INTERACTION:CHAT","title":"回复弹幕","summary":"互动时间窗触发","reply_hint":"一次生成完整可播正文","sample_questions":["这个怎么发货"],"mission_kind":"reply_chat","mission_event_count":2,"mission_window_seconds":12,"manual_action":"answer"},"switch_at_ms":900,"current_mainline":"刚才在讲压榨工艺","resume_mainline":"下一句继续讲工艺细节","resume_segment_id":"seg-2"}`,
		strategies: map[string]coreStrategySelection{
			"interrupt": {Category: "interrupt", Key: "read_comment_softly", Name: "轻声接弹幕"},
			"resume": {
				Category: "resume", Key: "BRIDGE", Name: "自然桥接",
				PlannedCutMS: 900, ResumeOffsetMS: 2400, ResumeReason: "same_safe_boundary",
				ResumePreview: "继续讲压榨工艺细节", ResumeSegmentID: "seg-r2",
			},
		},
	}
	agent := &fakeAgent{responses: []string{"朋友，这个发货问题我给你说明一下，会按当前页面安排；说回刚才这个工艺，关键还是看压榨环节。"}}
	tts := &fakeTTS{}
	worker := New(readyVoiceStore(), core, agent, tts)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}

	if err := worker.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(fmt.Sprint(core.dispatch["mission_id"])); got != "d-mission" {
		t.Fatalf("mission_id=%q want d-mission", got)
	}
	mission, ok := worker.MissionSnapshot("d-mission")
	if !ok {
		t.Fatal("speech mission snapshot missing")
	}
	if mission.State != speechmission.StateDispatched {
		t.Fatalf("mission state=%s want %s", mission.State, speechmission.StateDispatched)
	}
	if mission.PlanFrozenAt == nil {
		t.Fatal("mission plan was not frozen before generation")
	}
	if mission.Interaction.Kind != "reply_chat" || mission.Interaction.EventCount != 2 {
		t.Fatalf("interaction plan=%#v", mission.Interaction)
	}
	if mission.Interrupt.Strategy != "read_comment_softly" {
		t.Fatalf("interrupt plan=%#v", mission.Interrupt)
	}
	if mission.Resume.Strategy != "BRIDGE" || strings.TrimSpace(mission.Resume.BridgeText) != "" || !strings.Contains(mission.Resume.Guidance, "不提前加入具体桥话术") {
		t.Fatalf("resume plan=%#v", mission.Resume)
	}
	if mission.Resume.PlannedResumeAtMS != 2400 || mission.Resume.ResumeSegmentID != "seg-r2" || !strings.Contains(mission.Resume.ResumePreview, "压榨工艺") {
		t.Fatalf("resume pre-plan context=%#v", mission.Resume)
	}
	if mission.Resume.ActualResumeAtMS != 2345 {
		t.Fatalf("actual resume point=%d want 2345", mission.Resume.ActualResumeAtMS)
	}
	if mission.Addressing.Mode != "GROUP" || mission.Addressing.Candidate != "各位" || !mission.Addressing.Optional {
		t.Fatalf("dynamic addressing must come only from room preferences: %#v", mission.Addressing)
	}
	if core.strategyCalls["addressing"] != 0 {
		t.Fatalf("dynamic speech mission must not call backend/core addressing strategy, calls=%d", core.strategyCalls["addressing"])
	}
	if mission.HumanStyle.Mode == "natural_live_speech" || mission.HumanStyle.Reaction.Enabled {
		t.Fatalf("disabled emotion still ran humanization: %#v", mission.HumanStyle)
	}
	if strings.TrimSpace(mission.GeneratedText) == "" || strings.TrimSpace(mission.TTS.AudioURL) == "" {
		t.Fatalf("generated text or tts missing: %#v", mission)
	}
	if mission.Mainline.SwitchAtMS != 1234 {
		t.Fatalf("actual switch point=%d want 1234", mission.Mainline.SwitchAtMS)
	}
	if tts.last.Instruction != "" {
		t.Fatalf("disabled emotion still produced human style instructions: %q", tts.last.Instruction)
	}
	if len(mission.Trace) < 8 {
		t.Fatalf("mission trace too short: %d", len(mission.Trace))
	}
}

func TestReconcileRoomMissionMarksPlaybackCompleted(t *testing.T) {
	core := &fakeCore{
		claimRaw:   `{"claimed":false,"reason":"empty"}`,
		runtimeRaw: `{"room_id":11,"interrupt":{"status":"completed","decision_id":"d-done","mission_id":"d-done","resume_strategy":"DIRECT","bridge_text":"","bridge_used":false}}`,
	}
	worker := New(readyVoiceStore(), core, &fakeAgent{}, &fakeTTS{})
	worker.missions.Ensure(speechmission.Mission{
		ID: "d-done", DecisionID: "d-done", TenantID: 7, RoomID: 11, RuntimeSessionID: 9, State: speechmission.StateDispatched,
	})
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}

	if err := worker.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	mission, ok := worker.MissionSnapshot("d-done")
	if !ok {
		t.Fatal("mission snapshot missing")
	}
	if mission.State != speechmission.StateCompleted {
		t.Fatalf("mission state=%s want completed", mission.State)
	}
	if len(mission.Trace) < 2 || mission.Trace[len(mission.Trace)-1].Action != "playback_completed" {
		t.Fatalf("unexpected trace=%#v", mission.Trace)
	}
}

func TestProcessRoomDoesNotLetStaleMissionBlockCoreClaim(t *testing.T) {
	core := &fakeCore{
		runtimeRaw: `{"room_id":11,"interrupt":{"status":"completed","decision_id":"old-dispatched","mission_id":"old-dispatched"}}`,
	}
	agent := &fakeAgent{}
	tts := &fakeTTS{}
	worker := New(readyVoiceStore(), core, agent, tts)
	worker.missions.Ensure(speechmission.Mission{
		ID:               "old-dispatched",
		DecisionID:       "old-dispatched",
		TenantID:         7,
		RoomID:           11,
		RuntimeSessionID: 8,
		State:            speechmission.StateDispatched,
	})
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}

	if err := worker.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if core.dispatches != 1 || agent.calls != 1 || tts.calls != 1 {
		t.Fatalf("stale mission must not block next Core claim: dispatch=%d agent=%d tts=%d", core.dispatches, agent.calls, tts.calls)
	}
	active, ok := worker.MissionSnapshot("old-dispatched")
	if !ok {
		t.Fatal("mission snapshot missing")
	}
	if active.State != speechmission.StateCompleted {
		t.Fatalf("old mission state=%s want completed", active.State)
	}
}

func TestProcessRoomReliesOnCoreSpeechBusyInsteadOfMissionLock(t *testing.T) {
	core := &fakeCore{
		runtimeRaw: `{"room_id":11,"interrupt":{"status":"returning","decision_id":"current","mission_id":"current"}}`,
		claimRaw:   `{"claimed":false,"reason":"speech_busy"}`,
	}
	worker := New(readyVoiceStore(), core, &fakeAgent{}, &fakeTTS{})
	worker.missions.Ensure(speechmission.Mission{
		ID:               "current",
		DecisionID:       "current",
		TenantID:         7,
		RoomID:           11,
		RuntimeSessionID: 9,
		State:            speechmission.StateDispatched,
	})
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}

	if err := worker.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if core.dispatches != 0 {
		t.Fatalf("dispatches=%d want 0 while Core reports speech_busy", core.dispatches)
	}
	mission, ok := worker.MissionSnapshot("current")
	if !ok {
		t.Fatal("current mission snapshot missing")
	}
	if mission.State != speechmission.StateReturningMainline {
		t.Fatalf("mission state=%s want %s", mission.State, speechmission.StateReturningMainline)
	}
}

func TestSweepMissionLifecycleReleasesExpiredDecision(t *testing.T) {
	core := &fakeCore{}
	worker := New(readyVoiceStore(), core, &fakeAgent{}, &fakeTTS{})
	now := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	worker.now = func() time.Time { return now }
	mission := worker.missions.Ensure(speechmission.Mission{
		ID: "stale", DecisionID: "stale", TenantID: 7, RoomID: 11, RuntimeSessionID: 8, State: speechmission.StateDispatched,
	})
	if _, ok := worker.missions.Update(mission.ID, func(m *speechmission.Mission) {
		m.StateChangedAt = now.Add(-missionActiveTimeout - time.Second)
	}); !ok {
		t.Fatal("mission update failed")
	}

	worker.sweepMissionLifecycle(context.Background())
	if core.releases != 1 {
		t.Fatalf("releases=%d want 1", core.releases)
	}
	expired, ok := worker.MissionSnapshot(mission.ID)
	if !ok || expired.State != speechmission.StateExpired {
		t.Fatalf("mission must be expired after sweep: %#v", expired)
	}
}

func TestReconcileRoomMissionImmediatelyReleasesDetachedCoreState(t *testing.T) {
	core := &fakeCore{runtimeRaw: `{"room_id":11,"interrupt":{"status":"idle"}}`}
	worker := New(readyVoiceStore(), core, &fakeAgent{}, &fakeTTS{})
	mission := worker.missions.Ensure(speechmission.Mission{
		ID: "detached", DecisionID: "detached", TenantID: 7, RoomID: 11, RuntimeSessionID: 8, State: speechmission.StateDispatched,
	})
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}

	worker.reconcileRoomMission(context.Background(), session)
	if core.releases != 1 {
		t.Fatalf("releases=%d want 1", core.releases)
	}
	detached, ok := worker.MissionSnapshot(mission.ID)
	if !ok || detached.State != speechmission.StateExpired {
		t.Fatalf("detached mission must be expired immediately: %#v", detached)
	}
}

func TestProcessRoomSkipsReviewAbsoluteClaimReachesTTS(t *testing.T) {
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
	// TTS 前审核已去掉（对齐新轻量互动链路）：生成结果直接送 TTS，不再调审核模型改写。
	if agent.calls != 1 {
		t.Fatalf("agent calls=%d want generation only (review removed)", agent.calls)
	}
	// 生成原文直达 TTS，不再被规则层改写。
	if !strings.Contains(tts.last.Text, "绝对不踩雷") {
		t.Fatalf("generation text should reach TTS unchanged after review removal: %q", tts.last.Text)
	}
}

func TestVerbatimModeSkipsReviewFixedTextReachesTTS(t *testing.T) {
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
	// TTS 前审核已去掉：verbatim 固定文字直接送 TTS，不再调规则层审核。
	if agent.calls != 0 {
		t.Fatalf("verbatim should skip generation and review entirely, calls=%d", agent.calls)
	}
	// 固定文字直达 TTS，不再被规则层改写。
	if !strings.Contains(tts.last.Text, "绝对不踩雷") {
		t.Fatalf("verbatim fixed text should reach TTS unchanged: %q", tts.last.Text)
	}
	if got := core.dispatch["action"]; got != "quick" {
		t.Fatalf("action=%v want quick", got)
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

func TestProcessRoomReschedulesPreparedAudioWhenSafeCutExpires(t *testing.T) {
	core := &fakeCore{
		dispatchStatus: http.StatusConflict,
		dispatchRaw:    `{"error":"安全句末切点已失效，继续排队"}`,
	}
	agent := &fakeAgent{}
	tts := &fakeTTS{}
	worker := New(readyVoiceStore(), core, agent, tts)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}

	if err := worker.processRoom(context.Background(), session); err != nil {
		t.Fatalf("safe cut waiting must not back off the entire room: %v", err)
	}
	if core.dispatches != 3 {
		t.Fatalf("safe cut reselections=%d want 3 bounded attempts", core.dispatches)
	}
	if tts.calls != 1 {
		t.Fatalf("cut reselection repeated TTS: %d", tts.calls)
	}
	if core.releases != 1 {
		t.Fatalf("failed dispatch must release claim for queue retry, releases=%d", core.releases)
	}
	if len(core.releaseReasons) != 1 || core.releaseReasons[0] != "cut_reschedule" || len(worker.prepared) != 1 {
		t.Fatal("prepared audio was lost or a generation retry was consumed")
	}
}

func TestTTSInstructionIncludesHostStateAndTTSStyleReaction(t *testing.T) {
	missions := speechmission.New()
	missions.Ensure(speechmission.Mission{
		ID: "mission-human-state",
		HumanStyle: speechmission.HumanStylePlan{
			Guidance: "自然表达",
			State:    speechmission.HumanStatePlan{HostState: "今天嗓子不舒服，声音轻一点"},
			Reaction: speechmission.HumanReactionPlan{
				Enabled:     true,
				Channel:     "TTS_STYLE",
				Instruction: "语速稍慢，声音略轻",
			},
		},
	})
	worker := &Worker{missions: missions}
	item := &decisionItem{MissionID: "mission-human-state"}
	got := worker.ttsInstructionForMission(item, "开头保持清楚")
	for _, expected := range []string{"开头保持清楚", "语速稍慢", "句子缩短", "声音略轻"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("tts instruction missing %q: %s", expected, got)
		}
	}
	for _, forbidden := range []string{"今天嗓子不舒服", "咳嗽", "感冒"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("raw host state must not leak into tts instruction: %q in %s", forbidden, got)
		}
	}
}

func TestMergedNamedWelcomeCoversGroupInsteadOfSingleViewer(t *testing.T) {
	w := New(readyVoiceStore(), nil, &fakeAgent{}, nil)
	item := decisionItem{ID: "merged", MissionKind: "welcome_named", MissionEventCount: 7, Nicknames: []string{"星夜", "一期", "秀丽江山"}}
	plan := w.buildAddressingPlan(11, item, "", "", model.RoomAddressingPreferences{}, nil)
	if plan.Mode == "SINGLE" || len(plan.SelectedNames) < 2 || len(plan.SelectedNames) > 3 {
		t.Fatal("merged welcome collapsed into one named viewer", plan)
	}
	single := item
	single.MissionEventCount = 1
	single.Nicknames = []string{"星夜"}
	plan = w.buildAddressingPlan(11, single, "", "", model.RoomAddressingPreferences{}, nil)
	if plan.Mode != "SINGLE" || len(plan.SelectedNames) != 1 {
		t.Fatal("single viewer changed unnecessarily", plan)
	}
}
