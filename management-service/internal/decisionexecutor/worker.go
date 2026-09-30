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

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/agentmemory"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
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
	LoadLivePolicyLayers(context.Context, int64, int64) (string, *model.LivePolicyVersion, *model.LivePolicyVersion, *model.LivePolicyVersion, error)
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
	store    store
	core     coreDoer
	agent    completer
	tts      synthesizer
	leader   leader
	interval time.Duration
	now      func() time.Time
	missions *speechmission.Registry
	slots    chan struct{}

	mu         sync.Mutex
	inFlight   map[int64]time.Time
	retryAfter map[int64]time.Time

	addressMu   sync.Mutex
	recentNamed map[int64]map[string]time.Time
}

type decisionItem struct {
	ID                     string                                `json:"id"`
	MissionID              string                                `json:"mission_id,omitempty"`
	Topic                  string                                `json:"topic"`
	Title                  string                                `json:"title"`
	Summary                string                                `json:"summary"`
	ReplyHint              string                                `json:"reply_hint"`
	SampleQuestions        []string                              `json:"sample_questions"`
	Nicknames              []string                              `json:"nicknames,omitempty"`
	ManualAction           string                                `json:"manual_action"`
	ManualOrigin           string                                `json:"manual_origin"`
	ExecutionMode          string                                `json:"execution_mode"`
	FixedText              string                                `json:"fixed_text"`
	PreviewInstruction     string                                `json:"preview_instruction,omitempty"`
	PreviewMemoryType      string                                `json:"preview_memory_type,omitempty"`
	PreviewMemoryKey       string                                `json:"preview_memory_key,omitempty"`
	PreviewMatchedMemoryID int64                                 `json:"preview_matched_memory_id,omitempty"`
	PlannedSwitchAtMS      int                                   `json:"-"`
	ForceAfterRest         bool                                  `json:"-"`
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

func hiddenStrategyPrompt(item decisionItem) string {
	guidance := uniqueNonEmptyStrings(item.HiddenStrategyGuidance)
	if len(guidance) == 0 {
		return ""
	}
	return "\n\n【策略黑板：仅供模型内部执行】\n" +
		"下面每一项都只是生成约束，不是台词，也不是要逐项照念的模板。把所有约束与现场上下文一次性融合成一段完整口播；任何策略都不得单独生成第二段台词。最终只输出主播真正会说的正文，绝对不要复述本区标题、策略名称、概率、内部说明或系统字段。\n- " +
		strings.Join(guidance, "\n- ")
}

func speechMissionPrompt(item decisionItem) string {
	kind := strings.ToLower(strings.TrimSpace(item.MissionKind))
	if kind == "" {
		return ""
	}
	label := map[string]string{
		"reply_chat":    "回复弹幕",
		"reply_follow":  "回应关注",
		"reply_like":    "回应点赞",
		"welcome_named": "点名欢迎",
		"welcome_batch": "打包欢迎",
	}[kind]
	if label == "" {
		label = kind
	}
	var builder strings.Builder
	builder.WriteString("\n\n【本次口播任务】")
	builder.WriteString("\n事件类型：")
	builder.WriteString(label)
	if item.MissionEventCount > 0 {
		builder.WriteString("\n聚合事件数：")
		builder.WriteString(strconv.Itoa(item.MissionEventCount))
	}
	if item.MissionWindowSeconds > 0 {
		builder.WriteString("\n事件聚合窗口：")
		builder.WriteString(strconv.Itoa(item.MissionWindowSeconds))
		builder.WriteString("秒")
	}
	builder.WriteString("\n任务原则：这次事件只生成一段完整口播；把事件目的、打断方式、回归目标、开头意图、称呼计划、主播风格和仿真人约束一次融合，不要拆成多段分别生成。")
	return builder.String()
}

type strategyStageFunc func(context.Context, model.LiveRuntimeSession, string, *decisionItem)

func (w *Worker) strategyStageRegistry() map[string]strategyStageFunc {
	return map[string]strategyStageFunc{
		"interaction": w.applyInteractionStrategyStage,
		"interrupt":   w.applyInterruptStrategyStage,
		"resume":      w.applyResumeStrategyStage,
		"opening":     w.applyOpeningStrategyStage,
		"addressing":  w.applyAddressingStrategyStage,
		"humanize":    w.applyHumanizeStrategyStage,
	}
}

func normalizeStrategyStageOrder(raw string, registry map[string]strategyStageFunc) []string {
	defaultOrder := []string{"interaction", "interrupt", "resume", "opening", "addressing", "humanize"}
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
	registry := w.strategyStageRegistry()
	rawOrder := w.store.AgentPromptValue(ctx, "live.strategy.pipeline.order", "interaction,interrupt,resume,opening,addressing,humanize")
	order := normalizeStrategyStageOrder(rawOrder, registry)
	item.AppliedStrategyStages = item.AppliedStrategyStages[:0]
	for _, key := range order {
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

func (w *Worker) applyInteractionStrategyStage(
	_ context.Context,
	_ model.LiveRuntimeSession,
	_ string,
	item *decisionItem,
) {
	if item == nil {
		return
	}
	kind := strings.ToLower(strings.TrimSpace(item.MissionKind))
	goal := interactionMissionGoal(kind)
	if goal == "" {
		return
	}
	decisionGuidance := ""
	if item.InteractionDecision.EventValue > 0 {
		decisionGuidance = " 该任务已经过事件价值和互动预算判断；不要在口播中透露热度、分值、预算、优先级或内部策略信息。"
		if debt := item.InteractionDecision.QuestionDebt; debt != nil && debt.UniqueUsers >= 2 {
			decisionGuidance += " 若语境自然，可概括为有不止一位观众关注这个问题，但不要机械报内部人数。"
		}
	}
	item.addStrategyConstraint(strategyConstraint{
		Stage:    "interaction",
		Key:      kind,
		Name:     item.Title,
		Guidance: "本轮互动目标：" + goal + "；只处理本轮需要回应的事件，不机械报数，不额外扩展不存在的事实。" + decisionGuidance,
		Required: true,
	})
	w.updateMission(item, func(m *speechmission.Mission) {
		m.Interaction = speechmission.InteractionPlan{
			Kind:          kind,
			Goal:          goal,
			EventCount:    item.MissionEventCount,
			WindowSeconds: item.MissionWindowSeconds,
			Decision:      item.InteractionDecision,
			Required:      true,
		}
	})
}

func interactionMissionGoal(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "reply_chat":
		return "自然回应当前有效弹幕或问题"
	case "reply_follow":
		return "自然感谢刚刚发生的关注，不机械报关注数量"
	case "reply_like":
		return "自然回应点赞支持，把多次点赞聚合成一次真人式感谢"
	case "welcome_named":
		return "低频自然欢迎刚进入直播间的具体观众；是否真正点名仍由称呼策略和语境决定"
	case "welcome_batch":
		return "自然欢迎最近进入的一批新朋友，不逐个报名字"
	case "conversion_signal":
		return "自然回应当前成交或下单信号，优先解决成交相关信息，但不得编造订单状态、库存、价格或承诺"
	default:
		return ""
	}
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

func (w *Worker) applyAddressingStrategyStage(
	ctx context.Context,
	session model.LiveRuntimeSession,
	seed string,
	item *decisionItem,
) {
	if item == nil {
		return
	}

	// Dynamic room interaction uses the room-level "称呼习惯" only.
	// The backend strategy-center addressing pool belongs to scripted/mainline copy generation
	// and must not leak into real-time replies, welcomes, likes or follow acknowledgements.
	preferences, prefErr := w.store.GetRoomAddressingPreferences(ctx, session.TenantID, session.RoomID)
	if prefErr != nil {
		preferences = model.RoomAddressingPreferences{NamingPreference: "natural"}
	}

	plan := w.buildAddressingPlan(session.RoomID, *item, "room_preference", "", preferences)
	plan = applyAddressingFrequency(plan, *item, seed)
	item.SelectedAddressing = plan.Candidate

	guidance := addressingPlanGuidance(plan)
	item.addStrategyConstraint(strategyConstraint{
		Stage:    "addressing",
		Key:      plan.Key,
		Name:     plan.Mode,
		Guidance: guidance,
		Required: !plan.Optional && !strings.EqualFold(plan.Mode, "NONE"),
	})
	w.updateMission(item, func(m *speechmission.Mission) {
		m.Addressing = plan
	})
	log.Printf(
		"decision dynamic addressing tenant=%d room=%d decision=%s source=room_preference mode=%s candidate=%s names=%s recent_penalty=%.2f optional=%t target_rate=%d selected_by_rate=%t",
		session.TenantID, session.RoomID, item.ID, plan.Mode, plan.Candidate,
		strings.Join(plan.SelectedNames, ","), plan.RecentNamePenalty, plan.Optional, plan.TargetRate, plan.SelectedByRate,
	)
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

func applyAddressingFrequency(plan speechmission.AddressingPlan, item decisionItem, seed string) speechmission.AddressingPlan {
	if strings.EqualFold(strings.TrimSpace(plan.Mode), "NONE") {
		plan.Optional = true
		plan.TargetRate = 0
		plan.SelectedByRate = false
		return plan
	}
	rate := addressingTargetRate(plan.Preference)
	if strings.EqualFold(strings.TrimSpace(item.MissionKind), "welcome_named") {
		rate = 100
	}
	plan.TargetRate = rate
	bucketSeed := strings.Join([]string{
		strings.TrimSpace(seed),
		strings.TrimSpace(item.ID),
		strings.TrimSpace(item.MissionKind),
		strings.TrimSpace(plan.Candidate),
	}, "|")
	selected := stableAddressingBucket(bucketSeed) < rate
	plan.SelectedByRate = selected
	plan.Optional = !selected
	if selected {
		return plan
	}
	plan.Mode = "NONE"
	plan.Candidate = ""
	plan.GroupLabel = ""
	plan.SelectedNames = nil
	return plan
}

func (w *Worker) buildAddressingPlan(roomID int64, item decisionItem, key, candidate string, preferences model.RoomAddressingPreferences) speechmission.AddressingPlan {
	eligibleNames, recentPenalty := w.playableAddressingNames(roomID, item.Nicknames)
	selectedNames := append([]string(nil), eligibleNames...)
	preference := strings.ToLower(strings.TrimSpace(preferences.NamingPreference))
	if preference != "less" && preference != "more" {
		preference = "natural"
	}
	preferredTerms := cleanAddressingTerms(preferences.PreferredTerms)
	blockedTerms := cleanAddressingTerms(preferences.BlockedTerms)
	candidate = strings.TrimSpace(candidate)
	if preferred := choosePreferredAddressingTerm(preferredTerms, blockedTerms, item.ID); preferred != "" {
		candidate = preferred
		key = "preferred"
	} else if addressingTermBlocked(candidate, blockedTerms) {
		candidate = ""
	}
	if len(selectedNames) > 2 {
		selectedNames = selectedNames[:2]
	}
	mode := addressingModeForPlan(item, candidate, selectedNames)
	if preference == "less" && isAggregateAddressingMission(item) {
		selectedNames = nil
		if candidate != "" {
			mode = "GROUP"
		} else {
			mode = "NONE"
		}
	}
	if preference == "more" && len(selectedNames) > 0 && isAggregateAddressingMission(item) {
		if candidate != "" {
			mode = "MIXED"
		} else {
			mode = "SAMPLE"
		}
	}
	switch mode {
	case "NONE", "GROUP":
		selectedNames = nil
	case "SINGLE":
		if len(selectedNames) > 1 {
			selectedNames = selectedNames[:1]
		}
	}
	maxNamedCount := 2
	if preference == "less" {
		maxNamedCount = 1
	}
	if key = strings.TrimSpace(key); key == "" {
		key = strings.ToLower(mode)
	}
	return speechmission.AddressingPlan{
		Mode:              mode,
		Candidate:         candidate,
		Key:               key,
		Preference:        preference,
		NamedCandidates:   eligibleNames,
		SelectedNames:     selectedNames,
		PreferredTerms:    preferredTerms,
		BlockedTerms:      blockedTerms,
		GroupLabel:        candidate,
		MaxNamedCount:     maxNamedCount,
		RecentNamePenalty: recentPenalty,
		Optional:          true,
	}
}

func cleanAddressingTerms(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
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
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
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
	hasNames := len(selectedNames) > 0
	switch kind {
	case "welcome_batch", "reply_follow":
		if item.MissionEventCount > 1 {
			if hasNames && hasGroupLabel {
				return "MIXED"
			}
			if hasNames {
				return "SAMPLE"
			}
			if hasGroupLabel {
				return "GROUP"
			}
			return "NONE"
		}
		if hasNames || hasGroupLabel {
			return "SINGLE"
		}
		return "NONE"
	case "reply_like":
		if hasGroupLabel {
			return "GROUP"
		}
		return "NONE"
	case "welcome_named":
		if hasNames || hasGroupLabel {
			return "SINGLE"
		}
		return "NONE"
	case "reply_chat":
		if item.MissionEventCount > 1 || len(item.SampleQuestions) > 1 {
			if hasGroupLabel {
				return "GROUP"
			}
			return "NONE"
		}
		if hasNames || hasGroupLabel {
			return "SINGLE"
		}
		return "NONE"
	default:
		if hasNames || hasGroupLabel {
			return "SINGLE"
		}
		return "NONE"
	}
}

func (w *Worker) playableAddressingNames(roomID int64, raw []string) ([]string, float64) {
	if w == nil || len(raw) == 0 {
		return nil, 0
	}
	now := time.Now().UTC()
	if w.now != nil {
		now = w.now().UTC()
	}
	cleaned := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, rawName := range raw {
		name, ok := normalizePlayableNickname(rawName)
		if !ok {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		cleaned = append(cleaned, name)
	}
	if len(cleaned) == 0 {
		return nil, 0
	}

	w.addressMu.Lock()
	defer w.addressMu.Unlock()
	recent := w.recentNamed[roomID]
	for name, usedAt := range recent {
		if now.Sub(usedAt) >= addressingNameCooldown {
			delete(recent, name)
		}
	}
	result := make([]string, 0, len(cleaned))
	suppressed := 0
	for _, name := range cleaned {
		if usedAt, exists := recent[name]; exists && now.Sub(usedAt) < addressingNameCooldown {
			suppressed++
			continue
		}
		result = append(result, name)
	}
	penalty := 0.0
	if total := len(result) + suppressed; total > 0 {
		penalty = float64(suppressed) / float64(total)
	}
	return result, penalty
}

func (w *Worker) recordUsedAddressingNames(roomID int64, names []string, finalText string) {
	if w == nil || roomID <= 0 || len(names) == 0 {
		return
	}
	finalText = strings.TrimSpace(finalText)
	if finalText == "" {
		return
	}
	now := time.Now().UTC()
	if w.now != nil {
		now = w.now().UTC()
	}
	used := make([]string, 0, len(names))
	for _, rawName := range names {
		name, ok := normalizePlayableNickname(rawName)
		if !ok || !strings.Contains(finalText, name) {
			continue
		}
		used = append(used, name)
	}
	if len(used) == 0 {
		return
	}
	w.addressMu.Lock()
	if w.recentNamed[roomID] == nil {
		w.recentNamed[roomID] = make(map[string]time.Time)
	}
	for _, name := range used {
		w.recentNamed[roomID][name] = now
	}
	w.addressMu.Unlock()
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

func addressingPlanGuidance(plan speechmission.AddressingPlan) string {
	selected := strings.Join(plan.SelectedNames, "、")
	groupLabel := strings.TrimSpace(plan.GroupLabel)
	base := ""
	switch strings.ToUpper(strings.TrimSpace(plan.Mode)) {
	case "NONE":
		base = "称呼计划：本轮不主动使用称呼；不要为了显得热情强塞昵称、朋友、老哥、宝子等称呼。"
	case "GROUP":
		if groupLabel != "" {
			if plan.Optional {
				base = "称呼计划：本轮面向群体；语境自然时可使用群体称呼“" + groupLabel + "”，但不要逐个报名字，也不要为了执行策略重复称呼。"
			} else {
				base = "称呼计划：本轮需要自然使用一次群体称呼“" + groupLabel + "”；只出现一次，不要逐个报名字，不要重复称呼。"
			}
		} else {
			base = "称呼计划：本轮面向群体；使用自然群体表达，不逐个点名。"
		}
	case "SAMPLE":
		if selected != "" {
			if plan.Optional {
				base = "称呼计划：本轮允许从可播昵称里抽样称呼“" + selected + "”，最多 " + strconv.Itoa(maxAddressingInt(plan.MaxNamedCount, 1)) + " 个；不得扩写或虚构其他昵称。"
			} else {
				base = "称呼计划：本轮需要自然点名“" + selected + "”中的 1 个，最多 " + strconv.Itoa(maxAddressingInt(plan.MaxNamedCount, 1)) + " 个；不得扩写或虚构其他昵称。"
			}
		} else {
			base = "称呼计划：本轮允许抽样称呼可播昵称；不得虚构昵称。"
		}
	case "MIXED":
		if selected != "" && groupLabel != "" {
			if plan.Optional {
				base = "称呼计划：本轮可先自然称呼“" + selected + "”，再用“" + groupLabel + "”带到其他人；不得逐个报完整名单。"
			} else {
				base = "称呼计划：本轮需要自然点名“" + selected + "”中的 1 个，可再用“" + groupLabel + "”带到其他人；不得逐个报完整名单。"
			}
		} else {
			base = "称呼计划：本轮允许点少量可播昵称后自然带到其他观众；不得逐个报完整名单，也不得虚构昵称。"
		}
	default:
		if selected != "" {
			if plan.Optional {
				base = "称呼计划：本轮只面向当前事件对应的单个观众；语境自然时可称呼“" + selected + "”最多 1 次，不得重复点名或虚构昵称。"
			} else {
				base = "称呼计划：本轮需要自然称呼观众“" + selected + "”1 次；不得重复点名或虚构昵称。"
			}
		} else if groupLabel != "" {
			if plan.Optional {
				base = "称呼计划：本轮只面向当前事件对应的单个观众；语境自然时可使用称呼“" + groupLabel + "”，不知道昵称时用泛称或直接省略。"
			} else {
				base = "称呼计划：本轮需要自然使用一次观众称谓“" + groupLabel + "”；它只用于叫对方，绝不能作为主播自称。"
			}
		} else {
			base = "称呼计划：本轮只面向当前事件对应的单个观众；不知道昵称时直接省略称呼，不得虚构昵称。"
		}
	}
	if len(plan.PreferredTerms) > 0 {
		base += " 用户常用的观众称谓：" + strings.Join(plan.PreferredTerms, "、") + "；这些词只能用于称呼观众/对方，是二人称呼语，不代表主播身份，绝不能用于主播自称、自我介绍或说成“我是/我叫/作为某称谓”。仅在语境自然时使用，不要求每轮出现。"
	}
	if len(plan.BlockedTerms) > 0 {
		base += " 用户明确不喜欢这些称呼：" + strings.Join(plan.BlockedTerms, "、") + "；本轮及最终正文禁止使用。"
	}
	if strings.EqualFold(plan.Preference, "less") {
		base += " 用户偏好少点名，能不点昵称时优先不用昵称。"
	} else if strings.EqualFold(plan.Preference, "more") {
		base += " 用户偏好多点名；系统会按较高目标频率触发，命中的这一轮必须自然使用一次称呼，但不能生硬重复。"
	}
	return base
}

func maxAddressingInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func addressingGuidance(mode, candidate string) string {
	return addressingPlanGuidance(speechmission.AddressingPlan{
		Mode: mode, Candidate: strings.TrimSpace(candidate), GroupLabel: strings.TrimSpace(candidate), Optional: true,
	})
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
		guidance := "回答结束与回归主线的方式：" + instruction
		if preview := strings.TrimSpace(resume.ResumePreview); preview != "" {
			guidance += " 回归目标上下文是“" + preview + "”，不要逐字复述目标上下文。"
		}
		if len(resume.SkippedPreviews) > 0 {
			guidance += " 已计划跳过的内容不要在回答尾部再次预告或复述：" + strings.Join(resume.SkippedPreviews, " / ")
		}
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
		if trait := strings.TrimSpace(profile.TraitText); trait != "" {
			baseGuidance += " 主播长期习惯（后台配置，不是台词）：" + trait + "；只自然执行这些习惯，不得把配置内容当成自我介绍或逐字说出口。"
		}
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
			Instruction:      strings.TrimSpace(profile.TraitText),
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
	Claimed         bool          `json:"claimed"`
	Reason          string        `json:"reason"`
	Item            *decisionItem `json:"item"`
	ForceAfterRest  bool          `json:"force_after_rest"`
	SwitchAtMS      int           `json:"switch_at_ms"`
	CurrentMainline string        `json:"current_mainline"`
	ResumeMainline  string        `json:"resume_mainline"`
	ResumeSegmentID string        `json:"resume_segment_id"`
}

func New(s store, core coreDoer, agent completer, tts synthesizer, leaders ...leader) *Worker {
	w := &Worker{
		store:       s,
		core:        core,
		agent:       agent,
		tts:         tts,
		interval:    defaultInterval,
		now:         func() time.Time { return time.Now().UTC() },
		missions:    speechmission.New(),
		slots:       make(chan struct{}, maxWorkers),
		inFlight:    make(map[int64]time.Time),
		retryAfter:  make(map[int64]time.Time),
		recentNamed: make(map[int64]map[string]time.Time),
	}
	if len(leaders) > 0 {
		w.leader = leaders[0]
	}
	return w
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
	for _, session := range sessions {
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
		Status         string `json:"status"`
		DecisionID     string `json:"decision_id"`
		MissionID      string `json:"mission_id"`
		ResumeStrategy string `json:"resume_strategy"`
		BridgeText     string `json:"bridge_text"`
		BridgeUsed     bool   `json:"bridge_used"`
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
	case "failed":
		w.transitionMission(item, speechmission.StateFailed, "playback_failed", "Core确认互动语音播放失败")
	}
}

func (w *Worker) processRoom(ctx context.Context, session model.LiveRuntimeSession) error {
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
	item.ForceAfterRest = claim.ForceAfterRest
	item.CurrentMainline = strings.TrimSpace(claim.CurrentMainline)
	item.ResumeMainline = strings.TrimSpace(claim.ResumeMainline)
	item.ResumeSegmentID = strings.TrimSpace(claim.ResumeSegmentID)
	w.ensureMission(session, item)
	w.transitionMission(item, speechmission.StatePlanningInteraction, "claimed", "口播任务已领取，开始汇总本轮策略黑板")
	completed := false
	defer func() {
		if !completed {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := w.release(releaseCtx, session, item.ID); err != nil {
				log.Printf("decision executor release tenant=%d room=%d decision=%s: %v", session.TenantID, session.RoomID, item.ID, err)
			}
		}
	}()

	manualOrigin := strings.ToLower(strings.TrimSpace(item.ManualOrigin))
	isSimulation := manualOrigin == "test_simulation" || manualOrigin == "agent_input_preview"
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
		if _, _, _, l3, layerErr := w.store.LoadLivePolicyLayers(ctx, session.TenantID, session.RoomID); layerErr == nil && l3 != nil {
			userLayerVersion = l3.VersionNo
		}
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
	ttsInstruction := w.ttsInstructionForMission(item, ttsProfile.Instruction)
	w.transitionMission(item, speechmission.StateSynthesizingTTS, "tts_start", "最终话术审核通过，开始生成语音")
	w.updateMission(item, func(m *speechmission.Mission) {
		m.TTS = speechmission.TTSDirective{
			Provider: voiceProvider(voice), Model: ttsModel, VoiceID: voice.VoiceID,
			Rate: voiceRate(voice), Instruction: ttsInstruction,
		}
	})
	ttsCtx, ttsCancel := context.WithTimeout(ctx, 35*time.Second)
	defer ttsCancel()
	audio, err := w.tts.SynthesizeURL(ttsCtx, ttsgateway.SynthesizeRequest{
		Provider:    voiceProvider(voice),
		Model:       ttsModel,
		VoiceID:     voice.VoiceID,
		Text:        text,
		Rate:        voiceRate(voice),
		Instruction: ttsInstruction,
	})
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
		session.TenantID, session.RoomID, item.ID, item.SelectedInterrupt, voiceRate(voice), strings.TrimSpace(ttsProfile.Instruction) != "", ttsModel,
	)

	action := "answer"
	if strings.EqualFold(strings.TrimSpace(item.ManualAction), "quick") {
		action = "quick"
	}
	w.transitionMission(item, speechmission.StateWaitingCutPoint, "dispatch_start", "TTS已生成，等待Core确认实际打断与回归位置")
	dispatchResult, err := w.dispatch(ctx, session, *item, action, text, audio.AudioURL)
	if err != nil {
		w.transitionMission(item, speechmission.StateFailed, "dispatch_failed", err.Error())
		return err
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
	if mission, ok := w.missionSnapshot(item); ok {
		w.recordUsedAddressingNames(session.RoomID, mission.Addressing.SelectedNames, text)
	}
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
	completed = true
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
	if item == nil {
		return "", fmt.Errorf("待执行策略为空")
	}
	text := ""
	if strings.EqualFold(strings.TrimSpace(item.ExecutionMode), "verbatim") {
		// 100%原话只表示不主动改写；规则层仍然拥有最终播出否决权。
		text = strings.TrimSpace(item.FixedText)
		if text == "" {
			text = strings.TrimSpace(primaryQuestion(*item))
		}
		if text == "" {
			return "", fmt.Errorf("100%%原话模式没有可播出的固定文字")
		}
	} else {
		seed := strings.TrimSpace(item.ID)
		if seed == "" {
			seed = strings.TrimSpace(primaryQuestion(*item))
		}
		w.applyStrategyPipeline(ctx, session, seed, item)
		w.transitionMission(item, speechmission.StateGeneratingText, "generate_text", "全部策略已汇总，开始一次性生成最终可播正文")
		prompt, err := w.answerPrompt(ctx, session, *item)
		if err != nil {
			return "", err
		}
		answerCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		answerSystemPrompt := w.store.AgentPromptValue(ctx, "live.answer.system", "你是直播口播合成器。根据事件、现场上下文和策略黑板，一次生成一段真实、自然、可直接播出的完整口播，不编造事实。") +
			"\n所有策略都只是生成约束，不允许逐项解释、逐段分别生成或输出多个候选。称呼只是可选约束，不自然时必须省略。称呼配置里的常用称谓只用于称呼观众/对方，绝不能当作主播自称、主播身份或自我介绍。主播状态只用于控制语速、句长、停顿、音色和情绪，绝不能在正文里说出“我不舒服/我咳嗽/我今天状态如何”之类状态说明。最终答案禁止输出任何内部标题、策略名称、概率、系统说明、字段名或分析过程。"
		answer, err := w.agent.Complete(answerCtx, agentgateway.Request{
			Messages: []agentgateway.Message{
				{Role: "system", Content: answerSystemPrompt},
				{Role: "user", Content: prompt},
			},
			MaxTokens:      650,
			EnableThinking: false,
			Timeout:        20 * time.Second,
		})
		if err != nil {
			return "", fmt.Errorf("生成回答失败: %w", err)
		}
		text = strings.TrimSpace(answer.Text)
		if text == "" {
			return "", fmt.Errorf("生成回答为空")
		}
	}
	w.transitionMission(item, speechmission.StateValidatingText, "validate_text", "最终正文已生成，进入事实、合规、记忆和内部信息终审")
	finalText, err := w.finalizeSpeechText(ctx, session, *item, text)
	if err != nil {
		return "", err
	}
	text = limitSpeechText(finalText, maxSpeechRunes)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("最终播出文字为空")
	}
	if strings.EqualFold(strings.TrimSpace(item.SelectedResume), "BRIDGE") {
		item.BridgeText = extractFinalSpeechSentence(text)
	} else {
		item.BridgeText = ""
	}
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
	question = strings.TrimSpace(question)
	if tenantID <= 0 || roomID <= 0 || question == "" {
		return SimulationOutput{}, fmt.Errorf("测试问题不能为空")
	}
	if w == nil || w.store == nil || w.agent == nil {
		return SimulationOutput{}, fmt.Errorf("测试智能体未初始化")
	}
	session := model.LiveRuntimeSession{TenantID: tenantID, RoomID: roomID}
	item := decisionItem{
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
	text, err := w.generateDecisionText(ctx, session, item)
	if err != nil {
		return SimulationOutput{}, err
	}
	result := SimulationOutput{Question: question, Reply: text, ExecutionMode: "intent"}
	if plan, planErr := w.store.GetLiveAgentPlanForRoom(ctx, tenantID, roomID); planErr == nil {
		result.PlanName = strings.TrimSpace(plan.Name)
	}
	if _, _, _, l3, layerErr := w.store.LoadLivePolicyLayers(ctx, tenantID, roomID); layerErr == nil && l3 != nil {
		result.UserLayerVersion = l3.VersionNo
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

func (w *Worker) release(ctx context.Context, session model.LiveRuntimeSession, decisionID string) error {
	query := tenantQuery(session.TenantID)
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
			"decision_id":           item.ID,
			"mission_id":            missionID(&item),
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
			"force_after_rest":      item.ForceAfterRest,
		},
	)
	if err != nil {
		return dispatchResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return dispatchResult{}, fmt.Errorf("audio dispatch http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var result dispatchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return dispatchResult{}, fmt.Errorf("decode audio dispatch result: %w", err)
	}
	return result, nil
}

func (w *Worker) readyVoice(ctx context.Context, tenantID, roomID int64) (model.VoiceProfile, bool, error) {
	// The published live-agent version is the immutable runtime contract.
	// Its pre-generated mainline audio and realtime interaction audio must
	// use exactly the same voice identity.
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
			},
		}, true, nil
	} else if !errors.Is(publishedErr, appdb.ErrLiveAgentPlanVersionNotFound) {
		return model.VoiceProfile{}, false, publishedErr
	}

	versions, err := w.store.ListLiveAgentConfigVersions(ctx, tenantID)
	if err != nil {
		return model.VoiceProfile{}, false, err
	}
	selected := selectedRoomVoice(versions, roomID)
	if len(selected) > 0 {
		source := strings.ToLower(strings.TrimSpace(fmt.Sprint(selected["source"])))
		if source == "clone" {
			if profileID := mapInt64(selected["profile_id"]); profileID > 0 {
				profile, profileErr := w.store.GetVoiceProfile(ctx, tenantID, profileID)
				if profileErr == nil && strings.EqualFold(strings.TrimSpace(profile.CloneStatus), "ready") && strings.TrimSpace(profile.VoiceID) != "" {
					return profile, true, nil
				}
			}
		}
		if source == "official" {
			if voice, exists := voicecatalog.Find(strings.TrimSpace(fmt.Sprint(selected["voice_id"]))); exists {
				return officialVoiceProfile(voice), true, nil
			}
		}
	}
	voice := voicecatalog.Default()
	if strings.TrimSpace(voice.ID) == "" || strings.TrimSpace(voice.Model) == "" {
		return model.VoiceProfile{}, false, nil
	}
	return officialVoiceProfile(voice), true, nil
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
	return model.VoiceProfile{
		Name:        voice.Name,
		Provider:    "aliyun_qwen",
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

var unsupportedClaimPhrases = []string{
	"正规渠道",
	"品质保障",
	"严格筛选",
	"官方正品",
	"绝对正品",
}

var unsupportedShippingClaimPhrases = []string{
	"合作的主流快递",
	"主流快递",
	"系统匹配",
	"仓库实际发货",
	"48小时内",
	"四十八小时内",
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

func (w *Worker) finalizeSpeechText(
	ctx context.Context,
	session model.LiveRuntimeSession,
	item decisionItem,
	text string,
) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("最终播出文字为空")
	}

	industry, l1, l2, l3, err := w.store.LoadLivePolicyLayers(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return "", fmt.Errorf("规则层终审读取失败: %w", err)
	}
	effective := policy.BuildEffective(industry, l1, l2, l3)
	runtimeInstruction := w.store.AgentPromptValue(ctx, "policy.runtime.execution", "")
	if strings.TrimSpace(runtimeInstruction) != "" {
		effective.PromptText = strings.TrimSpace(runtimeInstruction + "\n\n" + effective.PromptText)
	}
	contextText, _ := w.answerPrompt(ctx, session, item)
	riskTerms, reasons := finalSpeechRisks(text, contextText, effective)
	for _, term := range internalSpeechLeakTerms {
		if term != "" && strings.Contains(text, term) {
			reasons = append(reasons, "内部生成控制信息不得进入播出文本")
			break
		}
	}
	for _, term := range memoryWordingRiskTerms(contextText) {
		if term != "" && strings.Contains(text, term) {
			riskTerms = append(riskTerms, term)
			reasons = append(reasons, "当前直播间用词规范禁止表达："+term)
		}
	}
	if mission, ok := w.missionSnapshot(&item); ok {
		for _, violation := range presentationControlViolations(mission, text) {
			reasons = append(reasons, violation)
		}
	}
	riskTerms = uniqueNonEmptyStrings(riskTerms)
	reasons = uniqueNonEmptyStrings(reasons)
	hasAgentMemories := hasAgentMemoryPrompt(contextText)
	manualOrigin := strings.ToLower(strings.TrimSpace(item.ManualOrigin))
	forceOperatorReview := manualOrigin == "agent_input" || manualOrigin == "agent_input_preview"
	if forceOperatorReview {
		reasons = append(reasons, "操作者上行文字必须经过播出前审核")
		reasons = uniqueNonEmptyStrings(reasons)
	}
	if len(reasons) == 0 && !hasAgentMemories && !forceOperatorReview {
		return text, nil
	}

	reviewCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	reviewSystemPrompt := w.store.AgentPromptValue(ctx, "live.final_review.system", "只返回修正后的可直接播出正文，删除无依据事实和绝对化承诺。")
	if hasAgentMemories {
		reviewSystemPrompt += "\n\n【智能体记忆终审硬要求】\n当前直播间智能体记忆都是用户已经采用、正在生效的约束。必须逐条核对与当前问题相关的记忆，不能遗漏：\n- semantic：必须结合当前问题、商品和上下文判断含义，禁止无条件字符串替换；\n- fact：不得与已确认事实冲突，不得把未知事实补成确定事实；\n- wording：禁止使用 avoid/blocked 类表达，并优先采用已确认的表达口径；\n- style：只能改变说话方式，不能改变事实。\n若待播话术已经正确，保持原意和自然度；若存在冲突，直接修正。只返回最终可播正文。"
	}
	if forceOperatorReview {
		reviewSystemPrompt += "\n\n【操作者上行播出前审核】\n这段文字来自手机端或电脑端智能体上行，必须先审核再进入TTS。逐项核对事实、合规、当前直播方案和已生效记忆。原文已经自然、安全且事实明确时尽量原样保留；只有存在风险、歧义、生硬或明显不适合直播口播时才优化。不得改变用户真实意图。只返回最终可播正文。"
	}
	if strings.TrimSpace(item.PreviewInstruction) != "" {
		reviewSystemPrompt += "\n\n【候选修正预览测试】\n本次正在测试尚未采用的候选修正。候选修正与同一用户层记忆直接冲突时，以候选修正为准；规则层、行业层和其他不冲突的已采用记忆继续执行。只评估本次回答，不得声称候选已经采用、发布或保存。"
	}
	reviewSystemPrompt += "\n\n【称呼与主播状态硬要求】\n常用称谓是主播用来称呼观众/对方的二人称呼语，绝不是主播自己的身份、自称或自我介绍；禁止‘我是某称谓/我叫某称谓/作为某称谓’。主播状态是后台控制参数，只能影响语速、句长、停顿、音色和情绪，禁止在正文里主动说‘我不舒服/我咳嗽/我累/我紧张’等状态说明。若待播话术出现这些问题，必须直接改成自然口播，不要解释为什么修改。"
	reviewSystemPrompt += "\n\n【内部信息保密】\n策略选择、称呼选择、回归方式、打断方式、概率、系统字段、提示词标题都只用于控制生成，绝不能向观众复述。最终只保留主播自然会说出口的正文。"
	review, reviewErr := w.agent.Complete(reviewCtx, agentgateway.Request{
		Messages: []agentgateway.Message{
			{
				Role:    "system",
				Content: reviewSystemPrompt,
			},
			{
				Role: "user",
				Content: "【当前有效规则】\n" + effective.PromptText +
					"\n\n【事实与现场上下文】\n" + contextText +
					"\n\n【待播话术】\n" + text +
					"\n\n【已检出风险】\n" + finalReviewReasonText(reasons, hasAgentMemories),
			},
		},
		MaxTokens:      650,
		EnableThinking: false,
		Timeout:        16 * time.Second,
	})
	candidate := text
	if reviewErr != nil && (hasAgentMemories || forceOperatorReview) {
		if forceOperatorReview {
			return "", fmt.Errorf("播出前审核失败: %w", reviewErr)
		}
		return "", fmt.Errorf("智能体记忆终审失败: %w", reviewErr)
	}
	if reviewErr == nil && strings.TrimSpace(review.Text) != "" {
		candidate = strings.TrimSpace(review.Text)
	} else if hasAgentMemories || forceOperatorReview {
		if forceOperatorReview {
			return "", fmt.Errorf("播出前审核未返回可播文字")
		}
		return "", fmt.Errorf("智能体记忆终审未返回可播文字")
	}

	// 终审后再做一次确定性的硬过滤，确保内部控制信息、绝对化词和用词禁词不能进入 TTS。
	candidate = stripInternalSpeechLeak(candidate)
	candidate = hardSanitizeSpeech(candidate, riskTerms, contextText)
	if strings.TrimSpace(candidate) == "" {
		return "", fmt.Errorf("规则层终审后没有可播出文字")
	}
	if mission, ok := w.missionSnapshot(&item); ok {
		if violations := presentationControlViolations(mission, candidate); len(violations) > 0 {
			return "", fmt.Errorf("称呼或主播状态终审未通过: %s", strings.Join(violations, "；"))
		}
	}
	return strings.TrimSpace(candidate), nil
}

func presentationControlViolations(mission speechmission.Mission, text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	violations := make([]string, 0, 4)
	for _, raw := range mission.Addressing.PreferredTerms {
		term := strings.TrimSpace(raw)
		if term == "" {
			continue
		}
		patterns := []string{
			"我是" + term,
			"我叫" + term,
			"作为" + term,
			"我这个" + term,
			"我就是" + term,
		}
		for _, pattern := range patterns {
			if strings.Contains(text, pattern) {
				violations = append(violations, "观众称谓“"+term+"”被错误用作主播自称")
				break
			}
		}
	}

	state := strings.TrimSpace(mission.HumanStyle.State.HostState)
	if state != "" {
		if len([]rune(state)) >= 4 && strings.Contains(text, state) {
			violations = append(violations, "后台主播状态被直接复述进正文")
		}
		stateTerms := []string{"咳嗽", "感冒", "不舒服", "嗓子不舒服", "喉咙不舒服", "疲惫", "很累", "累了", "紧张", "焦虑"}
		selfPrefixes := []string{"我", "我有点", "我今天", "今天我", "主播我"}
		for _, term := range stateTerms {
			if !strings.Contains(state, term) {
				continue
			}
			for _, prefix := range selfPrefixes {
				if strings.Contains(text, prefix+term) {
					violations = append(violations, "后台主播状态“"+term+"”被说成了直播正文")
					break
				}
			}
		}
	}
	return uniqueNonEmptyStrings(violations)
}

func stripInternalSpeechLeak(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}
	for _, term := range internalSpeechLeakTerms {
		if strings.HasPrefix(term, "【") {
			text = strings.ReplaceAll(text, term, "")
		}
	}
	lines := strings.Split(text, "\n")
	filtered := lines[:0]
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(trimmed, "策略名称：") ||
			strings.Contains(trimmed, "系统提示：") ||
			strings.Contains(trimmed, "隐藏生成条件") ||
			strings.Contains(lower, "resume_strategy") ||
			strings.Contains(lower, "interrupt_strategy") ||
			strings.Contains(lower, "addressing_mode") {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}

func hasAgentMemoryPrompt(contextText string) bool {
	return strings.Contains(contextText, "【当前直播间智能体记忆】")
}

func finalReviewReasonText(reasons []string, hasAgentMemories bool) string {
	if len(reasons) > 0 {
		return strings.Join(reasons, "；")
	}
	if hasAgentMemories {
		return "未检出显式关键词风险；仍必须逐条核对当前直播间已采用的智能体记忆，确认无遗漏后再输出。"
	}
	return "无"
}

func finalSpeechRisks(
	text string,
	contextText string,
	effective model.LiveEffectivePolicy,
) ([]string, []string) {
	terms := append([]string(nil), builtInAbsoluteRiskTerms...)
	terms = append(terms, configuredL1RiskTerms(effective)...)
	terms = uniqueNonEmptyStrings(terms)

	reasons := make([]string, 0)
	for _, term := range terms {
		if term != "" && strings.Contains(text, term) {
			reasons = append(reasons, "绝对化或规则层高风险表达："+term)
		}
	}
	for _, phrase := range unsupportedClaimPhrases {
		if !strings.Contains(text, phrase) {
			continue
		}
		if strings.Contains(contextText, phrase) {
			continue
		}
		reasons = append(reasons, "缺少事实依据的承诺或事实："+phrase)
		terms = append(terms, phrase)
	}
	for _, phrase := range unsupportedShippingClaimPhrases {
		if !strings.Contains(text, phrase) {
			continue
		}
		if strings.Contains(contextText, phrase) {
			continue
		}
		reasons = append(reasons, "直播方案/策略中没有依据的物流事实："+phrase)
		terms = append(terms, phrase)
	}
	return uniqueNonEmptyStrings(terms), uniqueNonEmptyStrings(reasons)
}

func memoryWordingRiskTerms(contextText string) []string {
	const marker = "【用词硬校验词】"
	result := make([]string, 0)
	for _, line := range strings.Split(contextText, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, marker) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, marker))
		for _, term := range strings.FieldsFunc(value, func(r rune) bool {
			return r == '｜' || r == '|' || r == ',' || r == '，' || r == '、' || r == ';' || r == '；'
		}) {
			if term = strings.TrimSpace(term); term != "" {
				result = append(result, term)
			}
		}
	}
	return uniqueNonEmptyStrings(result)
}

func configuredL1RiskTerms(effective model.LiveEffectivePolicy) []string {
	result := make([]string, 0)
	for _, rule := range effective.Rules {
		if rule.SourceLayer != model.LivePolicyLayerL1 {
			continue
		}
		for _, key := range []string{"forbidden_terms", "blocked_terms", "risk_terms", "absolute_terms"} {
			value, ok := rule.Metadata[key]
			if !ok {
				continue
			}
			switch typed := value.(type) {
			case []string:
				result = append(result, typed...)
			case []any:
				for _, item := range typed {
					if term := strings.TrimSpace(fmt.Sprint(item)); term != "" {
						result = append(result, term)
					}
				}
			case string:
				for _, term := range strings.FieldsFunc(typed, func(r rune) bool {
					return r == ',' || r == '，' || r == '、' || r == ';' || r == '；'
				}) {
					if term = strings.TrimSpace(term); term != "" {
						result = append(result, term)
					}
				}
			}
		}
		ruleText := strings.TrimSpace(rule.Text)
		if ruleText == "" {
			continue
		}
		if !strings.Contains(ruleText, "避免") &&
			!strings.Contains(ruleText, "不得") &&
			!strings.Contains(ruleText, "不要") &&
			!strings.Contains(ruleText, "禁止") &&
			!strings.Contains(ruleText, "绝对") &&
			!strings.Contains(ruleText, "极限词") {
			continue
		}
		for _, match := range quotedRiskTermPattern.FindAllStringSubmatch(ruleText, -1) {
			if len(match) > 1 {
				if term := strings.TrimSpace(match[1]); term != "" {
					result = append(result, term)
				}
			}
		}
	}
	return uniqueNonEmptyStrings(result)
}

func hardSanitizeSpeech(text string, riskTerms []string, contextText string) string {
	text = strings.TrimSpace(text)
	replacements := []struct {
		old string
		new string
	}{
		{"绝对不踩雷", "更值得放心看看"},
		{"绝对不会", "一般不会"},
		{"绝对没有", "目前没有发现"},
		{"百分百", "尽量做到"},
		{"100%", "尽量做到"},
		{"零风险", "尽量降低风险"},
		{"全网最低", "直播间当前价格"},
		{"全网第一", "很受大家关注"},
		{"第一名", "表现很不错"},
		{"包你满意", "希望能让你满意"},
		{"包你好吃", "喜欢这口的可以试试"},
		{"保证", "尽量做到"},
		{"肯定", "更可能"},
		{"永远", "长期"},
		{"绝对", ""},
	}
	for _, item := range replacements {
		text = strings.ReplaceAll(text, item.old, item.new)
	}
	for _, phrase := range unsupportedClaimPhrases {
		if strings.Contains(contextText, phrase) {
			continue
		}
		switch phrase {
		case "正规渠道":
			text = strings.ReplaceAll(text, phrase, "直播间里给大家介绍的")
		case "品质保障":
			text = strings.ReplaceAll(text, phrase, "选品会认真把关")
		case "严格筛选":
			text = strings.ReplaceAll(text, phrase, "认真挑选")
		case "官方正品", "绝对正品":
			text = strings.ReplaceAll(text, phrase, "页面展示的商品")
		}
	}
	for _, term := range riskTerms {
		term = strings.TrimSpace(term)
		if term == "" || !strings.Contains(text, term) {
			continue
		}
		text = strings.ReplaceAll(text, term, "")
	}
	for strings.Contains(text, "，，") {
		text = strings.ReplaceAll(text, "，，", "，")
	}
	for strings.Contains(text, "。。") {
		text = strings.ReplaceAll(text, "。。", "。")
	}
	return strings.TrimSpace(strings.Trim(text, "，,。 "))
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

func liveAgentPlanPromptContext(plan model.LiveAgentPlan) string {
	if plan.ID <= 0 {
		return "\n【当前智能体直播方案】\n当前直播间未配置可用直播方案。遇到物流、价格、库存、发货时效等事实问题，不得按行业常识猜测；没有事实依据时只做不确定表达。"
	}

	var builder strings.Builder
	builder.WriteString("\n【当前智能体直播方案：事实与回答口径，优先执行】")
	builder.WriteString("\n方案名称：")
	builder.WriteString(strings.TrimSpace(plan.Name))
	if description := strings.TrimSpace(plan.Description); description != "" {
		builder.WriteString("\n方案说明：")
		builder.WriteString(description)
	}
	builder.WriteString("\n执行要求：观众问题如果能从本方案得到明确答案，必须直接按本方案的事实和口径回答；不得把明确方案改写成‘默认/一般/通常/主流快递/系统匹配’之类通用兜底，也不得增加方案没有写明的快递公司、发货时效、仓库流程、价格、库存或承诺。")

	termCount := 0
	for _, term := range plan.Terms {
		if termCount >= 30 || !strings.EqualFold(strings.TrimSpace(term.Status), "active") {
			continue
		}
		canonical := strings.TrimSpace(term.CanonicalText)
		note := strings.TrimSpace(term.Note)
		if canonical == "" && note == "" {
			continue
		}
		termCount++
		builder.WriteString("\n- 方案条目")
		if termType := strings.TrimSpace(term.TermType); termType != "" {
			builder.WriteString("[")
			builder.WriteString(termType)
			builder.WriteString("]")
		}
		builder.WriteString("：")
		builder.WriteString(canonical)
		if note != "" {
			builder.WriteString("；说明：")
			builder.WriteString(note)
		}
		variants := make([]string, 0, 3)
		for _, variant := range term.Variants {
			if len(variants) >= 3 {
				break
			}
			if value := strings.TrimSpace(variant.VariantText); value != "" {
				variants = append(variants, value)
			}
		}
		if len(variants) > 0 {
			builder.WriteString("；已确认表达：")
			builder.WriteString(strings.Join(variants, "、"))
		}
	}
	return builder.String()
}

func liveAgentPlanFactsPrompt(facts []model.LiveAgentPlanFact) string {
	if len(facts) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("\n【当前方案动态事实：实时热更新】")
	builder.WriteString("\n这些事实是当前方案刚刚生效的正式事实，回答相关问题时优先于旧主线稿中的过时表述；不得虚构未列出的事实。")
	count := 0
	for _, fact := range facts {
		if count >= 80 || !strings.EqualFold(strings.TrimSpace(fact.Status), "active") {
			continue
		}
		key := strings.TrimSpace(fact.Key)
		value := strings.TrimSpace(fact.Value)
		if key == "" || value == "" {
			continue
		}
		count++
		builder.WriteString("\n- ")
		if category := strings.TrimSpace(fact.Category); category != "" {
			builder.WriteString("[")
			builder.WriteString(category)
			builder.WriteString("] ")
		}
		builder.WriteString(key)
		builder.WriteString("：")
		builder.WriteString(value)
		if fact.VersionNo > 0 {
			builder.WriteString("（V")
			builder.WriteString(strconv.FormatInt(fact.VersionNo, 10))
			builder.WriteString("）")
		}
	}
	if count == 0 {
		return ""
	}
	return builder.String()
}

func liveAgentPlanStylePrompt(scripts []model.LiveAgentPlanScript) string {
	for _, script := range scripts {
		if !strings.EqualFold(strings.TrimSpace(script.Status), "active") ||
			!strings.EqualFold(strings.TrimSpace(script.AnalysisStatus), "analyzed") {
			continue
		}
		profile := script.Analysis.AnchorStyle
		if len(profile.Dimensions) == 0 && len(profile.ReusableRules) == 0 && strings.TrimSpace(profile.Summary) == "" {
			continue
		}
		var builder strings.Builder
		builder.WriteString("\n【当前主播风格：实时热更新】")
		builder.WriteString("\n这里只控制怎么说，绝不能改变事实、价格、规格、库存、活动、物流或承诺。")
		if summary := strings.TrimSpace(profile.Summary); summary != "" {
			builder.WriteString("\n风格摘要：")
			builder.WriteString(summary)
		}
		dimensionCount := 0
		for _, dimension := range profile.Dimensions {
			if dimensionCount >= 24 {
				break
			}
			rule := strings.TrimSpace(dimension.Rule)
			label := strings.TrimSpace(dimension.Label)
			if rule == "" && label == "" {
				continue
			}
			dimensionCount++
			builder.WriteString("\n- ")
			if label != "" {
				builder.WriteString(label)
				builder.WriteString("：")
			}
			builder.WriteString(rule)
		}
		for index, rule := range profile.ReusableRules {
			if index >= 12 {
				break
			}
			rule = strings.TrimSpace(rule)
			if rule != "" {
				builder.WriteString("\n- 可复用表达：")
				builder.WriteString(rule)
			}
		}
		return builder.String()
	}
	return ""
}

func (w *Worker) answerPrompt(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) (string, error) {
	plan, planErr := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if planErr != nil && !errors.Is(planErr, appdb.ErrLiveAgentPlanNotFound) {
		return "", fmt.Errorf("读取智能体直播方案失败: %w", planErr)
	}
	dynamicFactsPrompt := ""
	dynamicStylePrompt := ""
	if plan.ID > 0 {
		facts, factsErr := w.store.ListLiveAgentPlanFacts(ctx, session.TenantID, plan.ID)
		if factsErr != nil {
			return "", fmt.Errorf("读取直播方案动态事实失败: %w", factsErr)
		}
		dynamicFactsPrompt = liveAgentPlanFactsPrompt(facts)
		scripts, scriptsErr := w.store.ListLiveAgentPlanScripts(ctx, session.TenantID, plan.ID)
		if scriptsErr != nil {
			return "", fmt.Errorf("读取直播方案主播风格失败: %w", scriptsErr)
		}
		dynamicStylePrompt = liveAgentPlanStylePrompt(scripts)
	}
	industry, l1, l2, l3, err := w.store.LoadLivePolicyLayers(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return "", fmt.Errorf("读取直播策略失败: %w", err)
	}
	effective := policy.BuildEffective(industry, l1, l2, l3)
	runtimeInstruction := w.store.AgentPromptValue(ctx, "policy.runtime.execution", "")
	if strings.TrimSpace(runtimeInstruction) != "" {
		effective.PromptText = strings.TrimSpace(runtimeInstruction + "\n\n" + effective.PromptText)
	}
	memories, memoryErr := w.store.ListActiveAgentMemories(ctx, session.TenantID, session.RoomID)
	if memoryErr != nil {
		return "", fmt.Errorf("读取智能体记忆失败: %w", memoryErr)
	}
	if strings.TrimSpace(item.PreviewInstruction) != "" {
		filtered := memories[:0]
		for _, memory := range memories {
			if item.PreviewMatchedMemoryID > 0 && memory.ID == item.PreviewMatchedMemoryID {
				continue
			}
			if item.PreviewMemoryKey != "" && strings.EqualFold(strings.TrimSpace(memory.MemoryKey), strings.TrimSpace(item.PreviewMemoryKey)) &&
				(item.PreviewMemoryType == "" || strings.EqualFold(strings.TrimSpace(memory.MemoryType), strings.TrimSpace(item.PreviewMemoryType))) {
				continue
			}
			filtered = append(filtered, memory)
		}
		memories = filtered
	}
	memoryPrompt := agentmemory.Prompt(memories)
	rulesJSON, _ := json.Marshal(effective.Rules)
	planContext := liveAgentPlanPromptContext(plan)
	strategyGuidance := hiddenStrategyPrompt(item)
	missionContext := speechMissionPrompt(item)
	lengthGuidance := adaptiveAnswerLengthGuidance(item)
	continuityGuidance := ""
	if strings.TrimSpace(item.ResumeMainline) != "" {
		continuityGuidance = "\n【主线衔接硬要求】\n本次回答会在完整句末切入。切点前主线正在讲：" +
			trimContextRunes(item.CurrentMainline, 120) +
			"\n回答声音结束后，系统会直接从下一句主线恢复：" + trimContextRunes(item.ResumeMainline, 140) +
			"\n你的回答最后一句必须让后面的主线句听起来像自然接着说；不要提前照念或重复下一句主线，不要机械固定说“我们继续”，不要改变下一句的事实含义。"
	}
	questions := item.SampleQuestions
	if len(questions) > 8 {
		questions = questions[:8]
	}
	if len(questions) == 0 {
		questions = []string{primaryQuestion(item)}
	}
	variables := map[string]string{
		"question":             strings.Join(questions, "；"),
		"room_name":            fmt.Sprintf("room-%d", session.RoomID),
		"plan_name":            strings.TrimSpace(plan.Name),
		"industry_policy":      industry,
		"user_policy":          string(rulesJSON),
		"reference_answer":     strings.TrimSpace(item.ReplyHint),
		"conversation_history": strings.TrimSpace(item.Summary),
	}
	manualOrigin := strings.ToLower(strings.TrimSpace(item.ManualOrigin))
	if manualOrigin == "agent_input" || manualOrigin == "agent_input_preview" {
		requirement := w.store.RenderAgentPrompt(ctx, "live.answer.operator", "按操作者指令生成可直接播出的口播正文。", variables)
		if manualOrigin == "agent_input_preview" {
			requirement = strings.TrimSpace(requirement + "\n这是播出前预生成审核：先核对事实、合规和当前直播方案。原输入已经自然且安全时尽量保持原意和语气；只有确有必要时才优化措辞。只返回最终建议播出的正文，本步骤不触发TTS。")
		}
		return planContext + dynamicFactsPrompt + dynamicStylePrompt + missionContext + strategyGuidance +
			"\n当前直播策略规则：" + string(rulesJSON) +
			"\n当前直播间智能体记忆：" + memoryPrompt +
			"\n操作者指令：" + strings.Join(questions, "；") +
			"\n任务摘要：" + strings.TrimSpace(item.Summary) +
			"\n额外要求：" + strings.TrimSpace(item.ReplyHint) +
			"\n口播长度要求：" + lengthGuidance +
			continuityGuidance +
			"\n当前场景生成要求：" + requirement, nil
	}
	requirement := w.store.RenderAgentPrompt(ctx, "live.answer.audience", "根据当前口播任务、现场主线和全部策略约束，一次生成最终可播正文。", variables)
	previewPrompt := ""
	if strings.EqualFold(strings.TrimSpace(item.ManualOrigin), "test_simulation") {
		testRequirement := w.store.RenderAgentPrompt(ctx, "test.simulation.answer", "测试模式只返回模拟回答，不执行真实播音。", variables)
		requirement = strings.TrimSpace(requirement + "\n" + testRequirement)
		if preview := strings.TrimSpace(item.PreviewInstruction); preview != "" {
			previewPrompt = "\n【本次候选修正预览】\n" + preview +
				"\n这是尚未采用的候选修正，仅本次测试临时生效。若它与同一用户记忆发生直接冲突，以本候选为准；其他不冲突的已采用记忆继续执行。不得声称已经采用或保存。"
		}
	}
	inputLabel := "观众原话"
	if strings.TrimSpace(item.MissionKind) != "" {
		inputLabel = "事件输入"
	}
	return planContext + dynamicFactsPrompt + dynamicStylePrompt + missionContext + strategyGuidance +
		"\n当前直播策略规则：" + string(rulesJSON) +
		"\n当前直播间智能体记忆：" + memoryPrompt + previewPrompt +
		"\n当前问题类别：" + strings.TrimSpace(item.Title) +
		"\n" + inputLabel + "：" + strings.Join(questions, "；") +
		"\n任务摘要：" + strings.TrimSpace(item.Summary) +
		"\n回答提示：" + strings.TrimSpace(item.ReplyHint) +
		"\n口播长度要求：" + lengthGuidance +
		continuityGuidance +
		"\n当前场景生成要求：" + requirement, nil
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

func adaptiveAnswerLengthGuidance(item decisionItem) string {
	questions := make([]string, 0, len(item.SampleQuestions))
	for _, question := range item.SampleQuestions {
		if value := strings.TrimSpace(question); value != "" {
			questions = append(questions, value)
		}
	}
	if len(questions) == 0 {
		if value := strings.TrimSpace(primaryQuestion(item)); value != "" {
			questions = append(questions, value)
		}
	}
	totalRunes := len([]rune(strings.Join(questions, "；")))
	recommended := "40–140字"
	switch {
	case len(questions) >= 3 || totalRunes >= 80:
		recommended = "80–220字"
	case totalRunes <= 14:
		recommended = "20–80字"
	case totalRunes <= 35:
		recommended = "30–120字"
	case totalRunes >= 55:
		recommended = "60–180字"
	}
	return "300字是硬上限，不是目标字数。请根据当前问题复杂度、是否需要解释以及尽快回到主线的节奏自行决定长度；通常不少于20字。能一句讲清就短答，禁止为了凑字重复扩写。本次建议 " + recommended + "，确有必要可延长，但绝不能超过300字。"
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
