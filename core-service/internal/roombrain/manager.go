package roombrain

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"livecompanion/core/internal/director"
	"livecompanion/core/internal/humanization"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/resume"
	"livecompanion/core/internal/roomintel"
	"livecompanion/core/internal/timeline"
)

type Classification struct {
	Topic    string
	Question bool
	Negative bool
}

type TopicClassifier interface {
	Classify(text string) Classification
}

type HeuristicClassifier struct{}

func (HeuristicClassifier) Classify(text string) Classification {
	value := strings.TrimSpace(text)
	lower := strings.ToLower(value)
	question := strings.ContainsAny(value, "?？") || containsAny(lower,
		"多少", "怎么", "什么", "哪里", "哪儿", "哪个地方", "什么地方", "地址", "位置",
		"几斤", "多大", "能不能", "有没有", "吗", "呢", "为啥", "为什么", "多久", "几天")
	result := Classification{
		Question: question,
		Negative: containsAny(lower,
			"骗人", "假的", "太贵", "坑", "垃圾", "不好", "投诉", "退货", "退款", "破损", "坏了", "没发货", "不发货"),
	}
	if question {
		result.Topic = roomintel.QuestionTopic(value)
	}
	return result
}

func IsOrderSignalText(text string) bool {
	value := strings.ToLower(strings.TrimSpace(text))
	if value == "" {
		return false
	}
	if strings.ContainsAny(value, "?？") || containsAny(value,
		"怎么买", "怎么拍", "怎么下单", "如何买", "如何拍", "拍哪个", "哪个链接", "能买吗", "能不能买", "哪里买") {
		return false
	}
	return containsAny(value,
		"已下单", "已经下单", "下单了", "我下单", "已拍", "已经拍", "拍了", "拍好了", "拍下了", "我拍了",
		"买了", "我买了", "买好了", "已购买", "已经购买", "购买了")
}

func containsAny(value string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(value, part) {
			return true
		}
	}
	return false
}

type Manager struct {
	mu             sync.Mutex
	rooms          map[int64]*roomState
	classifier     TopicClassifier
	planner        *director.Planner
	now            func() time.Time
	answerCapacity float64
}

type roomState struct {
	mu sync.Mutex

	intel    *roomintel.Pool
	timeline *timeline.Session

	style         director.StyleProfile
	resumeContext resume.Context

	lastDebtEval time.Time
}

func NewManager() *Manager {
	return &Manager{
		rooms:          map[int64]*roomState{},
		classifier:     HeuristicClassifier{},
		planner:        director.NewDefaultPlanner(),
		now:            func() time.Time { return time.Now().UTC() },
		answerCapacity: 6,
	}
}

func NewManagerWith(classifier TopicClassifier, planner *director.Planner) *Manager {
	m := NewManager()
	if classifier != nil {
		m.classifier = classifier
	}
	if planner != nil {
		m.planner = planner
	}
	return m
}

func (m *Manager) Classify(text string) Classification {
	if m == nil || m.classifier == nil {
		return Classification{}
	}
	return m.classifier.Classify(text)
}

func (m *Manager) room(roomID int64, startedAt time.Time) *roomState {
	if roomID <= 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.rooms[roomID]
	if state != nil {
		return state
	}
	if startedAt.IsZero() {
		startedAt = m.now()
	}
	state = &roomState{
		intel:    roomintel.NewPool(),
		timeline: timeline.NewSession(startedAt),
		style: director.StyleProfile{
			Name:              "default-live-anchor",
			Tone:              "自然、接地气",
			SentenceRhythm:    "长短句交替",
			CTAStyle:          "轻量提醒，不机械喊口号",
			HumorCeiling:      1,
			DefaultTargetSecs: 10,
		},
		resumeContext: resume.Context{MainlineStillValid: true},
	}
	m.rooms[roomID] = state
	return state
}

