package decisionexecutor

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"

	"livecompanion/management/internal/addressing"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/contentstrategy"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/domaincard"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/interactionpurpose"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/semantic"
	"livecompanion/management/internal/speechmission"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/voicecatalog"
)

const (
	defaultInterval          = 800 * time.Millisecond
	errorBackoff             = 15 * time.Second
	maxWorkers               = 4
	roomExecutionTimeout     = 90 * time.Second
	missionActiveTimeout     = 3 * time.Minute
	missionTerminalRetention = 5 * time.Minute
	inFlightWatchdog         = roomExecutionTimeout + 5*time.Second
	maxSpeechRunes           = 300
	addressingNameCooldown   = 5 * time.Minute
)

type store interface {
	ListRunningLiveRuntimeSessions(context.Context) ([]model.LiveRuntimeSession, error)
	ListLiveAgentConfigVersions(context.Context, int64) ([]model.AgentConfigVersion, error)
	GetVoiceProfile(context.Context, int64, int64) (model.VoiceProfile, error)
	GetPublishedLiveAgentPlanVersionForRoom(context.Context, int64, int64) (model.LiveAgentPlanVersion, error)
	GetLiveAgentPlanForRoom(context.Context, int64, int64) (model.LiveAgentPlan, error)
	ListLiveAgentPlanFacts(context.Context, int64, int64) ([]model.LiveAgentPlanFact, error)
	ListLiveAgentPlanScripts(context.Context, int64, int64) ([]model.LiveAgentPlanScript, error)
	ListActiveAgentMemories(context.Context, int64, int64) ([]model.AgentMemoryItem, error)
	GetRoomHumanBehaviorProfile(context.Context, int64, int64) (model.RoomHumanBehaviorProfile, error)
	GetRoomAddressingPreferences(context.Context, int64, int64) (model.RoomAddressingPreferences, error)
	RecordGeneratedSpeechHistory(context.Context, model.GeneratedSpeechHistoryInput) error
	AgentPromptValue(context.Context, string, string) string
	RenderAgentPrompt(context.Context, string, string, map[string]string) string
}

// liveAnchorStyleSource is optional so older stores and tests keep the
// existing script-based fallback. A saved reusable主播风格, when bound to a
// plan, takes precedence over plan-local samples.
type liveAnchorStyleSource interface {
	GetLiveAnchorStyleForPlan(context.Context, int64, int64) (model.LiveAnchorStyle, error)
}

type coreDoer interface {
	DoRoom(context.Context, int64, int64, string, string, url.Values, any) (*http.Response, error)
}

type completer interface {
	Complete(context.Context, agentgateway.Request) (agentgateway.Response, error)
}

type synthesizer interface {
	SynthesizeURL(context.Context, ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error)
}

type leader interface {
	IsLeader() bool
}

type Worker struct {
	bridgeSlots        chan struct{}
	bridgeSamples      []time.Duration
	store              store
	core               coreDoer
	agent              completer
	tts                synthesizer
	paidContextFactory PaidRequestContextFactory
	paidModelCompleter PaidModelCompleter
	leader             leader
	interval           time.Duration
	executionRealm     string
	now                func() time.Time
	missions           *speechmission.Registry
	semanticEmbedder   semantic.Embedder
	semanticService    *semantic.Service
	slots              chan struct{}
	likeFollowStrategy InteractionStrategy
	welcomeStrategy    InteractionStrategy
	orderStrategy      InteractionStrategy

	mu         sync.Mutex
	inFlight   map[int64]time.Time
	retryAfter map[int64]time.Time

	addressMu            sync.Mutex
	recentNamed          map[int64]map[string]time.Time
	recentGreeting       map[int64][]string
	recentBridge         map[int64][]string
	recentBoundary       map[int64][]string
	recentWelcomeProfile map[int64]string
	nicknameAliases      map[string]nicknameAliasCacheEntry
	nicknameRulesOnce    sync.Once
	nicknameRulesValue   *addressing.Rules
	nicknameRandSeed     int64
	recentPhrases        map[int64][]string
	readingRules         map[string]readingRulesCacheEntry
	benefitRedis         *redis.Client
	prepared             map[preparedInteractionKey]preparedInteraction
}

type decisionItem struct {
	BusinessCoreOnly    bool                       `json:"-"`
	RecentAnswers       []InteractionRecentAnswer  `json:"-"`
	PreparationCutPlans json.RawMessage            `json:"-"`
	CompletionUntil     time.Time                  `json:"completion_until,omitempty"`
	AudioPrepared       bool                       `json:"audio_prepared,omitempty"`
	PreparedUntil       time.Time                  `json:"prepared_until,omitempty"`
	PurposeID           string                     `json:"purpose_id,omitempty"`
	PurposeKnowledge    json.RawMessage            `json:"purpose_knowledge,omitempty"`
	IntentAngles        []contentstrategy.Row      `json:"-"`
	SelectedContentPlan *contentstrategy.Selection `json:"-"`
	FullFacts           string                     `json:"-"`
	DomainCard          *domaincard.Card           `json:"-"`
	ScopedFacts         string                     `json:"-"`
	AnswerScope         string                     `json:"-"`
	ExpiresAt           time.Time                  `json:"expires_at,omitempty"`
	Scene               string                     `json:"scene,omitempty"`
	OrderSource         string                     `json:"order_source,omitempty"`
	OriginPlanID        int64                      `json:"origin_plan_id,omitempty"`
	OriginMainline      string                     `json:"origin_mainline,omitempty"`
	OriginTrackID       string                     `json:"origin_track_id,omitempty"`
	Schedule            json.RawMessage            `json:"schedule,omitempty"`
	ContentGoal         string                     `json:"-"`
	ContentAngle        string                     `json:"-"`
	contentError        error

	ID                     string                                `json:"id"`
	MissionID              string                                `json:"mission_id,omitempty"`
	Topic                  string                                `json:"topic"`
	Title                  string                                `json:"title"`
	Summary                string                                `json:"summary"`
	ReplyHint              string                                `json:"reply_hint"`
	SampleQuestions        []string                              `json:"sample_questions"`
	Nicknames              []string                              `json:"nicknames,omitempty"`
	PromptNicknames        []string                              `json:"-"`
	PromptShortNames       []string                              `json:"-"`
	PromptAddressPhrase    string                                `json:"-"`
	ManualAction           string                                `json:"manual_action"`
	ManualOrigin           string                                `json:"manual_origin"`
	ExecutionMode          string                                `json:"execution_mode"`
	FixedText              string                                `json:"fixed_text"`
	PreviewInstruction     string                                `json:"preview_instruction,omitempty"`
	PreviewMemoryType      string                                `json:"preview_memory_type,omitempty"`
	PreviewMemoryKey       string                                `json:"preview_memory_key,omitempty"`
	PreviewMatchedMemoryID int64                                 `json:"preview_matched_memory_id,omitempty"`
	PlannedSwitchAtMS      int                                   `json:"-"`
	CurrentMainline        string                                `json:"-"`
	ResumeMainline         string                                `json:"-"`
	ResumeSegmentID        string                                `json:"-"`
	SelectedInterrupt      string                                `json:"-"`
	SelectedResume         string                                `json:"-"`
	SelectedOpening        string                                `json:"-"`
	SelectedAddressing     string                                `json:"-"`
	HumanizationStrategy   string                                `json:"-"`
	HumanizationKind       string                                `json:"-"`
	HumanizationDelivery   string                                `json:"-"`
	HumanizationApplied    bool                                  `json:"-"`
	resolvedAddress        string                                `json:"-"`
	addressingGuidance     string                                `json:"-"`
	resolvedAddressingPlan speechmission.AddressingPlan          `json:"-"`
	EmotionDisabled        bool                                  `json:"-"`
	BridgeText             string                                `json:"-"`
	MissionKind            string                                `json:"mission_kind,omitempty"`
	MissionEventCount      int                                   `json:"mission_event_count,omitempty"`
	MissionWindowSeconds   int                                   `json:"mission_window_seconds,omitempty"`
	InteractionDecision    speechmission.InteractionDecisionPlan `json:"interaction_decision,omitempty"`
	AppliedStrategyStages  []string                              `json:"-"`
	StrategyConstraints    []strategyConstraint                  `json:"-"`
	HiddenStrategyGuidance []string                              `json:"-"`
}

type strategyConstraint struct {
	Stage    string
	Key      string
	Name     string
	Guidance string
	Required bool
}

func (item *decisionItem) addStrategyConstraint(constraint strategyConstraint) {
	if item == nil {
		return
	}
	constraint.Stage = strings.ToLower(strings.TrimSpace(constraint.Stage))
	constraint.Key = strings.TrimSpace(constraint.Key)
	constraint.Name = strings.TrimSpace(constraint.Name)
	constraint.Guidance = strings.TrimSpace(constraint.Guidance)
	if constraint.Stage == "" || constraint.Guidance == "" {
		return
	}
	item.StrategyConstraints = append(item.StrategyConstraints, constraint)
	item.addHiddenStrategyGuidance(constraint.Guidance)
}