func (m *Manager) Ingest(event model.RoomEvent) {
	eventType := strings.ToLower(strings.TrimSpace(event.EventType))
	if eventType == "session_start" {
		m.mu.Lock()
		delete(m.rooms, event.RoomID)
		m.mu.Unlock()
		_ = m.room(event.RoomID, event.OccurredAt)
		return
	}
	if eventType == "session_end" {
		state := m.room(event.RoomID, event.OccurredAt)
		if state != nil {
			state.mu.Lock()
			state.timeline.Append(timeline.Pin{
				At:       event.OccurredAt,
				Kind:     timeline.PinStrategy,
				Strategy: "session.end",
				Key:      "session_end",
			})
			state.mu.Unlock()
		}
		return
	}
	state := m.room(event.RoomID, event.OccurredAt)
	if state == nil {
		return
	}
	intelEvent, ok := m.toIntelEvent(event)
	if !ok {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.intel.Add(intelEvent)
	m.evaluateDebtsLocked(state, event.OccurredAt)
}

func (m *Manager) toIntelEvent(event model.RoomEvent) (roomintel.Event, bool) {
	at := event.OccurredAt
	if at.IsZero() {
		at = m.now()
	}
	out := roomintel.Event{
		EventID:    event.ID,
		UserID:     event.UserID,
		Nickname:   event.Nickname,
		Content:    event.Content,
		Count:      1,
		OccurredAt: at,
	}
	payload := decodePayload(event.Payload)

	switch strings.ToLower(strings.TrimSpace(event.EventType)) {
	case "member":
		out.Type = roomintel.EventEnter
		out.Online = intValue(payload, "member_count")
	case "chat", "comment":
		out.Type = roomintel.EventChat
		classification := m.classifier.Classify(event.Content)
		out.Topic = classification.Topic
		out.Question = classification.Question
		out.Negative = classification.Negative
	case "like":
		out.Type = roomintel.EventLike
		out.Count = int64Value(payload, "count")
		if out.Count <= 0 {
			out.Count = 1
		}
	case "follow":
		out.Type = roomintel.EventFollow
	case "order":
		out.Type = roomintel.EventOrder
		out.Count = int64Value(payload, "count")
		if out.Count <= 0 {
			out.Count = 1
		}
	case "order_signal":
		out.Type = roomintel.EventOrderSignal
	case "gift":
		out.Type = roomintel.EventGift
	case "room":
		out.Type = roomintel.EventRoom
		out.Online = intValue(payload, "online_count")
	default:
		return roomintel.Event{}, false
	}
	return out, true
}

func decodePayload(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil
	}
	return result
}

func intValue(values map[string]any, key string) int {
	value := int64Value(values, key)
	if value > math.MaxInt {
		return math.MaxInt
	}
	return int(value)
}

func int64Value(values map[string]any, key string) int64 {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case float64:
		return int64(value)
	case int:
		return int64(value)
	case int64:
		return value
	case json.Number:
		n, _ := value.Int64()
		return n
	}
	return 0
}

func (m *Manager) evaluateDebtsLocked(state *roomState, now time.Time) {
	if now.IsZero() {
		now = m.now()
	}
	if !state.lastDebtEval.IsZero() && now.Sub(state.lastDebtEval) < 10*time.Second {
		return
	}
	state.lastDebtEval = now
	intel := state.intel.Snapshot(now, m.answerCapacity)

	switch intel.Heat {
	case roomintel.HeatCold:
		state.timeline.RaiseDebt(timeline.DebtInteraction, 0.18, now)
	case roomintel.HeatWarm:
		state.timeline.RaiseDebt(timeline.DebtInteraction, 0.10, now)
	default:
		state.timeline.RaiseDebt(timeline.DebtInteraction, 0.02, now)
	}
	if intel.QuestionCount30s > 0 {
		state.timeline.RaiseDebt(timeline.DebtQuestion, math.Min(0.30, 0.06*float64(intel.QuestionCount30s)), now)
	}
	if intel.Entries30s >= 5 && intel.Likes30s < int64(intel.Entries30s) {
		state.timeline.RaiseDebt(timeline.DebtLikeCTA, 0.07, now)
	}
	if intel.Entries30s >= 5 && intel.Follows30s == 0 {
		state.timeline.RaiseDebt(timeline.DebtFollowCTA, 0.04, now)
	}
	if intel.Heat != roomintel.HeatCold {
		state.timeline.RaiseDebt(timeline.DebtConversion, 0.03, now)
	}
	if intel.Orders30s > 0 || intel.OrderSignals30s > 0 {
		state.timeline.RaiseDebt(timeline.DebtConversion, 0.10, now)
	}
}