func (item *decisionItem) addHiddenStrategyGuidance(value string) {
	if item == nil {
		return
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	item.HiddenStrategyGuidance = append(item.HiddenStrategyGuidance, value)
}

type strategyStageFunc func(context.Context, model.LiveRuntimeSession, string, *decisionItem)

func (w *Worker) strategyStageRegistry() map[string]strategyStageFunc {
	return map[string]strategyStageFunc{
		"interaction": w.applyInteractionStrategyStage,
		"interrupt":   w.applyInterruptStrategyStage,
		"resume":      w.applyResumeStrategyStage,
		"opening":     w.applyOpeningStrategyStage,
		"humanize":    w.applyHumanizeStrategyStage,
	}
}

func normalizeStrategyStageOrder(raw string, registry map[string]strategyStageFunc) []string {
	defaultOrder := []string{"interaction", "interrupt", "resume", "opening", "humanize"}
	seen := make(map[string]struct{}, len(registry))
	result := make([]string, 0, len(registry))
	raw = strings.NewReplacer("，", ",", "；", ",", ";", ",", "|", ",").Replace(raw)
	for _, part := range strings.Split(raw, ",") {
		key := strings.ToLower(strings.TrimSpace(part))
		if key == "" {
			continue
		}
		if _, ok := registry[key]; !ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	for _, key := range defaultOrder {
		if _, ok := registry[key]; !ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	extra := make([]string, 0, len(registry))
	for key := range registry {
		if _, ok := seen[key]; ok {
			continue
		}
		extra = append(extra, key)
	}
	sort.Strings(extra)
	result = append(result, extra...)
	return result
}

func (w *Worker) applyStrategyPipeline(
	ctx context.Context,
	session model.LiveRuntimeSession,
	seed string,
	item *decisionItem,
) {
	if w == nil || item == nil {
		return
	}
	if item.BusinessCoreOnly {
		// The simulation entry point executes the same business stage without
		// creating a live mission or applying any delivery stages.
		w.applyInteractionStrategyStage(ctx, session, seed, item)
		item.AppliedStrategyStages = []string{"interaction"}
		return
	}
	w.planNicknameAddresses(ctx, session, item)
	registry := w.strategyStageRegistry()
	rawOrder := w.store.AgentPromptValue(ctx, "live.strategy.pipeline.order", "interaction,interrupt,resume,opening,addressing,humanize")
	order := normalizeStrategyStageOrder(rawOrder, registry)
	item.AppliedStrategyStages = item.AppliedStrategyStages[:0]
	for _, key := range order {
		if key == "humanize" && item.EmotionDisabled {
			continue
		}
		stage := registry[key]
		if stage == nil {
			continue
		}
		w.transitionMission(item, missionStateForStrategyStage(key), "strategy_stage", key)
		stage(ctx, session, seed, item)
		item.AppliedStrategyStages = append(item.AppliedStrategyStages, key)
	}
	w.syncMissionConstraints(item)
	log.Printf(
		"decision strategy pipeline tenant=%d room=%d decision=%s order=%s",
		session.TenantID,
		session.RoomID,
		item.ID,
		strings.Join(item.AppliedStrategyStages, ">"),
	)
}

func (w *Worker) applyInteractionStrategyStage(ctx context.Context, session model.LiveRuntimeSession, _ string, item *decisionItem) {
	if item == nil || strings.TrimSpace(item.MissionKind) == "" {
		return
	}
	guidance, goal, angle, err := w.interactionContentGuidance(ctx, session, *item)
	if err != nil {
		item.contentError = err
		return
	}
	item.ContentGoal = goal.Key
	item.ContentAngle = angle
	item.addStrategyConstraint(strategyConstraint{Stage: "interaction", Key: item.MissionKind, Name: goal.Name, Guidance: guidance, Required: true})
	w.updateMission(item, func(m *speechmission.Mission) {
		m.Interaction = speechmission.InteractionPlan{Kind: item.MissionKind, Goal: goal.Name + "；" + goal.Action + "；角度：" + angle, EventCount: item.MissionEventCount, WindowSeconds: item.MissionWindowSeconds, Decision: item.InteractionDecision, Required: true}
	})
}

func interactionMissionGoal(kind string) string {
	cfg, err := interactionpolicy.Load()
	if err != nil {
		return ""
	}
	event := cfg.Events[interactionScene(kind)]
	if len(event.Goals) == 0 {
		return ""
	}
	return event.Goals[0].Name
}

func (w *Worker) applyInterruptStrategyStage(
	ctx context.Context,
	session model.LiveRuntimeSession,
	seed string,
	item *decisionItem,
) {
	selected, selectErr := w.selectCoreStrategy(
		ctx,
		session,
		seed,
		"interrupt",
		[]string{"read_comment_softly", "hard_cut", "ask_controller", "thinking_pause", "repeat_confirm"},
	)
	if selectErr != nil {
		log.Printf("decision strategy tenant=%d room=%d decision=%s category=interrupt fallback=%v", session.TenantID, session.RoomID, item.ID, selectErr)
		return
	}
	if instruction := interruptStrategyInstruction(selected.Key); instruction != "" {
		item.SelectedInterrupt = selected.Key
		item.addStrategyConstraint(strategyConstraint{
			Stage:    "interrupt",
			Key:      selected.Key,
			Name:     selected.Name,
			Guidance: "切入方式：" + instruction,
			Required: true,
		})
		w.updateMission(item, func(m *speechmission.Mission) {
			m.Interrupt = speechmission.InterruptPlan{Strategy: selected.Key, Name: selected.Name, Guidance: instruction, Required: true}
		})
		log.Printf("decision strategy tenant=%d room=%d decision=%s category=interrupt selected=%s", session.TenantID, session.RoomID, item.ID, selected.Key)
	}
}

func (w *Worker) applyOpeningStrategyStage(
	_ context.Context,
	_ model.LiveRuntimeSession,
	_ string,
	item *decisionItem,
) {
	if item == nil {
		return
	}
	intent, name, guidance := openingIntentForMission(*item)
	if intent == "" || guidance == "" {
		return
	}
	item.SelectedOpening = intent
	item.addStrategyConstraint(strategyConstraint{
		Stage:    "opening",
		Key:      intent,
		Name:     name,
		Guidance: "开头意图：" + guidance + "；这是表达意图，不是固定模板，不得机械复述本说明。",
		Required: true,
	})
	w.updateMission(item, func(m *speechmission.Mission) {
		m.Opening = speechmission.OpeningPlan{
			Intent:   intent,
			Name:     name,
			Guidance: guidance,
			Required: true,
		}
	})
}

func openingIntentForMission(item decisionItem) (string, string, string) {
	kind := strings.ToLower(strings.TrimSpace(item.MissionKind))
	switch kind {
	case "welcome_named", "welcome_batch":
		return "audience_welcome", "自然欢迎", "自然接住新进房事件后进入欢迎内容，可以直接进入，不要套固定欢迎开场"
	case "reply_follow":
		return "acknowledge_support", "回应支持", "先自然接住刚发生的关注，再把感谢融入一句完整口播，不要播报系统事件"
	case "reply_like":
		return "acknowledge_support", "回应支持", "先自然接住大家刚才的点赞支持，再自然回到本轮内容，不要机械报点赞数量"
	case "reply_chat":
		question := strings.TrimSpace(primaryQuestion(item))
		if len(item.SampleQuestions) >= 2 || len([]rune(question)) >= 36 {
			return "light_restate", "轻微复述", "问题较复杂时允许用很短的一句复述确认重点，然后直接回答；只复述核心意思，不逐字重复观众原话"
		}
		return "direct_answer", "直接回答", "直接接住当前问题进入回答；除非上下文确实需要，不要额外加固定寒暄或重复问题"
	default:
		if strings.TrimSpace(primaryQuestion(item)) != "" {
			return "natural_transition", "自然进入", "根据当前上下文自然进入本轮回应，需要时可直接回答，也可以无显式开头进入内容"
		}
		return "", "", ""
	}
}

func addressingModeForMission(item decisionItem, candidate string) string {
	if strings.TrimSpace(candidate) == "" {
		return "NONE"
	}
	switch strings.ToLower(strings.TrimSpace(item.MissionKind)) {
	case "welcome_batch", "reply_follow", "reply_like":
		return "GROUP"
	case "welcome_named":
		return "SINGLE"
	case "reply_chat":
		if item.MissionEventCount > 1 || len(item.SampleQuestions) > 1 {
			return "GROUP"
		}
		return "SINGLE"
	default:
		return "SINGLE"
	}
}

func addressingTargetRate(preference string) int {
	switch strings.ToLower(strings.TrimSpace(preference)) {
	case "less":
		return 15
	case "more":
		return 70
	default:
		return 35
	}
}

func stableAddressingBucket(seed string) int {
	sum := sha256.Sum256([]byte(seed))
	return int(binary.BigEndian.Uint32(sum[:4]) % 100)
}

func choosePreferredAddressingTerm(preferredTerms, blockedTerms []string, seed string) string {
	candidates := make([]string, 0, len(preferredTerms))
	for _, term := range preferredTerms {
		term = strings.TrimSpace(term)
		if term == "" || addressingTermBlocked(term, blockedTerms) {
			continue
		}
		candidates = append(candidates, term)
	}
	if len(candidates) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(seed))
	return candidates[int(binary.BigEndian.Uint32(sum[:4])%uint32(len(candidates)))]
}

// anchorStyleAddressTerms 从主播风格评分基准解析主力/辅助/候选称呼。
// 每个直播间的主播风格不同，主力称呼也不同，不能写死。
func anchorStyleAddressTerms(profile model.LiveAgentPlanAnchorStyleProfile) []string {
	if profile.Delivery == nil {
		return nil
	}
	if strings.TrimSpace(profile.Delivery.ScoringBaselineJSON) == "" {
		terms := []string{}
		if override := profile.Delivery.AddressingOverride; override != nil {
			terms = append(terms, override.AudienceTerms...)
		}
		for _, habit := range profile.Delivery.Habits {
			if habit.Kind == "audience_address" {
				terms = append(terms, habit.Text)
			}
		}
		return shuffledAddressTerms(terms)
	}
	baseline, err := anchorstylescore.Parse(profile.Delivery.ScoringBaselineJSON)
	if err != nil {
		return nil
	}
	terms := []string{}
	if m := strings.TrimSpace(baseline.Identity.Audience.Main); m != "" {
		terms = append(terms, m)
	}
	terms = append(terms, baseline.Identity.Audience.Other...)
	terms = append(terms, baseline.Identity.Audience.Candidates...)
	return shuffledAddressTerms(terms)
}

// shuffledAddressTerms keeps every candidate but removes list-order bias.
// The model/program must not always pick the first term just because it is first.
func shuffledAddressTerms(terms []string) []string {
	terms = cleanAddressingTerms(terms)
	rand.Shuffle(len(terms), func(i, j int) { terms[i], terms[j] = terms[j], terms[i] })
	return terms
}

// fetchAnchorAddressTerms 获取当前直播间主播风格的主力称呼（动态，每个直播间不同，不写死）。
func (w *Worker) fetchAnchorAddressTerms(ctx context.Context, session model.LiveRuntimeSession) []string {
	if w == nil || w.store == nil {
		return nil
	}
	source, ok := w.store.(liveAnchorStyleSource)
	if !ok {
		return nil
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil || plan.ID <= 0 {
		return nil
	}
	style, err := source.GetLiveAnchorStyleForPlan(ctx, session.TenantID, plan.ID)
	if err != nil {
		return nil
	}
	return anchorStyleAddressTerms(style.Profile)
}

func (w *Worker) recentGreetingPhrases(roomID int64) []string {
	if w == nil {
		return nil
	}
	w.addressMu.Lock()
	defer w.addressMu.Unlock()
	return w.recentGreeting[roomID]
}

func (w *Worker) recordGreetingPhrase(roomID int64, phrase string) {
	if w == nil || roomID <= 0 || phrase == "" {
		return
	}
	w.addressMu.Lock()
	defer w.addressMu.Unlock()
	if w.recentGreeting == nil {
		w.recentGreeting = map[int64][]string{}
	}
	recent := w.recentGreeting[roomID]
	recent = append(recent, phrase)
	// 保留最近 2 个用过的方向，保证相邻 3 轮不重复。
	if len(recent) > 2 {
		recent = recent[len(recent)-2:]
	}
	w.recentGreeting[roomID] = recent
}

// recentBridgePhrases 返回该房间近期用过的回归桥话术，用于生成时避重。
func (w *Worker) recentBridgePhrases(roomID int64) []string {
	if w == nil {
		return nil
	}
	w.addressMu.Lock()
	defer w.addressMu.Unlock()
	return w.recentBridge[roomID]
}

// recordBridgePhrase 记录本次回归桥话术，保留最近 3 个，保证相邻回归的纯承接措辞不重复。
func (w *Worker) recordBridgePhrase(roomID int64, phrase string) {
	if w == nil || roomID <= 0 || phrase == "" {
		return
	}
	w.addressMu.Lock()
	defer w.addressMu.Unlock()
	if w.recentBridge == nil {
		w.recentBridge = map[int64][]string{}
	}
	recent := w.recentBridge[roomID]
	recent = append(recent, phrase)
	if len(recent) > 3 {
		recent = recent[len(recent)-3:]
	}
	w.recentBridge[roomID] = recent
}

func cleanAddressingTerms(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || containsSemanticAddressTerm(value) {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func addressingTermBlocked(value string, blocked []string) bool {
	rawValue := strings.TrimSpace(value)
	if rawValue == "" {
		return false
	}
	if containsSemanticAddressTerm(rawValue) {
		return true
	}
	value = strings.ToLower(rawValue)
	for _, raw := range blocked {
		term := strings.ToLower(strings.TrimSpace(raw))
		if term != "" && (value == term || strings.Contains(value, term)) {
			return true
		}
	}
	return false
}

func isAggregateAddressingMission(item decisionItem) bool {
	kind := strings.ToLower(strings.TrimSpace(item.MissionKind))
	return item.MissionEventCount > 1 || len(item.SampleQuestions) > 1 ||
		kind == "welcome_batch" || kind == "reply_follow"
}

func addressingModeForPlan(item decisionItem, candidate string, selectedNames []string) string {
	kind := strings.ToLower(strings.TrimSpace(item.MissionKind))
	hasGroupLabel := strings.TrimSpace(candidate) != ""
	nameCount := len(selectedNames)
	// 多人判定：多个事件、多个原始昵称、或多个问题。
	multi := item.MissionEventCount > 1 || len(item.Nicknames) > 1 || len(item.SampleQuestions) > 1

	// 点赞：群体感谢，优先群称；合规昵称够 2 个才抽点。
	if kind == "reply_like" {
		if hasGroupLabel {
			return "GROUP"
		}
		if nameCount >= 2 {
			return "SAMPLE"
		}
		return "NONE"
	}
	// 暖场：面向群体，用大称呼，不逐个点名。
	if kind == "warmup" {
		return "GROUP"
	}
	// 多人：合规昵称够 2 个就点 2~3 人；凑不足 2 人统一群称（不裸奔）。
	if multi {
		if nameCount >= 2 {
			if hasGroupLabel {
				return "MIXED"
			}
			return "SAMPLE"
		}
		if hasGroupLabel {
			return "GROUP"
		}
		return "NONE"
	}
	// 单人：点这 1 个（有昵称）或群称（无昵称）。
	if nameCount == 1 || hasGroupLabel {
		return "SINGLE"
	}
	return "NONE"
}

func normalizePlayableNickname(raw string) (string, bool) {
	value := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	runes := []rune(value)
	if len(runes) == 0 || len(runes) > 12 {
		return "", false
	}
	lower := strings.ToLower(value)
	for _, blocked := range []string{"http", "www", ".com", "微信", "vx", "v信", "加我", "加群", "私聊", "客服", "代理", "二维码"} {
		if strings.Contains(lower, blocked) {
			return "", false
		}
	}
	meaningful := 0
	symbols := 0
	for _, r := range runes {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r):
			meaningful++
		case unicode.IsSpace(r):
		default:
			symbols++
		}
	}
	if meaningful == 0 || symbols > 2 || symbols*3 > len(runes) {
		return "", false
	}
	return value, true
}

func (w *Worker) applyResumeStrategyStage(
	ctx context.Context,
	session model.LiveRuntimeSession,
	seed string,
	item *decisionItem,
) {
	resume, selectErr := w.selectCoreResumeStrategy(ctx, session, seed+":resume", item.Topic, estimatedAnswerDurationMS(*item))
	if selectErr != nil {
		log.Printf("decision strategy tenant=%d room=%d decision=%s category=resume fallback=%v", session.TenantID, session.RoomID, item.ID, selectErr)
		return
	}
	if instruction := resumeStrategyInstruction(resume.Key); instruction != "" {
		item.SelectedResume = resume.Key
		coreTemplate, err := interactionpolicy.Read("interaction-return-core-answer.md")
		if err != nil {
			item.contentError = err
			return
		}
		guidance := string(coreTemplate)
		item.addStrategyConstraint(strategyConstraint{
			Stage:    "resume",
			Key:      resume.Key,
			Name:     resume.Name,
			Guidance: guidance,
			Required: true,
		})
		w.updateMission(item, func(m *speechmission.Mission) {
			segmentID := strings.TrimSpace(resume.ResumeSegmentID)
			if segmentID == "" {
				segmentID = item.ResumeSegmentID
			}
			m.Resume = speechmission.ResumePlan{
				Strategy:              resume.Key,
				Name:                  resume.Name,
				Guidance:              guidance,
				ResumeMainline:        item.ResumeMainline,
				ResumeSegmentID:       segmentID,
				CutAfterSegment:       strings.TrimSpace(resume.CutAfterSegmentID),
				OriginalResumeSegment: strings.TrimSpace(resume.OriginalResumeSegmentID),
				CoveredSegments:       append([]string(nil), resume.CoveredSegmentIDs...),
				PlannedResumeSegment:  strings.TrimSpace(resume.PlannedResumeSegmentID),
				SkipCount:             resume.SkipCount,
				PlannedResumeAtMS:     resume.ResumeOffsetMS,
				ResumeReason:          resume.ResumeReason,
				ResumePreview:         resume.ResumePreview,
				SkippedPreviews:       append([]string(nil), resume.SkippedPreviews...),
				Required:              true,
			}
		})
		log.Printf("decision strategy tenant=%d room=%d decision=%s category=resume selected=%s", session.TenantID, session.RoomID, item.ID, resume.Key)
	}
}

func (w *Worker) applyHumanizeStrategyStage(
	ctx context.Context,
	session model.LiveRuntimeSession,
	_ string,
	item *decisionItem,
) {
	if item == nil {
		return
	}
	baseGuidance := "把整段话说成真人主播现场自然接话；允许短句和自然停顿，但不要固定口癖，不要为了仿真改变任何事实。"
	profile, profileErr := w.store.GetRoomHumanBehaviorProfile(ctx, session.TenantID, session.RoomID)
	if profileErr == nil {
		if state := strings.TrimSpace(profile.StateText); state != "" && (profile.StateExpiresAt == nil || w.now().UTC().Before(profile.StateExpiresAt.UTC())) {
			baseGuidance += " 主播当前状态属于后台控制参数，不是直播内容。" + hostStateDeliveryGuidance(state) + " 严禁在正文中主动解释、复述或透露主播身体、情绪状态及原因。"
		}
	}
	plan, err := w.loadCoreHumanizationPlan(ctx, session)
	if err != nil {
		item.addStrategyConstraint(strategyConstraint{
			Stage: "humanize", Key: "natural_live_speech", Name: "自然表达",
			Guidance: baseGuidance, Required: true,
		})
		w.updateMission(item, func(m *speechmission.Mission) {
			m.HumanStyle = buildHumanStyleMissionPlan(*item, selectedHumanizationPlan{
				Strategy: "fallback.natural",
				Reason:   "core_humanization_unavailable",
			}, profile, baseGuidance, false)
		})
		return
	}

	guidance := baseGuidance
	applied := plan.Enabled && strings.TrimSpace(plan.Kind) != "" && !strings.EqualFold(strings.TrimSpace(plan.Kind), "NONE")
	if applied {
		if instruction := strings.TrimSpace(plan.Instruction); instruction != "" {
			guidance += " 本轮可采用一次以下真人化行为：" + instruction + "；只出现一次，不要额外再加第二种真人化动作。"
		}
	} else {
		guidance += " 本轮不要主动加入重复、自我纠正、清嗓、咳嗽或刻意口头禅，保持干净自然。"
	}
	item.HumanizationStrategy = strings.TrimSpace(plan.Strategy)
	item.HumanizationKind = strings.TrimSpace(plan.Kind)
	item.HumanizationDelivery = strings.TrimSpace(plan.Delivery)
	item.HumanizationApplied = applied
	key := item.HumanizationStrategy
	if key == "" {
		key = "humanization.none"
	}
	item.addStrategyConstraint(strategyConstraint{
		Stage: "humanize", Key: key, Name: humanizationKindName(item.HumanizationKind), Guidance: guidance, Required: true,
	})
	w.updateMission(item, func(m *speechmission.Mission) {
		m.HumanStyle = buildHumanStyleMissionPlan(*item, plan, profile, guidance, applied)
	})
}

type coreHumanizationPlan struct {
	Intelligence struct {
		Heat string `json:"Heat"`
	} `json:"Intelligence"`
	Director struct {
		Progress     string `json:"Progress"`
		Atmosphere   string `json:"Atmosphere"`
		Humanization struct {
			Strategy        string    `json:"Strategy"`
			Enabled         bool      `json:"Enabled"`
			Kind            string    `json:"Kind"`
			Delivery        string    `json:"Delivery"`
			Instruction     string    `json:"Instruction"`
			AssetKey        string    `json:"AssetKey"`
			MaxCount        int       `json:"MaxCount"`
			Reason          string    `json:"Reason"`
			Source          string    `json:"Source"`
			RuleID          string    `json:"RuleID"`
			Intensity       float64   `json:"Intensity"`
			Channel         string    `json:"Channel"`
			CooldownSeconds int64     `json:"CooldownSeconds"`
			ExpiresAt       time.Time `json:"ExpiresAt"`
		} `json:"Humanization"`
	} `json:"Director"`
}

type selectedHumanizationPlan struct {
	Strategy        string
	Enabled         bool
	Kind            string
	Delivery        string
	Instruction     string
	AssetKey        string
	MaxCount        int
	Reason          string
	Source          string
	RuleID          string
	Intensity       float64
	Channel         string
	CooldownSeconds int64
	ExpiresAt       time.Time
	Heat            string
	Progress        string
	Atmosphere      string
}

func (w *Worker) loadCoreHumanizationPlan(ctx context.Context, session model.LiveRuntimeSession) (selectedHumanizationPlan, error) {
	if w == nil || w.core == nil || session.TenantID <= 0 || session.RoomID <= 0 {
		return selectedHumanizationPlan{}, fmt.Errorf("core humanization unavailable")
	}
	resp, err := w.core.DoRoom(
		ctx, session.TenantID, session.RoomID, http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d/brain", session.RoomID),
		tenantQuery(session.TenantID), nil,
	)
	if err != nil {
		return selectedHumanizationPlan{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return selectedHumanizationPlan{}, fmt.Errorf("humanization brain http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var view coreHumanizationPlan
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		return selectedHumanizationPlan{}, err
	}
	value := view.Director.Humanization
	return selectedHumanizationPlan{
		Strategy: value.Strategy, Enabled: value.Enabled, Kind: value.Kind, Delivery: value.Delivery,
		Instruction: value.Instruction, AssetKey: value.AssetKey, MaxCount: value.MaxCount, Reason: value.Reason,
		Source: value.Source, RuleID: value.RuleID, Intensity: value.Intensity, Channel: value.Channel,
		CooldownSeconds: value.CooldownSeconds, ExpiresAt: value.ExpiresAt,
		Heat: view.Intelligence.Heat, Progress: view.Director.Progress, Atmosphere: view.Director.Atmosphere,
	}, nil
}

func buildHumanStyleMissionPlan(item decisionItem, plan selectedHumanizationPlan, profile model.RoomHumanBehaviorProfile, guidance string, applied bool) speechmission.HumanStylePlan {
	maxCount := plan.MaxCount
	if maxCount <= 0 && applied {
		maxCount = 1
	}
	if !applied {
		maxCount = 0
	}
	strategy := strings.TrimSpace(plan.Strategy)
	if strategy == "" {
		strategy = "humanization.none"
	}
	kind := strings.TrimSpace(plan.Kind)
	if kind == "" {
		kind = "NONE"
	}
	reason := strings.TrimSpace(plan.Reason)
	return speechmission.HumanStylePlan{
		Mode:     "natural_live_speech",
		Strategy: strategy,
		Kind:     kind,
		Delivery: strings.TrimSpace(plan.Delivery),
		Enabled:  applied,
		Guidance: strings.TrimSpace(guidance),
		Reason:   reason,
		Emotion:  "natural_warm",
		Pace:     "conversational",
		Trait: speechmission.HumanTraitPlan{
			Persona:          "natural_live_anchor",
			Emotion:          "natural_warm",
			Pace:             "conversational",
			Humor:            "adaptive_restrained",
			MaxReactionCount: 1,
			// Long-term persona rules are plan-owned and compiled by the anchor
			// style overlay. Room behavior retains only the expiring host state.
			Instruction: "",
		},
		State: speechmission.HumanStatePlan{
			Heat:        strings.TrimSpace(plan.Heat),
			Progress:    strings.TrimSpace(plan.Progress),
			Atmosphere:  strings.TrimSpace(plan.Atmosphere),
			MissionKind: strings.TrimSpace(item.MissionKind),
			HostState:   strings.TrimSpace(profile.StateText),
			ExpiresAt:   profile.StateExpiresAt,
		},
		Reaction: speechmission.HumanReactionPlan{
			Strategy:        strategy,
			Kind:            kind,
			Delivery:        strings.TrimSpace(plan.Delivery),
			Instruction:     strings.TrimSpace(plan.Instruction),
			AssetKey:        strings.TrimSpace(plan.AssetKey),
			MaxCount:        maxCount,
			Enabled:         applied,
			Reason:          reason,
			Source:          strings.TrimSpace(plan.Source),
			RuleID:          strings.TrimSpace(plan.RuleID),
			Intensity:       plan.Intensity,
			Channel:         strings.TrimSpace(plan.Channel),
			CooldownSeconds: plan.CooldownSeconds,
			ExpiresAt:       plan.ExpiresAt,
		},
	}
}

func humanizationKindName(kind string) string {
	switch strings.ToUpper(strings.TrimSpace(kind)) {
	case "PAUSE":
		return "自然停顿"
	case "FILLER":
		return "轻语气词"
	case "REPEAT_FRAGMENT":
		return "轻微重复"
	case "SELF_CORRECTION":
		return "轻微自我修正"
	case "INVERSION":
		return "口语倒装"
	case "REHOOK":
		return "自然再承接"
	case "THROAT_CLEAR":
		return "轻清嗓"
	case "COUGH":
		return "轻咳"
	default:
		return "自然表达"
	}
}

type claimResponse struct {
	CutPlans        json.RawMessage `json:"cut_plans,omitempty"`
	Claimed         bool            `json:"claimed"`
	Reason          string          `json:"reason"`
	Item            *decisionItem   `json:"item"`
	SwitchAtMS      int             `json:"switch_at_ms"`
	CurrentMainline string          `json:"current_mainline"`
	ResumeMainline  string          `json:"resume_mainline"`
	ResumeSegmentID string          `json:"resume_segment_id"`
}

func New(s store, core coreDoer, agent completer, tts synthesizer, leaders ...leader) *Worker {
	w := &Worker{
		store:          s,
		bridgeSlots:    make(chan struct{}, 4),
		core:           core,
		agent:          agent,
		tts:            tts,
		interval:       defaultInterval,
		executionRealm: "prod",
		now:            func() time.Time { return time.Now().UTC() },
		missions:       speechmission.New(),
		slots:          make(chan struct{}, maxWorkers),
		inFlight:       make(map[int64]time.Time),
		retryAfter:     make(map[int64]time.Time),
		recentNamed:    make(map[int64]map[string]time.Time),
		prepared:       make(map[preparedInteractionKey]preparedInteraction),
	}
	// 三个轻互动策略默认用模板骨架实现（不调模型），后续逐个替换为模型实现。
	w.likeFollowStrategy = likeFollowStrategy{}
	w.welcomeStrategy = welcomeStrategy{}
	w.orderStrategy = orderStrategy{}
	if len(leaders) > 0 {
		w.leader = leaders[0]
	}
	return w
}

func (w *Worker) SetExecutionRealm(realm string) {
	if w == nil {
		return
	}
	w.executionRealm = model.NormalizeExecutionRealm(realm)
}

func (w *Worker) SetSemanticEmbedder(embedder semantic.Embedder) {
	if w == nil {
		return
	}
	w.semanticEmbedder = embedder
}

func (w *Worker) SetSemanticService(service *semantic.Service) {
	if w == nil {
		return
	}
	w.semanticService = service
}

// SetBenefitRedis 注入 Redis client，供欢迎策略的活动/福利缓存使用。
// 福利缓存常驻 Redis（48 小时时效兜底），仅在「删除/修改方案或房间」时主动失效。
// 未注入时欢迎策略退化为每次现算（模型识别 + 程序兜底），不缓存。
func (w *Worker) SetBenefitRedis(client *redis.Client) {
	if w == nil {
		return
	}
	w.benefitRedis = client
}

func missionID(item *decisionItem) string {
	if item == nil {
		return ""
	}
	if value := strings.TrimSpace(item.MissionID); value != "" {
		return value
	}
	return strings.TrimSpace(item.ID)
}

func (w *Worker) ensureMission(session model.LiveRuntimeSession, item *decisionItem) {
	if w == nil || w.missions == nil || item == nil {
		return
	}
	id := missionID(item)
	if id == "" {
		return
	}
	item.MissionID = id
	w.missions.Ensure(speechmission.Mission{
		ID:               id,
		DecisionID:       strings.TrimSpace(item.ID),
		TenantID:         session.TenantID,
		RoomID:           session.RoomID,
		RuntimeSessionID: session.ID,
		State:            speechmission.StateCreated,
		Event: speechmission.EventContext{
			Kind:          strings.TrimSpace(item.MissionKind),
			Topic:         strings.TrimSpace(item.Topic),
			Title:         strings.TrimSpace(item.Title),
			Summary:       strings.TrimSpace(item.Summary),
			Questions:     append([]string(nil), item.SampleQuestions...),
			Nicknames:     append([]string(nil), item.Nicknames...),
			EventCount:    item.MissionEventCount,
			WindowSeconds: item.MissionWindowSeconds,
		},
		Mainline: speechmission.MainlineContext{
			Before:          strings.TrimSpace(item.CurrentMainline),
			After:           strings.TrimSpace(item.ResumeMainline),
			ResumeSegmentID: strings.TrimSpace(item.ResumeSegmentID),
			SwitchAtMS:      item.PlannedSwitchAtMS,
		},
	})
}

func (w *Worker) updateMission(item *decisionItem, mutate func(*speechmission.Mission)) {
	if w == nil || w.missions == nil || item == nil {
		return
	}
	_, _ = w.missions.Update(missionID(item), mutate)
}

func (w *Worker) transitionMission(item *decisionItem, state speechmission.State, action, note string) {
	if w == nil || w.missions == nil || item == nil {
		return
	}
	_, _ = w.missions.Transition(missionID(item), state, action, note)
}

func missionStateForStrategyStage(stage string) speechmission.State {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "interaction":
		return speechmission.StatePlanningInteraction
	case "interrupt":
		return speechmission.StatePlanningInterrupt
	case "resume":
		return speechmission.StatePlanningResume
	case "opening", "addressing", "humanize":
		return speechmission.StatePlanningExpression
	default:
		return speechmission.StatePlanningExpression
	}
}

func (w *Worker) syncMissionConstraints(item *decisionItem) {
	if item == nil {
		return
	}
	constraints := make([]speechmission.Constraint, 0, len(item.StrategyConstraints))
	for _, value := range item.StrategyConstraints {
		constraints = append(constraints, speechmission.Constraint{
			Stage: value.Stage, Key: value.Key, Name: value.Name, Guidance: value.Guidance, Required: value.Required,
		})
	}
	w.updateMission(item, func(m *speechmission.Mission) {
		m.AppliedStages = append([]string(nil), item.AppliedStrategyStages...)
		m.Constraints = constraints
	})
	w.updateMission(item, func(m *speechmission.Mission) {
		if m.PlanFrozenAt == nil {
			now := time.Now().UTC()
			m.PlanFrozenAt = &now
		}
	})
	w.transitionMission(item, speechmission.StateGeneratingText, "plan_frozen", "互动、打断、回归、开头、称呼和仿真人约束已冻结，本轮生成阶段不再改写策略计划")
}

func (w *Worker) missionSnapshot(item *decisionItem) (speechmission.Mission, bool) {
	if w == nil || w.missions == nil || item == nil {
		return speechmission.Mission{}, false
	}
	return w.missions.Snapshot(missionID(item))
}

func (w *Worker) MissionSnapshot(id string) (speechmission.Mission, bool) {
	if w == nil || w.missions == nil {
		return speechmission.Mission{}, false
	}
	return w.missions.Snapshot(id)
}

func (w *Worker) RoomMissionSnapshots(roomID int64) []speechmission.Mission {
	if w == nil || w.missions == nil {
		return []speechmission.Mission{}
	}
	return w.missions.RoomSnapshots(roomID)
}

func (w *Worker) Run(ctx context.Context) {
	if w == nil || w.store == nil || w.core == nil || w.agent == nil || w.tts == nil {
		return
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	w.runCycle(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runCycle(ctx)
		}
	}
}

func (w *Worker) runCycle(ctx context.Context) {
	if w.leader != nil && !w.leader.IsLeader() {
		return
	}
	w.sweepMissionLifecycle(ctx)
	sessions, err := w.store.ListRunningLiveRuntimeSessions(ctx)
	if err != nil {
		log.Printf("decision executor list runtime sessions: %v", err)
		return
	}
	w.prunePreparedInteractions(sessions)
	for _, session := range sessions {
		if !session.BelongsToExecutionRealm(w.executionRealm) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(session.Status), "running") {
			continue
		}
		flightStartedAt, ok := w.beginRoom(session.RoomID)
		if !ok {
			continue
		}
		select {
		case w.slots <- struct{}{}:
			// A global worker slot is reserved for this room until the goroutine
			// exits. Later scheduler ticks remain free to scan other rooms.
		default:
			w.endRoom(session.RoomID, flightStartedAt)
			continue
		}
		session := session
		go func() {
			defer func() { <-w.slots }()
			defer w.endRoom(session.RoomID, flightStartedAt)
			roomCtx, cancel := context.WithTimeout(ctx, roomExecutionTimeout)
			defer cancel()
			if err := w.processRoom(roomCtx, session); err != nil {
				w.backoffRoom(session.RoomID)
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(roomCtx.Err(), context.DeadlineExceeded) {
					log.Printf("decision executor tenant=%d room=%d timed out after %s", session.TenantID, session.RoomID, roomExecutionTimeout)
				} else {
					log.Printf("decision executor tenant=%d room=%d: %v", session.TenantID, session.RoomID, err)
				}
			}
		}()
	}
}

func (w *Worker) sweepMissionLifecycle(ctx context.Context) {
	if w == nil || w.missions == nil {
		return
	}
	expired, removed := w.missions.Sweep(w.now(), missionActiveTimeout, missionTerminalRetention)
	for _, mission := range expired {
		if strings.TrimSpace(mission.DecisionID) == "" || mission.TenantID <= 0 || mission.RoomID <= 0 {
			continue
		}
		releaseCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := w.release(releaseCtx, model.LiveRuntimeSession{
			ID: mission.RuntimeSessionID, TenantID: mission.TenantID, RoomID: mission.RoomID,
		}, mission.DecisionID)
		cancel()
		if err != nil {
			log.Printf("decision executor expire release tenant=%d room=%d decision=%s: %v", mission.TenantID, mission.RoomID, mission.DecisionID, err)
			continue
		}
		log.Printf("decision executor expired stale mission tenant=%d room=%d decision=%s mission=%s", mission.TenantID, mission.RoomID, mission.DecisionID, mission.ID)
	}
	if removed > 0 {
		log.Printf("decision executor pruned terminal missions count=%d", removed)
	}
}

type coreSpeechRuntimeSnapshot struct {
	Interrupt struct {
		Status         string     `json:"status"`
		DecisionID     string     `json:"decision_id"`
		MissionID      string     `json:"mission_id"`
		ResumeStrategy string     `json:"resume_strategy"`
		BridgeText     string     `json:"bridge_text"`
		BridgeUsed     bool       `json:"bridge_used"`
		StartedAt      *time.Time `json:"started_at"`
		UpdatedAt      *time.Time `json:"updated_at"`
	} `json:"interrupt"`
}

func (w *Worker) reconcileRoomMission(ctx context.Context, session model.LiveRuntimeSession) {
	if w == nil || w.core == nil || w.missions == nil || session.RoomID <= 0 {
		return
	}
	missions := w.missions.RoomSnapshots(session.RoomID)
	var target *speechmission.Mission
	for i := range missions {
		if missions[i].State == speechmission.StateDispatched ||
			missions[i].State == speechmission.StateWaitingCutPoint ||
			missions[i].State == speechmission.StateReturningMainline {
			copy := missions[i]
			target = &copy
			break
		}
	}
	if target == nil {
		return
	}
	resp, err := w.core.DoRoom(
		ctx, session.TenantID, session.RoomID, http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d/speech-runtime", session.RoomID),
		tenantQuery(session.TenantID), nil,
	)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return
	}
	var snapshot coreSpeechRuntimeSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return
	}
	runtimeStatus := strings.ToLower(strings.TrimSpace(snapshot.Interrupt.Status))
	runtimeMissionID := strings.TrimSpace(snapshot.Interrupt.MissionID)
	runtimeDecisionID := strings.TrimSpace(snapshot.Interrupt.DecisionID)
	matches := true
	if runtimeMissionID != "" {
		if runtimeMissionID != strings.TrimSpace(target.ID) {
			matches = false
		}
	} else if runtimeDecisionID != strings.TrimSpace(target.DecisionID) {
		matches = false
	}
	if !matches {
		// If Core is no longer running an interaction, this Management mission
		// is stale hot state (for example after a Core restart). Release it
		// immediately instead of blocking the room until the hard timeout.
		if runtimeStatus == "" || runtimeStatus == "idle" || runtimeStatus == "completed" || runtimeStatus == "failed" {
			item := &decisionItem{ID: target.DecisionID, MissionID: target.ID}
			w.transitionMission(item, speechmission.StateExpired, "core_runtime_detached", "Core已不再持有这条互动执行，立即释放残留热状态")
			releaseCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = w.release(releaseCtx, session, target.DecisionID)
			cancel()
		}
		return
	}
	item := &decisionItem{ID: target.DecisionID, MissionID: target.ID}
	if target.RuntimeSessionID == session.ID {
		w.archiveSessionPlayback(ctx, session, snapshot)
	}
	w.updateMission(item, func(m *speechmission.Mission) {
		if value := strings.TrimSpace(snapshot.Interrupt.ResumeStrategy); value != "" {
			m.Resume.Strategy = value
		}
		m.Resume.BridgeText = strings.TrimSpace(snapshot.Interrupt.BridgeText)
	})
	switch runtimeStatus {
	case "returning":
		w.transitionMission(item, speechmission.StateReturningMainline, "mainline_returning", "互动语音已结束，等待主线实际恢复")
	case "completed":
		w.transitionMission(item, speechmission.StateCompleted, "playback_completed", "Core确认互动语音已经完整播放并完成主线回归")
		if target.RuntimeSessionID == session.ID && strings.TrimSpace(target.GeneratedText) != "" {
			semanticIndexCtx, semanticIndexCancel := context.WithTimeout(ctx, 2500*time.Millisecond)
			if semanticErr := w.indexRecentSpeech(semanticIndexCtx, session, decisionItem{ID: target.DecisionID}, target.GeneratedText); semanticErr != nil {
				log.Printf("decision completed speech semantic index degraded tenant=%d room=%d decision=%s: %v", session.TenantID, session.RoomID, target.DecisionID, semanticErr)
			}
			semanticIndexCancel()
		}
	case "failed":
		w.transitionMission(item, speechmission.StateFailed, "playback_failed", "Core确认互动语音播放失败")
	}
}

func (w *Worker) processRoom(ctx context.Context, session model.LiveRuntimeSession) error {
	ctx = agentgateway.WithRoomModelScope(ctx, agentgateway.RoomModelScope{TenantID: session.TenantID, RoomID: session.RoomID, RuntimeID: session.ID, RequireLease: true})
	w.reconcileRoomMission(ctx, session)
	// Core is the single authority for room speech occupancy. The claim API
	// checks speech-runtime and returns speech_busy while an interaction is
	// ready/playing/returning. SpeechMission is observation/audit state only;
	// stale Management memory must never block the next interaction.
	claim, err := w.claim(ctx, session)
	if err != nil {
		return err
	}
	if !claim.Claimed || claim.Item == nil {
		return nil
	}
	item := claim.Item
	item.PlannedSwitchAtMS = claim.SwitchAtMS
	item.PreparationCutPlans = append(json.RawMessage(nil), claim.CutPlans...)
	item.CurrentMainline = strings.TrimSpace(claim.CurrentMainline)
	item.ResumeMainline = strings.TrimSpace(claim.ResumeMainline)
	item.ResumeSegmentID = strings.TrimSpace(claim.ResumeSegmentID)
	w.ensureMission(session, item)
	w.transitionMission(item, speechmission.StatePlanningInteraction, "claimed", "口播任务已领取，开始汇总本轮策略黑板")
	completed := false
	reschedule := false
	defer func() {
		if !completed {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			reason := ""
			if reschedule {
				reason = "cut_reschedule"
			}
			if err := w.release(releaseCtx, session, item.ID, reason); err != nil {
				log.Printf("decision executor release tenant=%d room=%d decision=%s: %v", session.TenantID, session.RoomID, item.ID, err)
			}
		}
	}()

	manualOrigin := strings.ToLower(strings.TrimSpace(item.ManualOrigin))
	isSimulation := manualOrigin == "test_simulation" || manualOrigin == "agent_input_preview"
	if isSimulation {
		ctx = context.WithValue(ctx, workerPaidOptOutKey{}, true)
	}
	memoryBefore, err := w.interactionMemoryFingerprint(ctx, session)
	if err != nil {
		return err
	}
	var preparationScope string
	if !isSimulation {
		preparationScope, err = w.interactionPreparationScope(ctx, session, *item)
		if err != nil {
			return err
		}
		if prepared, ok := w.takePreparedInteraction(session, *item, preparationScope, memoryBefore); ok {
			// Keep the generated reply and its voice directives, but let Core
			// select a safe boundary on the current mainline instead of the old one.
			current, resume, segment := item.CurrentMainline, item.ResumeMainline, item.ResumeSegmentID
			*item = prepared.Item
			item.CurrentMainline, item.ResumeMainline, item.ResumeSegmentID = current, resume, segment
			item.PlannedSwitchAtMS = 0
			w.transitionMission(item, speechmission.StateWaitingCutPoint, "prepared_audio_reused", "复用已审核正文和声音，仅重新安排当前主线切点")
			err = w.dispatchPreparedInteraction(ctx, session, item, prepared.Text, prepared.AudioURL, preparationScope, memoryBefore, prepared.ExpiresAt)
			if errors.Is(err, errPreparedInteractionWaiting) {
				reschedule = true
				return nil // release for scheduling without the room-wide error backoff
			}
			completed = err == nil
			return err
		}
	}
	if item.AudioPrepared && !item.ExpiresAt.IsZero() && !w.now().Before(item.ExpiresAt) && (item.CompletionUntil.IsZero() || !w.now().Before(item.CompletionUntil)) {
		return fmt.Errorf("已准备音频失效且原事件已到期，停止重新生成")
	}
	if !isSimulation && session.ID > 0 && (w.paidContextFactory != nil || w.paidModelCompleter != nil) {
		plan, planErr := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
		if planErr != nil {
			return planErr
		}
		ctx = withWorkerPaidScope(ctx, session, plan.ID, item.ID+":"+preparationScope)
	}
	text, err := w.generateDecisionTextMutable(ctx, session, item)
	if err != nil {
		w.transitionMission(item, speechmission.StateFailed, "generation_failed", err.Error())
		return err
	}

	if isSimulation {
		planName := ""
		if plan, planErr := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID); planErr == nil {
			planName = strings.TrimSpace(plan.Name)
		}
		var userLayerVersion uint64
		executionMode := strings.TrimSpace(item.ExecutionMode)
		if executionMode == "" {
			executionMode = "intent"
		}
		if err := w.completeSimulation(ctx, session, *item, text, executionMode, planName, userLayerVersion); err != nil {
			return err
		}
		w.transitionMission(item, speechmission.StateCompleted, "simulation_completed", "测试模式已完成最终话术生成，不进入TTS")
		completed = true
		w.clearBackoff(session.RoomID)
		return nil
	}

	// TTS 前只处理命中 P0 的句子：能换就换，换不掉就丢这一句，其余原样组装。
	// 不整段重生成，避免一句违规把整段口播和 token 全部重来。
	if !strings.EqualFold(strings.TrimSpace(item.ExecutionMode), "verbatim") {
		sanitize := sanitizeSpokenText
		if isInteractionSpeech(*item) {
			sanitize = sanitizeInteractionSpokenText
		}
		text = sanitize(text)
		if sanitized, rewritten, dropped := w.rewriteTTSP0Sentences(ctx, session, text); rewritten > 0 || dropped > 0 {
			text = sanitized
			// 只记录计数，不把正文或命中词写进日志。
			w.transitionMission(item, speechmission.StateSynthesizingTTS, "p0_sentence_rewrite",
				fmt.Sprintf("TTS前按句处理P0：改写%d句，丢弃%d句", rewritten, dropped))
		}
		text = sanitize(text)
	}
	if strings.TrimSpace(text) == "" {
		err := fmt.Errorf("TTS前正文被P0全部拦截，未生成语音")
		w.transitionMission(item, speechmission.StateFailed, "p0_all_dropped", err.Error())
		return err
	}

	voice, ok, err := w.readyVoice(ctx, session.TenantID, session.RoomID)
	if err != nil {
		w.transitionMission(item, speechmission.StateFailed, "voice_resolve_failed", err.Error())
		return err
	}
	if !ok {
		err := fmt.Errorf("没有可用的默认声音，请先在声音中心选择可用音色")
		w.transitionMission(item, speechmission.StateFailed, "voice_unavailable", err.Error())
		return err
	}

	ttsModel := voiceModel(voice)
	ttsProfile := ttsProfileForInterrupt(item.SelectedInterrupt)
	ttsInstruction := ""
	if !item.EmotionDisabled {
		ttsInstruction = w.ttsInstructionForMission(item, ttsProfile.Instruction)
		if ttsgateway.IsCosyVoiceModel(ttsModel) {
			ttsInstruction, err = w.cosyVoiceInstructionForMission(item)
			if err != nil {
				return fmt.Errorf("准备CosyVoice表达指令失败: %w", err)
			}
		}
	}
	// 称呼之后让模型自己处理停顿：单人、多人统一。
	if strings.TrimSpace(item.PromptAddressPhrase) != "" {
		if ttsInstruction == "" {
			ttsInstruction = "开头称呼之后停顿约0.2秒，再自然往下说。"
		} else {
			ttsInstruction += "；开头称呼之后停顿约0.2秒，再自然往下说。"
		}
	}
	w.transitionMission(item, speechmission.StateSynthesizingTTS, "tts_start", "最终话术审核通过，开始生成语音")
	w.updateMission(item, func(m *speechmission.Mission) {
		m.TTS = speechmission.TTSDirective{
			Provider: voiceProvider(voice), Model: ttsModel, VoiceID: voice.VoiceID,
			Rate: voiceRate(voice), Instruction: ttsInstruction,
		}
	})
	ttsCtx, ttsCancel := context.WithTimeout(ctx, 35*time.Second)
	defer ttsCancel()
	ttsText, ssmlUsed := buildTTSText(text, item.PromptAddressPhrase, w.readingRulesFor(ctx, session), ttsgateway.SupportsSSML(ttsModel))
	audio, err := w.synthesizeInteractionAudio(ttsCtx, item, ttsgateway.SynthesizeRequest{
		Provider:    voiceProvider(voice),
		Model:       ttsModel,
		VoiceID:     voice.VoiceID,
		Text:        ttsText,
		Rate:        voiceRate(voice),
		Instruction: ttsInstruction,
	})
	if err != nil && ssmlUsed {
		audio, err = w.synthesizeInteractionAudio(ttsCtx, item, ttsgateway.SynthesizeRequest{
			Provider:    voiceProvider(voice),
			Model:       ttsModel,
			VoiceID:     voice.VoiceID,
			Text:        text,
			Rate:        voiceRate(voice),
			Instruction: ttsInstruction,
		})
	}
	if err != nil {
		err = fmt.Errorf("TTS生成失败: %w", err)
		w.transitionMission(item, speechmission.StateFailed, "tts_failed", err.Error())
		return err
	}
	if strings.TrimSpace(audio.AudioURL) == "" {
		err = fmt.Errorf("TTS未返回音频地址")
		w.transitionMission(item, speechmission.StateFailed, "tts_empty_audio", err.Error())
		return err
	}
	w.updateMission(item, func(m *speechmission.Mission) {
		m.TTS.AudioURL = strings.TrimSpace(audio.AudioURL)
	})
	log.Printf(
		"decision tts profile tenant=%d room=%d decision=%s interrupt=%s rate=%.2f instruction=%t model=%s",
		session.TenantID, session.RoomID, item.ID, item.SelectedInterrupt, voiceRate(voice), strings.TrimSpace(ttsInstruction) != "", ttsModel,
	)

	// Queue expiry governs admission. A claimed answer remains owned by its
	// execution lease until playback completes, fails or is explicitly removed.
	memoryNow, err := w.interactionMemoryFingerprint(ctx, session)
	if err != nil {
		return err
	}
	if memoryNow != memoryBefore {
		// Corrections remain hard constraints, unlike advisory host style.
		// 规则更新后只用最新文本重新合成语音，不再做长度兜底修复。
		text = item.nicknameSpeechText(text)
		audio, err = w.synthesizePaidAudio(ctx, session, "interaction_fact_refresh_tts", item.ID, ttsgateway.SynthesizeRequest{Provider: voiceProvider(voice), Model: ttsModel, VoiceID: voice.VoiceID, Text: prefixedInteractionText(text), Rate: voiceRate(voice), Instruction: ttsInstruction})
		if err != nil || strings.TrimSpace(audio.AudioURL) == "" {
			return fmt.Errorf("纠正规则更新后重新生成语音失败: %v", err)
		}
		latest, e := w.interactionMemoryFingerprint(ctx, session)
		if e != nil {
			return e
		}
		if latest != memoryNow {
			return fmt.Errorf("纠正规则再次更新，撤销旧回答并重新排队")
		}
	}
	latestScope, err := w.interactionPreparationScope(ctx, session, *item)
	if err != nil {
		return err
	}
	if latestScope != preparationScope {
		return fmt.Errorf("商品事实、方案或播出规则已变化，撤销旧声音并重新排队")
	}
	err = w.dispatchPreparedInteraction(ctx, session, item, text, audio.AudioURL, preparationScope, memoryNow, w.now().Add(preparedInteractionTTL))
	if errors.Is(err, errPreparedInteractionWaiting) {
		reschedule = true
		return nil
	}
	completed = err == nil
	return err
}