func (m *Manager) SetStyle(roomID int64, style director.StyleProfile) {
	state := m.room(roomID, m.now())
	if state == nil {
		return
	}
	state.mu.Lock()
	state.style = style
	state.mu.Unlock()
}

func (m *Manager) SetResumeContext(roomID int64, ctx resume.Context) {
	state := m.room(roomID, m.now())
	if state == nil {
		return
	}
	state.mu.Lock()
	state.resumeContext = ctx
	state.mu.Unlock()
}

func (m *Manager) RecordPin(roomID int64, pin timeline.Pin, spend timeline.DebtKind, cooldown time.Duration) {
	state := m.room(roomID, pin.At)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()

	if pin.ID == "" {
		pin.ID = fmt.Sprintf("%d-%d", roomID, m.now().UnixNano())
	}
	if pin.At.IsZero() {
		pin.At = m.now()
	}
	state.timeline.Append(pin)
	if pin.Kind == timeline.PinAnswer && pin.Topic != "" {
		state.intel.MarkAnswered(pin.Topic, pin.At)
	}
	if spend != "" {
		state.timeline.SpendDebt(spend, cooldown, pin.At)
	}
}

func (m *Manager) RemoveUser(roomID int64, userID string) {
	userID = strings.TrimSpace(userID)
	if roomID <= 0 || userID == "" {
		return
	}
	m.mu.Lock()
	state := m.rooms[roomID]
	m.mu.Unlock()
	if state == nil {
		return
	}
	state.mu.Lock()
	state.intel.RemoveUser(userID)
	state.mu.Unlock()
}

func (m *Manager) MergeTopics(roomID int64, representative string, sources []string) int {
	if roomID <= 0 || strings.TrimSpace(representative) == "" || len(sources) == 0 {
		return 0
	}
	m.mu.Lock()
	state := m.rooms[roomID]
	m.mu.Unlock()
	if state == nil {
		return 0
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.intel.MergeTopics(representative, sources)
}

func (m *Manager) Reset(roomID int64) {
	m.mu.Lock()
	delete(m.rooms, roomID)
	m.mu.Unlock()
}

type TopicView struct {
	Topic                 string
	Count                 int
	UniqueUsers           int
	LastSeenAt            time.Time
	LastAnsweredAt        time.Time
	SampleQuestions       []string
	Questions             []roomintel.TopicQuestion
	TTSQuestions          []roomintel.TopicQuestion
	TTSEligibleCount      int
	ArchivedQuestionCount int
}

type IntelligenceView struct {
	Heat                string
	OnlineCount         int
	Entries30s          int
	Entries60s          int
	Chats30s            int
	Likes30s            int64
	Follows30s          int
	Orders30s           int
	OrderSignals30s     int
	OrderSignals60s     int
	OrderSignalSamples  []string
	SessionEntries      int
	SessionChats        int
	SessionLikes        int64
	SessionFollows      int
	SessionGifts        int
	UniqueChatters30s   int
	QuestionCount30s    int
	NegativeFeedback30s int
	QuestionPressure    float64
	AudienceTurnover5m  float64
	PreferAggregateQNA  bool
	PreferOneToOneQNA   bool
	TopTopics           []TopicView
}

type PinView struct {
	ID           string
	At           time.Time
	Kind         string
	Strategy     string
	Topic        string
	Key          string
	MainlineUnit string
	Metadata     map[string]string
}

type DebtView struct {
	Value         float64
	LastRaised    time.Time
	LastSpent     time.Time
	CooldownUntil time.Time
}

type TimelineView struct {
	StartedAt        time.Time
	HotWindowSeconds int64
	HotPins          []PinView
	Debts            map[string]DebtView
	Summary          map[string]any
}

type HumanizationView struct {
	Strategy    string
	Enabled     bool
	Kind        string
	Delivery    string
	Instruction string
	Reason      string
}

type ResumeView struct {
	Strategy           string
	Mode               string
	BridgeText         string
	ResumeUnit         string
	SkipUnits          []string
	AbandonCurrentPlan bool
	ReAnchor           bool
	Reason             string
}

type DirectorView struct {
	Progress         string
	Atmosphere       string
	TargetSeconds    int
	HumorLevel       int
	HumorInstruction string
	PromptDirectives []string
	StrategyTrace    []string
	Humanization     HumanizationView
	Resume           ResumeView
}

type View struct {
	RoomID       int64
	GeneratedAt  time.Time
	Intelligence IntelligenceView
	Timeline     TimelineView
	Director     DirectorView
}

func (m *Manager) Snapshot(roomID int64) (View, error) {
	now := m.now()
	state := m.room(roomID, now)
	if state == nil {
		return View{}, fmt.Errorf("invalid room id")
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	intel := state.intel.Snapshot(now, m.answerCapacity)
	m.evaluateDebtsLocked(state, now)
	timelineView := state.timeline.View(now, mapTimelineHeat(intel.Heat))

	humanContext := buildHumanizationContext(now, intel, timelineView)
	debts := timelineView.Debts
	secondsSinceHumor := secondsSincePin(now, timelineView.HotPins, timeline.PinHumor)

	progressHint := ""
	if debts[timeline.DebtConversion].Value >= 0.80 {
		progressHint = "CONVERSION"
	}

	directorPlan, err := m.planner.Plan(director.Snapshot{
		RoomID:             roomID,
		ProgressHint:       progressHint,
		ProductStoryRemain: true,
		Style:              state.style,
		Signals: director.RoomSignals{
			OnlineCount:         intel.OnlineCount,
			Events30s:           intel.Entries30s + intel.Chats30s + intel.Follows30s + intel.Orders30s,
			Chat30s:             intel.Chats30s,
			Members30s:          intel.Entries30s,
			Follows30s:          intel.Follows30s,
			Likes30s:            int(intel.Likes30s),
			Orders30s:           intel.Orders30s,
			OrderSignals30s:     intel.OrderSignals30s,
			NegativeFeedback30s: intel.NegativeFeedback30s,
			ActionableQuestions: intel.QuestionCount30s,
			SecondsSinceHumor:   secondsSinceHumor,
			PaceLevel:           string(intel.Heat),
			QuestionPressure:    intel.QuestionPressure,
			AudienceTurnover5m:  intel.AudienceTurnover5m,
			PreferAggregateQNA:  intel.PreferAggregateQNA,
			PreferOneToOneQNA:   intel.PreferOneToOneQNA,
			InteractionDebt:     debts[timeline.DebtInteraction].Value,
			LikeDebt:            debts[timeline.DebtLikeCTA].Value,
			FollowDebt:          debts[timeline.DebtFollowCTA].Value,
			ConversionDebt:      debts[timeline.DebtConversion].Value,
		},
		Humanization: humanContext,
		Resume:       state.resumeContext,
	})
	if err != nil {
		return View{}, err
	}

	return View{
		RoomID:       roomID,
		GeneratedAt:  now,
		Intelligence: intelligenceDTO(intel),
		Timeline:     timelineDTO(timelineView),
		Director:     directorDTO(directorPlan),
	}, nil
}

func mapTimelineHeat(heat roomintel.Heat) timeline.Heat {
	switch heat {
	case roomintel.HeatCold:
		return timeline.HeatCold
	case roomintel.HeatWarm:
		return timeline.HeatWarm
	case roomintel.HeatHot:
		return timeline.HeatHot
	case roomintel.HeatOverheated:
		return timeline.HeatOverheated
	default:
		return timeline.HeatBusy
	}
}

func buildHumanizationContext(now time.Time, intel roomintel.Snapshot, view timeline.View) humanization.Context {
	lastHumanization := secondsSincePin(now, view.HotPins, timeline.PinHumanization)
	behaviorCount := 0
	for _, pin := range view.HotPins {
		if pin.Kind == timeline.PinHumanization && now.Sub(pin.At) <= time.Minute {
			behaviorCount++
		}
	}
	afterSale := false
	priceDispute := false
	for _, bucket := range intel.TopTopics {
		for _, sample := range bucket.SampleQuestions {
			lower := strings.ToLower(sample)
			if containsAny(lower, "售后", "退货", "退款", "破损", "坏了", "漏气") {
				afterSale = true
			}
			if intel.NegativeFeedback30s > 0 && containsAny(lower, "多少钱", "价格", "价钱", "贵", "优惠") {
				priceDispute = true
			}
		}
	}

	return humanization.Context{
		Opportunity:              intel.Heat != roomintel.HeatOverheated,
		NegativeFeedback:         intel.NegativeFeedback30s > 0,
		Complaint:                afterSale && intel.NegativeFeedback30s > 0,
		AfterSale:                afterSale,
		PriceDispute:             priceDispute,
		HighIntentClose:          intel.Orders30s > 0 || intel.OrderSignals30s > 0,
		SecondsSinceLastBehavior: lastHumanization,
		BehaviorCount60s:         behaviorCount,
		MaxBehaviorsPerMinute:    1,
		SecondsSinceThroatClear:  secondsSinceStrategy(now, view.HotPins, "humanization.throat_clear"),
		SecondsSinceCough:        secondsSinceStrategy(now, view.HotPins, "humanization.cough"),
		SecondsSinceCorrection:   secondsSinceStrategy(now, view.HotPins, "humanization.self_correction"),
		SecondsSinceRepeat:       secondsSinceStrategy(now, view.HotPins, "humanization.repeat_fragment"),
		SecondsSinceInversion:    secondsSinceStrategy(now, view.HotPins, "humanization.inversion"),
		PreferLightDisfluency:    true,
		PreferRepetition:         true,
		PreferInversion:          true,
		AllowNonVerbal:           true,
		Capabilities: humanization.Capabilities{
			SupportsParalinguisticMarks: false,
			HasThroatClearAsset:         false,
			HasCoughAsset:               false,
		},
	}
}

func secondsSincePin(now time.Time, pins []timeline.Pin, kind timeline.PinKind) int {
	var latest time.Time
	for _, pin := range pins {
		if pin.Kind == kind && pin.At.After(latest) {
			latest = pin.At
		}
	}
	if latest.IsZero() {
		return 3600
	}
	value := int(now.Sub(latest).Seconds())
	if value < 0 {
		return 0
	}
	return value
}

func secondsSinceStrategy(now time.Time, pins []timeline.Pin, strategy string) int {
	var latest time.Time
	for _, pin := range pins {
		if pin.Strategy == strategy && pin.At.After(latest) {
			latest = pin.At
		}
	}
	if latest.IsZero() {
		return 3600
	}
	value := int(now.Sub(latest).Seconds())
	if value < 0 {
		return 0
	}
	return value
}

func intelligenceDTO(value roomintel.Snapshot) IntelligenceView {
	topics := make([]TopicView, 0, len(value.TopTopics))
	for _, bucket := range value.TopTopics {
		topics = append(topics, TopicView{
			Topic:                 bucket.Topic,
			Count:                 bucket.Count,
			UniqueUsers:           bucket.UniqueUsers,
			LastSeenAt:            bucket.LastSeenAt,
			LastAnsweredAt:        bucket.LastAnsweredAt,
			SampleQuestions:       bucket.SampleQuestions,
			Questions:             bucket.Questions,
			TTSQuestions:          bucket.TTSQuestions,
			TTSEligibleCount:      bucket.TTSEligibleCount,
			ArchivedQuestionCount: bucket.ArchivedQuestionCount,
		})
	}
	return IntelligenceView{
		Heat:                string(value.Heat),
		OnlineCount:         value.OnlineCount,
		Entries30s:          value.Entries30s,
		Entries60s:          value.Entries60s,
		Chats30s:            value.Chats30s,
		Likes30s:            value.Likes30s,
		Follows30s:          value.Follows30s,
		Orders30s:           value.Orders30s,
		OrderSignals30s:     value.OrderSignals30s,
		OrderSignals60s:     value.OrderSignals60s,
		OrderSignalSamples:  value.OrderSignalSamples,
		SessionEntries:      value.SessionEntries,
		SessionChats:        value.SessionChats,
		SessionLikes:        value.SessionLikes,
		SessionFollows:      value.SessionFollows,
		SessionGifts:        value.SessionGifts,
		UniqueChatters30s:   value.UniqueChatters30s,
		QuestionCount30s:    value.QuestionCount30s,
		NegativeFeedback30s: value.NegativeFeedback30s,
		QuestionPressure:    value.QuestionPressure,
		AudienceTurnover5m:  value.AudienceTurnover5m,
		PreferAggregateQNA:  value.PreferAggregateQNA,
		PreferOneToOneQNA:   value.PreferOneToOneQNA,
		TopTopics:           topics,
	}
}

func timelineDTO(value timeline.View) TimelineView {
	pins := make([]PinView, 0, len(value.HotPins))
	for _, pin := range value.HotPins {
		pins = append(pins, PinView{
			ID:           pin.ID,
			At:           pin.At,
			Kind:         string(pin.Kind),
			Strategy:     pin.Strategy,
			Topic:        pin.Topic,
			Key:          pin.Key,
			MainlineUnit: pin.MainlineUnit,
			Metadata:     pin.Metadata,
		})
	}
	debts := map[string]DebtView{}
	for key, debt := range value.Debts {
		debts[string(key)] = DebtView{
			Value:         debt.Value,
			LastRaised:    debt.LastRaised,
			LastSpent:     debt.LastSpent,
			CooldownUntil: debt.CooldownUntil,
		}
	}
	return TimelineView{
		StartedAt:        value.StartedAt,
		HotWindowSeconds: int64(value.HotWindow / time.Second),
		HotPins:          pins,
		Debts:            debts,
		Summary: map[string]any{
			"counts_by_kind":     value.Summary.CountsByKind,
			"counts_by_strategy": value.Summary.CountsByStrategy,
			"topic_counts":       value.Summary.TopicCounts,
			"last_by_kind":       value.Summary.LastByKind,
			"last_by_strategy":   value.Summary.LastByStrategy,
			"last_by_topic":      value.Summary.LastByTopic,
		},
	}
}

func directorDTO(value director.Plan) DirectorView {
	return DirectorView{
		Progress:         string(value.Progress),
		Atmosphere:       string(value.Atmosphere),
		TargetSeconds:    value.TargetSeconds,
		HumorLevel:       value.HumorLevel,
		HumorInstruction: value.HumorInstruction,
		PromptDirectives: value.PromptDirectives,
		StrategyTrace:    value.StrategyTrace,
		Humanization: HumanizationView{
			Strategy:    value.Humanization.Strategy,
			Enabled:     value.Humanization.Enabled,
			Kind:        string(value.Humanization.Event.Kind),
			Delivery:    string(value.Humanization.Event.Delivery),
			Instruction: value.Humanization.Event.Instruction,
			Reason:      value.Humanization.Reason,
		},
		Resume: ResumeView{
			Strategy:           value.Resume.Strategy,
			Mode:               string(value.Resume.Mode),
			BridgeText:         value.Resume.BridgeText,
			ResumeUnit:         value.Resume.ResumeUnit,
			SkipUnits:          value.Resume.SkipUnits,
			AbandonCurrentPlan: value.Resume.AbandonCurrentPlan,
			ReAnchor:           value.Resume.ReAnchor,
			Reason:             value.Resume.Reason,
		},
	}
}