func (w *Worker) dispatchPreparedInteraction(ctx context.Context, session model.LiveRuntimeSession, item *decisionItem, text, audioURL, scope, memory string, expiresAt time.Time) error {
	w.updateMission(item, func(m *speechmission.Mission) {
		m.GeneratedText = text
		m.TTS.AudioURL = strings.TrimSpace(audioURL)
	})
	action := "answer"
	if strings.EqualFold(strings.TrimSpace(item.ManualAction), "quick") {
		action = "quick"
	}
	w.transitionMission(item, speechmission.StateWaitingCutPoint, "dispatch_start", "TTS已生成，等待Core确认实际打断与回归位置")
	cancelBridge := w.startReturnBridge(session, *item, scope, memory)
	dispatchResult, err := w.dispatch(ctx, session, *item, action, text, audioURL)
	if err != nil {
		cancelBridge()
		var conflict *safeSwitchRetry
		if errors.As(err, &conflict) && ctx.Err() == nil {
			w.savePreparedInteraction(session, *item, text, audioURL, scope, memory, expiresAt)
			w.transitionMission(item, speechmission.StateCreated, "cut_reschedule", "切点已变化，保留已审核声音等待重新调度")
			log.Printf("decision prepared audio retained tenant=%d room=%d decision=%s reason=%s", session.TenantID, session.RoomID, item.ID, conflict.message)
			return errPreparedInteractionWaiting
		}
		w.transitionMission(item, speechmission.StateFailed, "dispatch_failed", err.Error())
		return err
	}
	if !dispatchResult.DeferredReturn {
		cancelBridge()
	}
	w.updateMission(item, func(m *speechmission.Mission) {
		if dispatchResult.SwitchAtMS != nil {
			m.Mainline.SwitchAtMS = *dispatchResult.SwitchAtMS
		}
		if dispatchResult.ResumeOffsetMS != nil {
			m.Resume.ActualResumeAtMS = *dispatchResult.ResumeOffsetMS
		}
		if strings.TrimSpace(dispatchResult.ResumeStrategy) != "" {
			m.Resume.Strategy = strings.TrimSpace(dispatchResult.ResumeStrategy)
		}
		if strings.TrimSpace(dispatchResult.ResumeSegmentID) != "" {
			m.Resume.ActualResumeSegment = strings.TrimSpace(dispatchResult.ResumeSegmentID)
		}
		if dispatchResult.ActualSkipCount != nil {
			m.Resume.SkipCount = *dispatchResult.ActualSkipCount
		}
		m.Resume.BridgeText = item.BridgeText
		m.Resume.DedupTriggered = dispatchResult.DedupTriggered
		m.Resume.DuplicateScore = dispatchResult.DuplicateScore
	})
	w.transitionMission(item, speechmission.StateDispatched, "core_dispatched", dispatchResult.traceNote())
	w.archiveSessionAudio(ctx, session, "reply:"+item.ID, "interaction", text, audioURL, dispatchResult.Task.DurationMS)
	sourceType := "interrupt_answer"
	if action == "quick" {
		sourceType = "interrupt_quick"
	}
	historyCtx, historyCancel := context.WithTimeout(ctx, 3*time.Second)
	historyErr := w.store.RecordGeneratedSpeechHistory(historyCtx, model.GeneratedSpeechHistoryInput{
		TenantID:          session.TenantID,
		RoomID:            session.RoomID,
		RuntimeSessionID:  session.ID,
		RuntimeExternalID: session.ExternalID,
		DecisionID:        item.ID,
		SourceType:        sourceType,
		QuestionText:      primaryQuestion(*item),
		GeneratedText:     text,
	})
	historyCancel()
	if historyErr != nil {
		// 播音已经成功下发，历史写入失败不能触发重试，否则会重复播音。
		log.Printf("decision executor history tenant=%d room=%d decision=%s: %v", session.TenantID, session.RoomID, item.ID, historyErr)
	}
	// Core 自己按 started_at + duration_ms 完成播音任务；终端回报只用于设备健康与排查。
	w.clearBackoff(session.RoomID)
	return nil
}

type SimulationOutput struct {
	Question         string `json:"question"`
	Reply            string `json:"reply"`
	ExecutionMode    string `json:"execution_mode"`
	PlanName         string `json:"plan_name,omitempty"`
	UserLayerVersion uint64 `json:"user_layer_version,omitempty"`
}

type coreStrategySelection struct {
	Category                string   `json:"category"`
	Key                     string   `json:"key"`
	Name                    string   `json:"name"`
	PlannedCutMS            int      `json:"planned_cut_ms,omitempty"`
	ResumeOffsetMS          int      `json:"resume_offset_ms,omitempty"`
	ResumeReason            string   `json:"resume_reason,omitempty"`
	ResumePreview           string   `json:"resume_preview,omitempty"`
	ResumeSegmentID         string   `json:"resume_segment_id,omitempty"`
	CutAfterSegmentID       string   `json:"cut_after_segment_id,omitempty"`
	OriginalResumeSegmentID string   `json:"original_resume_segment_id,omitempty"`
	CoveredSegmentIDs       []string `json:"covered_segment_ids,omitempty"`
	PlannedResumeSegmentID  string   `json:"planned_resume_segment_id,omitempty"`
	SkipCount               int      `json:"skip_count,omitempty"`
	SkippedPreviews         []string `json:"skipped_previews,omitempty"`
}

func (w *Worker) selectCoreStrategy(ctx context.Context, session model.LiveRuntimeSession, seed, category string, candidates []string) (coreStrategySelection, error) {
	if w == nil || w.core == nil || session.TenantID <= 0 || session.RoomID <= 0 {
		return coreStrategySelection{}, nil
	}
	resp, err := w.core.DoRoom(
		ctx, session.TenantID, session.RoomID, http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/strategy-select", session.RoomID),
		tenantQuery(session.TenantID),
		map[string]any{"category": category, "candidates": candidates, "seed": seed},
	)
	if err != nil {
		return coreStrategySelection{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return coreStrategySelection{}, fmt.Errorf("strategy select http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var selected coreStrategySelection
	if err := json.NewDecoder(resp.Body).Decode(&selected); err != nil {
		return coreStrategySelection{}, err
	}
	return selected, nil
}

func (w *Worker) selectCoreResumeStrategy(ctx context.Context, session model.LiveRuntimeSession, seed, topic string, estimatedMS int) (coreStrategySelection, error) {
	if w == nil || w.core == nil || session.TenantID <= 0 || session.RoomID <= 0 {
		return coreStrategySelection{}, nil
	}
	resp, err := w.core.DoRoom(
		ctx, session.TenantID, session.RoomID, http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/strategy-select", session.RoomID),
		tenantQuery(session.TenantID),
		map[string]any{
			"category":     "resume",
			"seed":         seed,
			"topic":        strings.TrimSpace(topic),
			"estimated_ms": estimatedMS,
		},
	)
	if err != nil {
		return coreStrategySelection{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return coreStrategySelection{}, fmt.Errorf("resume strategy select http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var selected coreStrategySelection
	if err := json.NewDecoder(resp.Body).Decode(&selected); err != nil {
		return coreStrategySelection{}, err
	}
	return selected, nil
}

func interruptStrategyInstruction(key string) string {
	switch strings.TrimSpace(key) {
	case "read_comment_softly":
		return "先像真人主播一样轻声、自然地读一下或复述当前弹幕，再回答；不要像系统播报。"
	case "hard_cut":
		return "本次采用直接切入：不要长铺垫，第一句就进入核心回答，但保持完整自然句。"
	case "ask_controller":
		return "本次采用中控确认感：只有涉及尚未确认的事实时才自然说一句向中控核实；已有明确事实不要假装不知道。"
	case "thinking_pause":
		return "本次保留很轻的思考感，可以有极短的‘嗯’或自然停顿，不要拖沓。"
	case "repeat_confirm":
		return "先用一句自然口语确认观众到底在问什么，再进入回答。"
	default:
		return ""
	}
}

type interruptTTSProfile struct {
	Instruction string
}

func ttsProfileForInterrupt(key string) interruptTTSProfile {
	switch strings.TrimSpace(key) {
	case "read_comment_softly":
		return interruptTTSProfile{
			Instruction: "像真人直播间轻声接弹幕：开头复述观众问题时声音轻一点、贴近聊天，随后回答正文恢复正常主播语气和力度；不要像系统播报。",
		}
	case "hard_cut":
		return interruptTTSProfile{
			Instruction: "直接、利落地切入核心回答，起句清楚，不做长铺垫，不故意拖停顿；保持真人主播自然语气。",
		}
	case "ask_controller":
		return interruptTTSProfile{
			Instruction: "像真人主播现场确认信息：需要核实时带一点向中控确认后的自然感觉，语气克制真实；已有明确事实时直接回答，不表演、不夸张。",
		}
	case "thinking_pause":
		return interruptTTSProfile{
			Instruction: "开头带非常轻微的思考感，允许一次很短的自然停顿或轻微语气词，随后马上进入回答；不要拖沓。",
		}
	case "repeat_confirm":
		return interruptTTSProfile{
			Instruction: "先像真人主播一样自然确认观众问题，再顺势回答；确认句简短，不机械重复整段弹幕。",
		}
	default:
		return interruptTTSProfile{}
	}
}

func resumeStrategyInstruction(key string) string {
	switch strings.ToUpper(strings.TrimSpace(key)) {
	case "DIRECT":
		return "核心问题回答清楚后自然收住，不额外复述下一句主线。"
	case "BRIDGE":
		return "核心回答结束后必须加一句很短、口语化的桥接，把话题自然交还主线；桥接必须作为最后一句完整独立句，不要照抄下一句主线，也不要每次固定同一句。"
	case "FUSION_SKIP":
		return "把当前问题相关内容一次说完整，尾句自然收束；系统会跳过已经被回答覆盖的后续内容，不要再预告重复内容。"
	case "CROSS_RESUME":
		return "回答尾部做一次自然的话题收束，给系统跨过已覆盖段落留下干净入口，不要念出内部跳段逻辑。"
	case "RE_ANCHOR":
		return "长互动结束时用一句简短口语把注意力重新拉回当前商品或直播主轴，再结束本段回答。"
	case "SWITCH_PLAN":
		return "完整收束当前回答，不承诺继续刚才那一段旧主线，给系统切换到新的主线入口。"
	default:
		return ""
	}
}

func estimatedAnswerDurationMS(item decisionItem) int {
	if strings.EqualFold(strings.TrimSpace(item.ManualAction), "quick") {
		return 6000
	}
	questionRunes := utf8.RuneCountInString(strings.TrimSpace(primaryQuestion(item)))
	switch {
	case questionRunes <= 12:
		return 8000
	case questionRunes <= 30:
		return 11000
	default:
		return 15000
	}
}

func hostStateDeliveryGuidance(state string) string {
	state = strings.TrimSpace(state)
	if state == "" {
		return ""
	}
	lower := strings.ToLower(state)
	containsAny := func(values ...string) bool {
		for _, value := range values {
			if value != "" && strings.Contains(lower, strings.ToLower(value)) {
				return true
			}
		}
		return false
	}
	switch {
	case containsAny("咳嗽", "感冒", "嗓子", "喉咙", "不舒服", "生病"):
		return "表达动作：语速稍慢、句子缩短、声音略轻、停顿更自然；必要时允许轻微清嗓或短暂停顿，但不要解释原因，也不要把身体状态说成台词。"
	case containsAny("累", "疲惫", "困", "没精神"):
		return "表达动作：语速略慢、句子更短、减少连续长句和高强度情绪，停顿自然；不要向观众解释主播疲惫状态。"
	case containsAny("兴奋", "开心", "状态好", "精神好"):
		return "表达动作：语气更有精神、更明快，但保持自然，不刻意喊叫或夸张。"
	case containsAny("紧张", "焦虑"):
		return "表达动作：语速稳一点、句子清楚、停顿更从容，避免连续急促表达；不要主动说明主播紧张。"
	default:
		return "表达动作：只根据当前状态微调语速、句长、停顿、声音力度和情绪，不改变正文事实；后台状态本身绝不能说出口。"
	}
}

func (w *Worker) ttsInstructionForMission(item *decisionItem, base string) string {
	parts := make([]string, 0, 6)
	if value := strings.TrimSpace(base); value != "" {
		parts = append(parts, value)
	}
	if mission, ok := w.missionSnapshot(item); ok {
		if strings.TrimSpace(mission.HumanStyle.Guidance) != "" {
			parts = append(parts, "整体表达自然、温和、有真人直播临场感；语速保持自然，不要朗读腔，也不要刻意夸张情绪。")
		}
		if state := strings.TrimSpace(mission.HumanStyle.State.HostState); state != "" {
			parts = append(parts, hostStateDeliveryGuidance(state))
		}
		reaction := mission.HumanStyle.Reaction
		if reaction.Enabled && (strings.EqualFold(reaction.Channel, "TTS_STYLE") || strings.EqualFold(reaction.Channel, "MIXED")) {
			if instruction := strings.TrimSpace(reaction.Instruction); instruction != "" {
				parts = append(parts, instruction)
			}
		}
	}
	return strings.Join(uniqueNonEmptyStrings(parts), "；")
}

func (w *Worker) generateDecisionText(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) (string, error) {
	return w.generateDecisionTextMutable(ctx, session, &item)
}

func (w *Worker) generateDecisionTextMutable(ctx context.Context, session model.LiveRuntimeSession, item *decisionItem) (string, error) {
	if session.ID > 0 {
		ctx = agentgateway.WithRoomModelScope(ctx, agentgateway.RoomModelScope{TenantID: session.TenantID, RoomID: session.RoomID, RuntimeID: session.ID, RequireLease: true})
	}
	if item == nil {
		return "", fmt.Errorf("待执行策略为空")
	}
	if item.BusinessCoreOnly {
		item.EmotionDisabled = true
	} else {
		emotionEnabled, emotionErr := w.emotionEnabledForRoom(ctx, session.TenantID, session.RoomID)
		if emotionErr != nil {
			return "", fmt.Errorf("读取实时情感设置失败: %w", emotionErr)
		}
		item.EmotionDisabled = !emotionEnabled
	}
	if strings.EqualFold(strings.TrimSpace(item.ExecutionMode), "verbatim") {
		// 100%原话只表示不主动改写；规则层仍然拥有最终播出否决权。
		text := strings.TrimSpace(item.FixedText)
		if text == "" {
			text = strings.TrimSpace(primaryQuestion(*item))
		}
		if text == "" {
			return "", fmt.Errorf("100%%原话模式没有可播出的固定文字")
		}
		text = item.nicknameSpeechText(text)
		if strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("最终播出文字为空")
		}
		item.BridgeText = ""
		w.updateMission(item, func(m *speechmission.Mission) {
			m.GeneratedText = text
			m.Resume.BridgeText = item.BridgeText
		})
		return text, nil
	}

	// 新轻量互动策略机：全部互动类型都走这里。
	// 旧「意图分析→规划→回答」三步模型链路已下线，不再有 answerPrompt / 业务规划 /
	// 回答说明 / FAQ 缓存 / 回答自评这些分支。
	// 但调度层（interrupt 打断 / resume 回归 / addressing 称呼）+ plan 冻结必须照常，
	// 否则 mission 黑板上缺调度策略、PlanFrozenAt 不冻结，下游切点/回归会丢。
	seed := strings.TrimSpace(item.ID)
	if seed == "" {
		seed = strings.TrimSpace(primaryQuestion(*item))
	}
	w.applyStrategyPipeline(ctx, session, seed, item)
	// Recording-stream programs are a queue of pre-built tracks. Core's
	// interaction-return bridge has no valid return scope for them; DIRECT lets
	// the approved answer play and continue with the next queued track.
	if strings.EqualFold(strings.TrimSpace(item.SelectedResume), "BRIDGE") && w.recordingStreamPublished(ctx, session) {
		item.SelectedResume = "DIRECT"
		item.BridgeText = ""
	}
	if item.contentError != nil {
		return "", item.contentError
	}
	text := w.lightweightInteractionText(ctx, session, *item)
	text = item.nicknameSpeechText(text)
	text = ensureAddressPunctuation(text, item.PromptAddressPhrase)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("轻量互动策略机未产出可播文字")
	}
	w.transitionMission(item, speechmission.StateGeneratingText, "generate_text", "轻量互动策略机已生成可播正文（跳过合规终审）")
	item.BridgeText = ""
	w.updateMission(item, func(m *speechmission.Mission) {
		m.GeneratedText = text
		m.Resume.BridgeText = item.BridgeText
	})
	return text, nil
}

func extractFinalSpeechSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	end := len(runes)
	var terminal rune
	if end > 0 && strings.ContainsRune("。！？!?；;", runes[end-1]) {
		terminal = runes[end-1]
		end--
	}
	for end > 0 && unicode.IsSpace(runes[end-1]) {
		end--
	}
	start := -1
	for i := end - 1; i >= 0; i-- {
		if strings.ContainsRune("。！？!?；;", runes[i]) {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return ""
	}
	for start < end && unicode.IsSpace(runes[start]) {
		start++
	}
	if start >= end {
		return ""
	}
	tail := strings.TrimSpace(string(runes[start:end]))
	if tail == "" {
		return ""
	}
	if terminal != 0 {
		tail += string(terminal)
	}
	return tail
}

func (w *Worker) SimulateAnswer(ctx context.Context, tenantID, roomID int64, question string) (SimulationOutput, error) {
	return w.SimulateAnswerWithPreview(ctx, tenantID, roomID, question, "", "", "", 0)
}

func (w *Worker) SimulateAnswerWithPreview(
	ctx context.Context,
	tenantID, roomID int64,
	question, previewInstruction, previewMemoryType, previewMemoryKey string,
	previewMatchedMemoryID int64,
) (SimulationOutput, error) {
	return w.simulateAnswer(ctx, tenantID, roomID, question, previewInstruction, previewMemoryType, previewMemoryKey, previewMatchedMemoryID, nil, nil, "")
}

func (w *Worker) SimulateInteraction(ctx context.Context, tenantID, roomID int64, question, previewInstruction string, recent []InteractionRecentAnswer) (SimulationOutput, error) {
	if err := ValidateInteractionRecentAnswers(recent); err != nil {
		return SimulationOutput{}, err
	}
	return w.simulateAnswer(ctx, tenantID, roomID, question, previewInstruction, "semantic", "", 0, recent, nil, "")
}

// SimulateInteractionWithIntent runs the test-mode answer through the real
// intent result (purpose + angles), so test mode and live chat share the same
// fact selection instead of bypassing intent with a bare manual question.
func (w *Worker) SimulateInteractionWithIntent(ctx context.Context, tenantID, roomID int64, question, previewInstruction string, recent []InteractionRecentAnswer, intent interactionpurpose.Result, nickname string) (SimulationOutput, error) {
	if err := ValidateInteractionRecentAnswers(recent); err != nil {
		return SimulationOutput{}, err
	}
	return w.simulateAnswer(ctx, tenantID, roomID, question, previewInstruction, "semantic", "", 0, recent, &intent, nickname)
}

func (w *Worker) simulateAnswer(
	ctx context.Context,
	tenantID, roomID int64,
	question, previewInstruction, previewMemoryType, previewMemoryKey string,
	previewMatchedMemoryID int64,
	recent []InteractionRecentAnswer,
	intent *interactionpurpose.Result,
	nickname string,
) (SimulationOutput, error) {
	question = strings.TrimSpace(question)
	if tenantID <= 0 || roomID <= 0 || question == "" {
		return SimulationOutput{}, fmt.Errorf("测试问题不能为空")
	}
	if w == nil || w.store == nil || w.agent == nil {
		return SimulationOutput{}, fmt.Errorf("测试智能体未初始化")
	}
	session := model.LiveRuntimeSession{TenantID: tenantID, RoomID: roomID}
	var purposeID string
	var purposeKnowledge json.RawMessage
	var intentAngles []contentstrategy.Row
	if intent != nil {
		purposeID = intent.PurposeID
		purposeKnowledge, _ = json.Marshal(intent)
		intentAngles = intent.Angles
	}
	item := decisionItem{
		BusinessCoreOnly:       true,
		RecentAnswers:          recent,
		Nicknames:              []string{},
		PurposeID:              purposeID,
		PurposeKnowledge:       purposeKnowledge,
		IntentAngles:           intentAngles,
		MissionKind:            "reply_question",
		Scene:                  "product_question",
		Title:                  "测试模拟观众提问",
		Summary:                "测试模式模拟真实观众问题，只生成最终回答，不播音",
		SampleQuestions:        []string{question},
		ManualAction:           "answer",
		ManualOrigin:           "test_simulation",
		ExecutionMode:          "intent",
		PreviewInstruction:     strings.TrimSpace(previewInstruction),
		PreviewMemoryType:      strings.TrimSpace(previewMemoryType),
		PreviewMemoryKey:       strings.TrimSpace(previewMemoryKey),
		PreviewMatchedMemoryID: previewMatchedMemoryID,
	}
	if nickname = strings.TrimSpace(nickname); nickname != "" {
		item.Nicknames = []string{nickname}
	}
	text, err := w.generateDecisionTextMutable(ctx, session, &item)
	if err != nil {
		return SimulationOutput{}, err
	}
	result := SimulationOutput{Question: question, Reply: text, ExecutionMode: "intent"}
	if plan, planErr := w.store.GetLiveAgentPlanForRoom(ctx, tenantID, roomID); planErr == nil {
		result.PlanName = strings.TrimSpace(plan.Name)
	}
	return result, nil
}

func (w *Worker) claim(ctx context.Context, session model.LiveRuntimeSession) (claimResponse, error) {
	query := tenantQuery(session.TenantID)
	resp, err := w.core.DoRoom(
		ctx, session.TenantID, session.RoomID, http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions/claim", session.RoomID),
		query, nil,
	)
	if err != nil {
		return claimResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return claimResponse{}, fmt.Errorf("claim http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var result claimResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return claimResponse{}, err
	}
	return result, nil
}

func (w *Worker) release(ctx context.Context, session model.LiveRuntimeSession, decisionID string, reasons ...string) error {
	query := tenantQuery(session.TenantID)
	if len(reasons) > 0 && reasons[0] == "cut_reschedule" {
		query.Set("reason", "cut_reschedule")
		w.mu.Lock()
		prepared, ok := w.prepared[preparedInteractionKey{session.TenantID, session.RoomID, session.ID, decisionID}]
		w.mu.Unlock()
		if ok {
			query.Set("prepared_until", prepared.ExpiresAt.UTC().Format(time.RFC3339Nano))
		}
	}
	resp, err := w.core.DoRoom(
		ctx, session.TenantID, session.RoomID, http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions/%s/release", session.RoomID, url.PathEscape(decisionID)),
		query, nil,
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return fmt.Errorf("release http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (w *Worker) completeSimulation(
	ctx context.Context,
	session model.LiveRuntimeSession,
	item decisionItem,
	text, executionMode, planName string,
	userLayerVersion uint64,
) error {
	query := tenantQuery(session.TenantID)
	resp, err := w.core.DoRoom(
		ctx, session.TenantID, session.RoomID, http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions/%s/simulation-complete", session.RoomID, url.PathEscape(item.ID)),
		query,
		map[string]any{
			"reply":              text,
			"execution_mode":     executionMode,
			"plan_name":          planName,
			"user_layer_version": userLayerVersion,
		},
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return fmt.Errorf("complete simulation http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

type dispatchResult struct {
	DeferredReturn bool `json:"deferred_return"`
	Task           struct {
		ID         string `json:"speech_task_id"`
		DurationMS int    `json:"duration_ms"`
	} `json:"task"`
	MissionID       string  `json:"mission_id,omitempty"`
	Dispatched      bool    `json:"dispatched"`
	Action          string  `json:"action,omitempty"`
	SwitchAtMS      *int    `json:"switch_at_ms,omitempty"`
	ResumeOffsetMS  *int    `json:"resume_offset_ms,omitempty"`
	ResumeStrategy  string  `json:"resume_strategy,omitempty"`
	ResumeSegmentID string  `json:"resume_segment_id,omitempty"`
	ActualSkipCount *int    `json:"actual_skip_count,omitempty"`
	BridgeUsed      bool    `json:"bridge_used"`
	DedupTriggered  bool    `json:"dedup_triggered"`
	DuplicateScore  float64 `json:"duplicate_score,omitempty"`
}

func (result dispatchResult) traceNote() string {
	parts := []string{"Core已接收互动语音"}
	if result.SwitchAtMS != nil {
		parts = append(parts, fmt.Sprintf("实际打断点=%dms", *result.SwitchAtMS))
	}
	if result.ResumeOffsetMS != nil {
		parts = append(parts, fmt.Sprintf("实际回归点=%dms", *result.ResumeOffsetMS))
	}
	if value := strings.TrimSpace(result.ResumeSegmentID); value != "" {
		parts = append(parts, "实际回归段="+value)
	}
	if result.ActualSkipCount != nil {
		parts = append(parts, fmt.Sprintf("实际跳过段数=%d", *result.ActualSkipCount))
	}
	if value := strings.TrimSpace(result.ResumeStrategy); value != "" {
		parts = append(parts, "实际回归策略="+value)
	}
	if result.DedupTriggered {
		parts = append(parts, fmt.Sprintf("回归去重已触发(%.3f)", result.DuplicateScore))
	}
	return strings.Join(parts, "；")
}

func (w *Worker) dispatch(
	ctx context.Context, session model.LiveRuntimeSession, item decisionItem, action, text, audioURL string,
) (dispatchResult, error) {
	for attempt := 0; ; attempt++ {
		result, err := w.dispatchOnce(ctx, session, item, action, text, audioURL)
		var conflict *safeSwitchRetry
		if err == nil || !errors.As(err, &conflict) || attempt >= 2 {
			return result, err
		}
		// Core explicitly rejected before publishing. Reuse complete audio;
		// uncertain transport outcomes are never repeated automatically.
		item.PlannedSwitchAtMS = 0
		timer := time.NewTimer(300 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return dispatchResult{}, ctx.Err()
		case <-timer.C:
		}
	}
}

type safeSwitchRetry struct{ message string }

func (e *safeSwitchRetry) Error() string { return e.message }

func (w *Worker) dispatchOnce(
	ctx context.Context,
	session model.LiveRuntimeSession,
	item decisionItem,
	action, text, audioURL string,
) (dispatchResult, error) {
	query := tenantQuery(session.TenantID)
	resp, err := w.core.DoRoom(
		ctx, session.TenantID, session.RoomID, http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/audio/interaction", session.RoomID),
		query,
		map[string]any{
			"deferred_return":          true,
			"allow_consecutive_repeat": strings.EqualFold(strings.TrimSpace(item.ExecutionMode), "verbatim"),
			"decision_id":              item.ID,
			"mission_id":               missionID(&item),
			"mission_kind":             strings.ToLower(strings.TrimSpace(item.MissionKind)),
			"content_goal":             item.ContentGoal, "content_angle": item.ContentAngle,
			"selected_content_plan": item.SelectedContentPlan,
			"preparation_cut_plans": item.PreparationCutPlans,
			"session_id":            session.ExternalID,
			"action":                action,
			"audio_url":             audioURL,
			"question":              primaryQuestion(item),
			"reply_text":            text,
			"topic":                 item.Topic,
			"interrupt_strategy":    item.SelectedInterrupt,
			"resume_strategy":       item.SelectedResume,
			"bridge_text":           item.BridgeText,
			"humanization_strategy": item.HumanizationStrategy,
			"humanization_kind":     item.HumanizationKind,
			"humanization_applied":  item.HumanizationApplied,
			"switch_at_ms":          item.PlannedSwitchAtMS,
		},
	)
	if err != nil {
		return dispatchResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		if resp.StatusCode == http.StatusConflict && (strings.Contains(string(raw), "主线已换段") || strings.Contains(string(raw), "安全句末切点已失效") || strings.Contains(string(raw), "当前35秒内没有安全句末切点")) {
			return dispatchResult{}, &safeSwitchRetry{message: string(raw)}
		}
		return dispatchResult{}, fmt.Errorf("audio dispatch http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var result dispatchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return dispatchResult{}, fmt.Errorf("decode audio dispatch result: %w", err)
	}
	return result, nil
}

func (w *Worker) emotionEnabledForRoom(ctx context.Context, tenantID, roomID int64) (bool, error) {
	published, err := w.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, tenantID, roomID)
	if err != nil {
		if errors.Is(err, appdb.ErrLiveAgentPlanVersionNotFound) {
			return false, nil
		}
		return false, err
	}
	return published.VoiceIdentity.EmotionEnabled != nil && *published.VoiceIdentity.EmotionEnabled, nil
}

func (w *Worker) readyVoice(ctx context.Context, tenantID, roomID int64) (model.VoiceProfile, bool, error) {
	// The published live-agent version is the immutable contract for realtime
	// interaction audio. Pre-recorded/generated mainline audio is an independent
	// playable asset and does not need to share the same voice model or voice id.
	if published, publishedErr := w.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, tenantID, roomID); publishedErr == nil {
		identity := published.VoiceIdentity
		if strings.TrimSpace(identity.VoiceID) == "" || strings.TrimSpace(identity.Model) == "" {
			return model.VoiceProfile{}, false, fmt.Errorf("已发布智能体版本缺少可用声音身份")
		}
		rate := identity.Rate
		if rate < 0.5 || rate > 2 {
			rate = 1
		}
		return model.VoiceProfile{
			Name:        strings.TrimSpace(identity.Name),
			Provider:    strings.TrimSpace(identity.Provider),
			VoiceID:     strings.TrimSpace(identity.VoiceID),
			CloneStatus: "ready",
			Config: map[string]any{
				"source":           strings.TrimSpace(identity.Source),
				"target_model":     strings.TrimSpace(identity.Model),
				"rate":             rate,
				"identity_version": strings.TrimSpace(identity.Version),
				"binding_id":       identity.BindingID,
				"emotion_enabled":  identity.EmotionEnabled != nil && *identity.EmotionEnabled,
			},
		}, true, nil
	} else if !errors.Is(publishedErr, appdb.ErrLiveAgentPlanVersionNotFound) {
		return model.VoiceProfile{}, false, publishedErr
	}

	return model.VoiceProfile{}, false, fmt.Errorf("请先为当前方案保存可用声音")
}

func selectedRoomVoice(versions []model.AgentConfigVersion, roomID int64) map[string]any {
	roomKey := strconv.FormatInt(roomID, 10)
	for _, version := range versions {
		if !strings.EqualFold(strings.TrimSpace(version.LifecycleStatus), "active") {
			continue
		}
		rooms, _ := version.SpeechConfig["rooms"].(map[string]any)
		room, _ := rooms[roomKey].(map[string]any)
		voice, _ := room["selected_voice"].(map[string]any)
		if len(voice) > 0 {
			return voice
		}
	}
	return nil
}

func mapInt64(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func officialVoiceProfile(voice voicecatalog.Voice) model.VoiceProfile {
	provider := voice.Provider
	if provider == "" {
		provider = "aliyun_qwen"
	}
	return model.VoiceProfile{
		Name:        voice.Name,
		Provider:    provider,
		VoiceID:     voice.ID,
		CloneStatus: "ready",
		IsDefault:   voice.ID == voicecatalog.DefaultVoiceID,
		Config: map[string]any{
			"source":       "system_official",
			"target_model": voice.Model,
			"rate":         1.0,
		},
	}
}

var quotedRiskTermPattern = regexp.MustCompile("[“\"「『]([^”\"」』]{1,24})[”\"」』]")

// 风险否定句式：没有X风险 / 无X风险 / 不存在X风险 / 毫无X隐患，都是零风险承诺，
// 事实未确认"安全性"时禁止说。例："没有卡喉的风险"。
var riskDenialPattern = regexp.MustCompile(`(没有|无|不存在|毫无|绝对没有)[^，。；,;！!？?，。]{0,10}(风险|危险|隐患)`)

var builtInAbsoluteRiskTerms = []string{
	"绝对不踩雷",
	"百分百",
	"100%",
	"绝对",
	"保证",
	"包你",
	"肯定",
	"永远",
	"零风险",
	"全网最低",
	"全网第一",
	"第一名",
}

var internalSpeechLeakTerms = []string{
	"【仅供模型内部执行的生成控制】",
	"【本次称呼】",
	"【本次打断行为】",
	"【本次回归策略】",
	"【当前方案动态事实：实时热更新】",
	"【当前主播风格：实时热更新】",
	"【主线衔接硬要求】",
	"隐藏生成条件",
	"策略名称：",
	"系统提示：",
}

func limitSpeechText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 || text == "" {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	cut := maxRunes
	searchStart := maxRunes - 80
	if searchStart < 0 {
		searchStart = 0
	}
	for index := maxRunes - 1; index >= searchStart; index-- {
		switch runes[index] {
		case '。', '！', '？', '；', '!', '?', ';':
			cut = index + 1
			index = -1
		}
	}
	result := strings.TrimSpace(string(runes[:cut]))
	if result == "" {
		return ""
	}
	last := []rune(result)
	if len(last) > 0 {
		switch last[len(last)-1] {
		case '。', '！', '？', '!', '?':
			return result
		}
	}
	return strings.TrimRight(result, "，、；;：:, ") + "。"
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

type liveAgentPlanStyleOverlayStore interface {
	GetLiveAgentPlanStyleOverlay(context.Context, int64, int64) (model.LiveAgentPlanStyleOverlayProfile, error)
}

func trimContextRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || value == "" {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

func primaryQuestion(item decisionItem) string {
	for _, question := range item.SampleQuestions {
		if value := strings.TrimSpace(question); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(item.Summary); value != "" {
		return value
	}
	return strings.TrimSpace(item.Title)
}

func voiceModel(profile model.VoiceProfile) string {
	if profile.Config != nil {
		if raw, ok := profile.Config["target_model"]; ok {
			if value := strings.TrimSpace(fmt.Sprint(raw)); value != "" {
				return value
			}
		}
	}
	return ""
}

func voiceProvider(profile model.VoiceProfile) string {
	if voiceModel(profile) == ttsgateway.TencentPremiumModel {
		return ttsgateway.ProviderTencent
	}
	if ttsgateway.IsCosyVoiceModel(voiceModel(profile)) {
		return ttsgateway.ProviderCosyVoice
	}
	switch strings.ToLower(strings.TrimSpace(profile.Provider)) {
	case "", "aliyun_qwen", "aliyun_qwen_clone", "qwen", "dashscope", "qwen_audio_3_0", ttsgateway.ProviderQwen:
		return ttsgateway.ProviderQwen
	default:
		return strings.TrimSpace(profile.Provider)
	}
}

func voiceRate(profile model.VoiceProfile) float64 {
	if profile.Config == nil {
		return 1
	}
	raw, ok := profile.Config["rate"]
	if !ok {
		return 1
	}
	switch value := raw.(type) {
	case float64:
		if value >= 0.5 && value <= 2 {
			return value
		}
	case float32:
		rate := float64(value)
		if rate >= 0.5 && rate <= 2 {
			return rate
		}
	case int:
		rate := float64(value)
		if rate >= 0.5 && rate <= 2 {
			return rate
		}
	}
	return 1
}

func tenantQuery(tenantID int64) url.Values {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	return query
}

func (w *Worker) beginRoom(roomID int64) (time.Time, bool) {
	if roomID <= 0 {
		return time.Time{}, false
	}
	now := w.now()
	w.mu.Lock()
	defer w.mu.Unlock()
	if now.Before(w.retryAfter[roomID]) {
		return time.Time{}, false
	}
	if startedAt, exists := w.inFlight[roomID]; exists {
		if now.Sub(startedAt) < inFlightWatchdog {
			return time.Time{}, false
		}
		log.Printf("decision executor stale in-flight released room=%d age=%s", roomID, now.Sub(startedAt).Round(time.Second))
	}
	w.inFlight[roomID] = now
	return now, true
}

func (w *Worker) endRoom(roomID int64, startedAt time.Time) {
	w.mu.Lock()
	if current, exists := w.inFlight[roomID]; exists && current.Equal(startedAt) {
		delete(w.inFlight, roomID)
	}
	w.mu.Unlock()
}

func (w *Worker) backoffRoom(roomID int64) {
	w.mu.Lock()
	w.retryAfter[roomID] = w.now().Add(errorBackoff)
	w.mu.Unlock()
}

func (w *Worker) clearBackoff(roomID int64) {
	w.mu.Lock()
	delete(w.retryAfter, roomID)
	w.mu.Unlock()
}

func (w *Worker) recordingStreamPublished(ctx context.Context, session model.LiveRuntimeSession) bool {
	if w == nil || w.store == nil {
		return false
	}
	version, err := w.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return false
	}
	return isRecordingStreamVersion(version)
}

func isRecordingStreamVersion(version model.LiveAgentPlanVersion) bool {
	mode, _ := version.GenerationContext["recording_voice_mode"].(string)
	return strings.EqualFold(strings.TrimSpace(mode), "stream_clone")
}
