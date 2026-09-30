package strategycenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

type Rule struct {
	Category        string         `json:"category"`
	Key             string         `json:"key"`
	Name            string         `json:"name"`
	Description     string         `json:"description,omitempty"`
	Enabled         bool           `json:"enabled"`
	BaseProbability int            `json:"base_probability"`
	MinProbability  int            `json:"min_probability"`
	SystemDefault   bool           `json:"system_default,omitempty"`
	Config          map[string]any `json:"config,omitempty"`
}

type AddressingOption struct {
	Key           string `json:"key"`
	Text          string `json:"text"`
	Enabled       bool   `json:"enabled"`
	Probability   int    `json:"probability"`
	SystemDefault bool   `json:"system_default"`
}

type Policy struct {
	TenantID       int64              `json:"tenant_id"`
	Rules          []Rule             `json:"rules"`
	AddressingMode string             `json:"addressing_mode"`
	Addressing     []AddressingOption `json:"addressing"`
}

type RoomInteractionPreferences struct {
	TenantID             int64     `json:"tenant_id"`
	RoomID               int64     `json:"room_id"`
	OverallInteraction   string    `json:"overall_interaction"`
	QuestionPreference   string    `json:"question_preference"`
	WelcomePreference    string    `json:"welcome_preference"`
	EngagementPreference string    `json:"engagement_preference"`
	ChatPreference       string    `json:"chat_preference"`
	ConversionPreference string    `json:"conversion_preference"`
	AutoHeat             bool      `json:"auto_heat"`
	UpdatedAt            time.Time `json:"updated_at,omitempty"`
}

type Signals struct {
	Entries30s int    `json:"entries_30s,omitempty"`
	Likes30s   int64  `json:"likes_30s,omitempty"`
	Follows30s int    `json:"follows_30s,omitempty"`
	Heat       string `json:"heat,omitempty"`
}

type Candidate struct {
	Key                  string  `json:"key"`
	Name                 string  `json:"name"`
	BaseWeight           int     `json:"base_weight,omitempty"`
	Weight               int     `json:"weight"`
	EffectiveProbability float64 `json:"effective_probability"`
	CoverageDebt         int     `json:"coverage_debt,omitempty"`
	RepeatPenalty        float64 `json:"repeat_penalty,omitempty"`
	DiversityBoost       float64 `json:"diversity_boost,omitempty"`
}

type Selection struct {
	Category   string      `json:"category"`
	Key        string      `json:"key"`
	Name       string      `json:"name"`
	Candidates []Candidate `json:"candidates"`
	Roll       int         `json:"roll"`
	Total      int         `json:"total_weight"`
	Seed       string      `json:"seed"`
}

type TriggerDecision struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Probability int    `json:"probability"`
	Roll        int    `json:"roll"`
	Triggered   bool   `json:"triggered"`
}

type ProbabilityStat struct {
	Category              string    `json:"category"`
	Key                   string    `json:"key"`
	Name                  string    `json:"name"`
	Enabled               bool      `json:"enabled"`
	ConfiguredProbability float64   `json:"configured_probability"`
	Samples               int64     `json:"samples"`
	HitCount              int64     `json:"hit_count"`
	LastProbability       float64   `json:"last_probability"`
	AverageProbability    float64   `json:"average_probability"`
	MinimumProbability    float64   `json:"minimum_probability"`
	MaximumProbability    float64   `json:"maximum_probability"`
	LastEvaluatedAt       time.Time `json:"last_evaluated_at"`
	EligibleCount         int64     `json:"eligible_count,omitempty"`
	SelectedCount         int64     `json:"selected_count,omitempty"`
	ConsecutiveMiss       int       `json:"consecutive_miss,omitempty"`
	CoverageDebt          int       `json:"coverage_debt,omitempty"`
	RepeatPenalty         float64   `json:"repeat_penalty,omitempty"`
	DiversityBoost        float64   `json:"diversity_boost,omitempty"`
	EffectiveWeight       int       `json:"effective_weight,omitempty"`
	LastSelectedAt        time.Time `json:"last_selected_at,omitempty"`
}

type StageStats struct {
	RoomID           int64                   `json:"room_id"`
	StageID          string                  `json:"stage_id"`
	StartedAt        time.Time               `json:"started_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
	DecisionCount    int64                   `json:"decision_count"`
	Items            []ProbabilityStat       `json:"items"`
	InteractionItems []InteractionWindowStat `json:"interaction_items"`
}

type InteractionWindowStat struct {
	Key                   string    `json:"key"`
	Name                  string    `json:"name"`
	State                 string    `json:"state"`
	PendingCount          int       `json:"pending_count"`
	TotalEvents           int64     `json:"total_events"`
	EmittedCount          int64     `json:"emitted_count"`
	LastMissionEventCount int       `json:"last_mission_event_count"`
	MinIntervalSeconds    int64     `json:"min_interval_seconds"`
	MaxWaitSeconds        int64     `json:"max_wait_seconds"`
	ConfiguredWeight      int       `json:"configured_weight"`
	EffectiveWeight       int       `json:"effective_weight"`
	EffectivePriority     int       `json:"effective_priority"`
	FirstPendingAt        time.Time `json:"first_pending_at"`
	LastEventAt           time.Time `json:"last_event_at"`
	LastEmittedAt         time.Time `json:"last_emitted_at"`
	NextDueAt             time.Time `json:"next_due_at"`
	LastEventValue        float64   `json:"last_event_value,omitempty"`
	LastValueLevel        string    `json:"last_value_level,omitempty"`
	LastDecisionReason    string    `json:"last_decision_reason,omitempty"`
	LastBudgetLevel       string    `json:"last_budget_level,omitempty"`
	LastBudgetAllowed     bool      `json:"last_budget_allowed"`
}

type probabilityAccumulator struct {
	ProbabilityStat
	sum float64
}

type stageAccumulator struct {
	roomID                 int64
	stageID                string
	startedAt              time.Time
	updatedAt              time.Time
	decisionCount          int64
	items                  map[string]*probabilityAccumulator
	lastSelectedByCategory map[string]string
	repeatCountByCategory  map[string]int
}

type Store struct {
	mu               sync.RWMutex
	policies         map[int64]Policy
	roomPreferences  map[int64]RoomInteractionPreferences
	stages           map[int64]*stageAccumulator
	interactionStats map[int64][]InteractionWindowStat
	subscribers      map[int64]map[uint64]chan StageStats
	nextSubscriberID uint64
}

func DefaultPolicy() Policy {
	return Policy{
		Rules: []Rule{
			{Category: "interrupt", Key: "read_comment_softly", Name: "小声读一次弹幕", Enabled: true, BaseProbability: 20, MinProbability: 10},
			{Category: "interrupt", Key: "hard_cut", Name: "直接硬切", Enabled: true, BaseProbability: 20, MinProbability: 10},
			{Category: "interrupt", Key: "ask_controller", Name: "询问中控", Enabled: true, BaseProbability: 10, MinProbability: 10},
			{Category: "interrupt", Key: "thinking_pause", Name: "短暂停顿", Enabled: true, BaseProbability: 20, MinProbability: 10},
			{Category: "interrupt", Key: "repeat_confirm", Name: "重复确认", Enabled: true, BaseProbability: 30, MinProbability: 10},
			{Category: "resume", Key: "DIRECT", Name: "直接续播", Enabled: true, BaseProbability: 20, MinProbability: 20},
			{Category: "resume", Key: "BRIDGE", Name: "桥接恢复", Enabled: true, BaseProbability: 20, MinProbability: 20},
			{Category: "resume", Key: "FUSION_SKIP", Name: "融合跳过", Enabled: true, BaseProbability: 20, MinProbability: 20},
			{Category: "resume", Key: "CROSS_RESUME", Name: "跨段恢复", Enabled: true, BaseProbability: 20, MinProbability: 20},
			{Category: "resume", Key: "RE_ANCHOR", Name: "重新锚定", Enabled: true, BaseProbability: 20, MinProbability: 20},
			{Category: "resume", Key: "SWITCH_PLAN", Name: "切换主线方案", Enabled: false, BaseProbability: 0, MinProbability: 20},
			{Category: "interaction", Key: "welcome_named", Name: "点名欢迎", Enabled: true, BaseProbability: 10, MinProbability: 5},
			{Category: "interaction", Key: "welcome_batch", Name: "打包欢迎", Enabled: true, BaseProbability: 15, MinProbability: 5},
			{Category: "interaction", Key: "reply_like", Name: "回应点赞", Enabled: true, BaseProbability: 8, MinProbability: 5},
			{Category: "interaction", Key: "reply_follow", Name: "回应关注", Enabled: true, BaseProbability: 8, MinProbability: 5},
			{Category: "interaction", Key: "reply_chat", Name: "回复弹幕", Enabled: true, BaseProbability: 50, MinProbability: 20},
		},
		AddressingMode: "system",
		Addressing: []AddressingOption{
			{Key: "friend", Text: "朋友", Enabled: true, Probability: 40, SystemDefault: true},
			{Key: "boss", Text: "老板", Enabled: true, Probability: 20, SystemDefault: true},
			{Key: "everyone", Text: "大家", Enabled: true, Probability: 20, SystemDefault: true},
			{Key: "baozi", Text: "宝子", Enabled: true, Probability: 20, SystemDefault: true},
		},
	}
}

func New() *Store {
	defaults := DefaultPolicy()
	defaults.TenantID = 0
	return &Store{
		policies:         map[int64]Policy{0: defaults},
		roomPreferences:  make(map[int64]RoomInteractionPreferences),
		stages:           make(map[int64]*stageAccumulator),
		interactionStats: make(map[int64][]InteractionWindowStat),
		subscribers:      make(map[int64]map[uint64]chan StageStats),
	}
}

func (s *Store) Load(ctx context.Context, db *sql.DB) error {
	if s == nil || db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT tenant_id, CAST(config_json AS CHAR) FROM live_strategy_center_configs`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tenantID int64
		var raw string
		if err := rows.Scan(&tenantID, &raw); err != nil {
			return err
		}
		var policy Policy
		if err := json.Unmarshal([]byte(raw), &policy); err != nil {
			return fmt.Errorf("decode strategy policy tenant %d: %w", tenantID, err)
		}
		s.Put(tenantID, policy)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	preferenceRows, err := db.QueryContext(ctx, `
		SELECT tenant_id, room_id, overall_interaction, question_preference,
		       welcome_preference, engagement_preference, chat_preference,
		       conversion_preference, auto_heat, updated_at
		FROM live_room_interaction_preferences
	`)
	if err != nil {
		return fmt.Errorf("load room interaction preferences: %w", err)
	}
	defer preferenceRows.Close()
	for preferenceRows.Next() {
		var item RoomInteractionPreferences
		if err := preferenceRows.Scan(
			&item.TenantID,
			&item.RoomID,
			&item.OverallInteraction,
			&item.QuestionPreference,
			&item.WelcomePreference,
			&item.EngagementPreference,
			&item.ChatPreference,
			&item.ConversionPreference,
			&item.AutoHeat,
			&item.UpdatedAt,
		); err != nil {
			return err
		}
		s.PutRoomInteractionPreferences(item.RoomID, item)
	}
	return preferenceRows.Err()
}

func DefaultRoomInteractionPreferences(roomID int64) RoomInteractionPreferences {
	return RoomInteractionPreferences{
		RoomID:               roomID,
		OverallInteraction:   "natural",
		QuestionPreference:   "natural",
		WelcomePreference:    "natural",
		EngagementPreference: "natural",
		ChatPreference:       "natural",
		ConversionPreference: "natural",
		AutoHeat:             true,
	}
}

func normalizePreferenceValue(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func normalizeRoomInteractionPreferences(roomID int64, input RoomInteractionPreferences) RoomInteractionPreferences {
	defaults := DefaultRoomInteractionPreferences(roomID)
	input.RoomID = roomID
	input.OverallInteraction = normalizePreferenceValue(input.OverallInteraction, defaults.OverallInteraction)
	input.QuestionPreference = normalizePreferenceValue(input.QuestionPreference, defaults.QuestionPreference)
	input.WelcomePreference = normalizePreferenceValue(input.WelcomePreference, defaults.WelcomePreference)
	input.EngagementPreference = normalizePreferenceValue(input.EngagementPreference, defaults.EngagementPreference)
	input.ChatPreference = normalizePreferenceValue(input.ChatPreference, defaults.ChatPreference)
	input.ConversionPreference = normalizePreferenceValue(input.ConversionPreference, defaults.ConversionPreference)
	return input
}

func (s *Store) PutRoomInteractionPreferences(roomID int64, input RoomInteractionPreferences) RoomInteractionPreferences {
	input = normalizeRoomInteractionPreferences(roomID, input)
	if s == nil || roomID <= 0 {
		return input
	}
	s.mu.Lock()
	s.roomPreferences[roomID] = input
	s.mu.Unlock()
	return input
}

func (s *Store) RoomInteractionPreferences(roomID int64) RoomInteractionPreferences {
	defaults := DefaultRoomInteractionPreferences(roomID)
	if s == nil || roomID <= 0 {
		return defaults
	}
	s.mu.RLock()
	item, ok := s.roomPreferences[roomID]
	s.mu.RUnlock()
	if !ok {
		return defaults
	}
	return normalizeRoomInteractionPreferences(roomID, item)
}

func preferenceFactor(value string, less, more float64) float64 {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "quiet", "less", "steady":
		return less
	case "active", "more":
		return more
	default:
		return 1
	}
}

func (s *Store) InteractionPreferenceFactor(roomID int64, kind string) float64 {
	prefs := s.RoomInteractionPreferences(roomID)
	factor := preferenceFactor(prefs.OverallInteraction, 0.78, 1.24)
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "question":
		factor *= preferenceFactor(prefs.QuestionPreference, 0.72, 1.38)
	case "welcome":
		factor *= preferenceFactor(prefs.WelcomePreference, 0.68, 1.36)
	case "engagement":
		factor *= preferenceFactor(prefs.EngagementPreference, 0.68, 1.32)
	case "chat":
		factor *= preferenceFactor(prefs.ChatPreference, 0.64, 1.34)
	case "conversion":
		factor *= preferenceFactor(prefs.ConversionPreference, 0.82, 1.38)
	}
	return factor
}

func interactionPreferenceKind(key string) string {
	switch strings.TrimSpace(key) {
	case "welcome_named", "welcome_batch":
		return "welcome"
	case "reply_like", "reply_follow":
		return "engagement"
	case "reply_chat", "reply_chat_social":
		return "chat"
	default:
		return "question"
	}
}

func (s *Store) AdjustInteractionWeight(roomID int64, key string, weight int) int {
	if weight <= 0 {
		return 0
	}
	adjusted := int(math.Round(float64(weight) * s.InteractionPreferenceFactor(roomID, interactionPreferenceKind(key))))
	if adjusted < 1 {
		return 1
	}
	if adjusted > 100 {
		return 100
	}
	return adjusted
}

func (s *Store) Put(tenantID int64, policy Policy) Policy {
	if s == nil {
		return policy
	}
	policy.TenantID = tenantID
	s.mu.Lock()
	s.policies[tenantID] = policy
	s.mu.Unlock()
	return policy
}

func (s *Store) Resolve(tenantID int64) Policy {
	if s == nil {
		return Policy{TenantID: tenantID}
	}
	s.mu.RLock()
	global, globalOK := s.policies[0]
	policy, ok := s.policies[tenantID]
	if !ok {
		policy, ok = global, globalOK
	} else if tenantID != 0 && globalOK {
		// Tenant configuration only owns user-facing addressing choices today.
		// Always inherit the latest global behavior/resume/interaction rules so
		// a customer who customized a nickname never freezes old strategy odds.
		policy.Rules = global.Rules
		if len(policy.Addressing) == 0 || !strings.EqualFold(strings.TrimSpace(policy.AddressingMode), "custom") {
			policy.Addressing = global.Addressing
			policy.AddressingMode = "system"
		}
	}
	s.mu.RUnlock()
	if !ok {
		return Policy{TenantID: tenantID}
	}
	policy.TenantID = tenantID
	return policy
}

func (s *Store) Allowed(tenantID int64, category, key string) bool {
	category = strings.ToLower(strings.TrimSpace(category))
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	for _, rule := range s.Resolve(tenantID).Rules {
		if rule.Enabled && rule.BaseProbability > 0 && strings.ToLower(strings.TrimSpace(rule.Category)) == category && strings.TrimSpace(rule.Key) == key {
			return true
		}
	}
	return false
}

func (s *Store) RuleWeight(tenantID int64, category, key string, signals Signals) (configured int, effective int, enabled bool) {
	category = strings.ToLower(strings.TrimSpace(category))
	key = strings.TrimSpace(key)
	if key == "" {
		return 0, 0, false
	}
	for _, rule := range s.Resolve(tenantID).Rules {
		if strings.ToLower(strings.TrimSpace(rule.Category)) != category || strings.TrimSpace(rule.Key) != key {
			continue
		}
		configured = rule.BaseProbability
		if !rule.Enabled || configured <= 0 {
			return configured, 0, false
		}
		effective = adjustWeight(rule.Key, configured, signals)
		if effective > 100 {
			effective = 100
		}
		return configured, effective, effective > 0
	}
	return 0, 0, false
}

func (s *Store) Pick(tenantID int64, category string, candidates []string, signals Signals, seed string) Selection {
	return s.pickForRoom(0, "", tenantID, category, candidates, signals, seed)
}

func (s *Store) PickForRoom(roomID int64, stageID string, tenantID int64, category string, candidates []string, signals Signals, seed string) Selection {
	return s.pickForRoom(roomID, strings.TrimSpace(stageID), tenantID, category, candidates, signals, seed)
}

func (s *Store) pickForRoom(roomID int64, stageID string, tenantID int64, category string, candidates []string, signals Signals, seed string) Selection {
	category = strings.ToLower(strings.TrimSpace(category))
	if seed == "" {
		seed = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	resumeStats, lastResumeKey, resumeRepeatCount := s.resumeDiversitySnapshot(roomID, stageID)
	policy := s.Resolve(tenantID)
	allowed := map[string]struct{}{}
	for _, key := range candidates {
		key = strings.TrimSpace(key)
		if key != "" {
			allowed[key] = struct{}{}
		}
	}
	weighted := make([]Candidate, 0)
	for _, rule := range policy.Rules {
		if !rule.Enabled || strings.ToLower(strings.TrimSpace(rule.Category)) != category {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[rule.Key]; !ok {
				continue
			}
		}
		baseWeight := rule.BaseProbability
		if baseWeight <= 0 {
			continue
		}
		baseWeight = adjustWeight(rule.Key, baseWeight, signals)
		if baseWeight <= 0 {
			continue
		}
		candidate := Candidate{Key: rule.Key, Name: rule.Name, BaseWeight: baseWeight, Weight: baseWeight}
		if category == "resume" && roomID > 0 && stageID != "" {
			stat := resumeStats[rule.Key]
			candidate.CoverageDebt = stat.CoverageDebt
			candidate.Weight, candidate.RepeatPenalty, candidate.DiversityBoost = resumeDiversityWeight(
				baseWeight,
				stat,
				rule.Key == lastResumeKey,
				resumeRepeatCount,
			)
		}
		weighted = append(weighted, candidate)
	}
	if len(weighted) == 0 {
		for _, key := range candidates {
			key = strings.TrimSpace(key)
			if key != "" {
				candidate := Candidate{Key: key, Name: key, BaseWeight: 1, Weight: 1}
				if category == "resume" && roomID > 0 && stageID != "" {
					stat := resumeStats[key]
					candidate.CoverageDebt = stat.CoverageDebt
					candidate.Weight, candidate.RepeatPenalty, candidate.DiversityBoost = resumeDiversityWeight(
						1,
						stat,
						key == lastResumeKey,
						resumeRepeatCount,
					)
				}
				weighted = append(weighted, candidate)
			}
		}
	}
	selection := Selection{Category: category, Candidates: weighted, Seed: seed}
	if len(weighted) == 0 {
		return selection
	}
	for _, item := range weighted {
		selection.Total += item.Weight
	}
	if selection.Total <= 0 {
		selection.Key = weighted[0].Key
		selection.Name = weighted[0].Name
		return selection
	}
	for i := range selection.Candidates {
		selection.Candidates[i].EffectiveProbability = effectiveProbability(
			selection.Candidates[i].Weight,
			selection.Total,
		)
	}
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:%s:%s", tenantID, category, seed)
	selection.Roll = int(h.Sum64()%uint64(selection.Total)) + 1
	cursor := 0
	for _, item := range weighted {
		cursor += item.Weight
		if selection.Roll <= cursor {
			selection.Key = item.Key
			selection.Name = item.Name
			return selection
		}
	}
	selection.Key = weighted[len(weighted)-1].Key
	selection.Name = weighted[len(weighted)-1].Name
	return selection
}

func (s *Store) resumeDiversitySnapshot(roomID int64, stageID string) (map[string]ProbabilityStat, string, int) {
	result := make(map[string]ProbabilityStat)
	if s == nil || roomID <= 0 || strings.TrimSpace(stageID) == "" {
		return result, "", 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	stage := s.stages[roomID]
	if stage == nil || stage.stageID != stageID {
		return result, "", 0
	}
	for identity, item := range stage.items {
		if item == nil || !strings.HasPrefix(identity, "resume:") {
			continue
		}
		result[item.Key] = item.ProbabilityStat
	}
	return result, stage.lastSelectedByCategory["resume"], stage.repeatCountByCategory["resume"]
}

func resumeDiversityWeight(baseWeight int, stat ProbabilityStat, isRecentSame bool, repeatCount int) (int, float64, float64) {
	if baseWeight <= 0 {
		return 0, 1, 1
	}
	repeatPenalty := 1.0
	if isRecentSame {
		switch {
		case repeatCount >= 3:
			repeatPenalty = 0.35
		case repeatCount == 2:
			repeatPenalty = 0.5
		default:
			repeatPenalty = 0.7
		}
	}
	debt := stat.CoverageDebt
	if debt < 0 {
		debt = 0
	}
	if debt > 5 {
		debt = 5
	}
	diversityBoost := 1 + float64(debt)*0.12
	if stat.EligibleCount >= 2 && stat.SelectedCount == 0 {
		diversityBoost += 0.15
	} else if stat.EligibleCount >= 4 && stat.SelectedCount*5 < stat.EligibleCount {
		diversityBoost += 0.08
	}
	effective := int(math.Round(float64(baseWeight) * repeatPenalty * diversityBoost))
	if effective < 1 {
		effective = 1
	}
	maxWeight := baseWeight * 3
	if maxWeight < 1 {
		maxWeight = 1
	}
	if effective > maxWeight {
		effective = maxWeight
	}
	return effective, repeatPenalty, diversityBoost
}

func (s *Store) PickAddressing(tenantID int64, seed string) Selection {
	if seed == "" {
		seed = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	policy := s.Resolve(tenantID)
	wantedSystem := !strings.EqualFold(strings.TrimSpace(policy.AddressingMode), "custom")
	items := make([]Candidate, 0)
	for _, option := range policy.Addressing {
		if !option.Enabled || option.Probability <= 0 {
			continue
		}
		if wantedSystem != option.SystemDefault {
			continue
		}
		items = append(items, Candidate{Key: option.Key, Name: option.Text, Weight: option.Probability})
	}
	selection := Selection{Category: "addressing", Candidates: items, Seed: seed}
	if len(items) == 0 {
		return selection
	}
	for _, item := range items {
		selection.Total += item.Weight
	}
	if selection.Total <= 0 {
		return selection
	}
	for i := range selection.Candidates {
		selection.Candidates[i].EffectiveProbability = effectiveProbability(
			selection.Candidates[i].Weight,
			selection.Total,
		)
	}
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:addressing:%s", tenantID, seed)
	selection.Roll = int(h.Sum64()%uint64(selection.Total)) + 1
	cursor := 0
	for _, item := range items {
		cursor += item.Weight
		if selection.Roll <= cursor {
			selection.Key = item.Key
			selection.Name = item.Name
			return selection
		}
	}
	return selection
}

func (s *Store) Trigger(tenantID int64, category, key string, signals Signals, seed string) TriggerDecision {
	category = strings.ToLower(strings.TrimSpace(category))
	key = strings.TrimSpace(key)
	decision := TriggerDecision{Key: key}
	policy := s.Resolve(tenantID)
	found := false
	for _, rule := range policy.Rules {
		if !rule.Enabled || strings.ToLower(strings.TrimSpace(rule.Category)) != category || rule.Key != key {
			continue
		}
		found = true
		decision.Name = rule.Name
		decision.Probability = adjustWeight(rule.Key, rule.BaseProbability, signals)
		if decision.Probability > 100 {
			decision.Probability = 100
		}
		break
	}
	if !found {
		// Core may restart before Management has re-synchronized the persisted
		// strategy policy. Preserve the legacy behavior instead of muting all
		// interactions during that short bootstrap window.
		decision.Name = key
		decision.Probability = 100
		decision.Roll = 1
		decision.Triggered = true
		return decision
	}
	if decision.Probability <= 0 {
		return decision
	}
	if seed == "" {
		seed = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:trigger:%s:%s:%s", tenantID, category, key, seed)
	decision.Roll = int(h.Sum64()%100) + 1
	decision.Triggered = decision.Roll <= decision.Probability
	return decision
}

func effectiveProbability(weight, total int) float64 {
	if weight <= 0 || total <= 0 {
		return 0
	}
	return math.Round((float64(weight)*100/float64(total))*100) / 100
}

func (s *Store) RecordSelection(roomID int64, stageID string, startedAt time.Time, selection Selection) {
	if s == nil || roomID <= 0 || strings.TrimSpace(stageID) == "" || selection.Total <= 0 || len(selection.Candidates) == 0 {
		return
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	stage := s.ensureStageLocked(roomID, stageID, startedAt, now)
	stage.decisionCount++
	for _, candidate := range selection.Candidates {
		probability := candidate.EffectiveProbability
		if probability <= 0 && candidate.Weight > 0 {
			probability = effectiveProbability(candidate.Weight, selection.Total)
		}
		s.recordProbabilityLocked(
			stage,
			selection.Category,
			candidate.Key,
			candidate.Name,
			probability,
			candidate.Key == selection.Key,
			now,
		)
		if strings.EqualFold(strings.TrimSpace(selection.Category), "resume") {
			s.recordResumeDiversityLocked(stage, candidate, candidate.Key == selection.Key, now)
		}
	}
	if strings.EqualFold(strings.TrimSpace(selection.Category), "resume") && strings.TrimSpace(selection.Key) != "" {
		if stage.lastSelectedByCategory == nil {
			stage.lastSelectedByCategory = make(map[string]string)
		}
		if stage.repeatCountByCategory == nil {
			stage.repeatCountByCategory = make(map[string]int)
		}
		if stage.lastSelectedByCategory["resume"] == selection.Key {
			stage.repeatCountByCategory["resume"]++
		} else {
			stage.lastSelectedByCategory["resume"] = selection.Key
			stage.repeatCountByCategory["resume"] = 1
		}
	}
	s.publishStageLocked(roomID)
}

func (s *Store) recordResumeDiversityLocked(stage *stageAccumulator, candidate Candidate, selected bool, now time.Time) {
	if stage == nil || strings.TrimSpace(candidate.Key) == "" {
		return
	}
	identity := "resume:" + strings.TrimSpace(candidate.Key)
	item := stage.items[identity]
	if item == nil {
		return
	}
	item.EligibleCount++
	item.EffectiveWeight = candidate.Weight
	item.RepeatPenalty = candidate.RepeatPenalty
	if item.RepeatPenalty <= 0 {
		item.RepeatPenalty = 1
	}
	item.DiversityBoost = candidate.DiversityBoost
	if item.DiversityBoost <= 0 {
		item.DiversityBoost = 1
	}
	if selected {
		item.SelectedCount++
		item.ConsecutiveMiss = 0
		item.CoverageDebt -= 2
		if item.CoverageDebt < 0 {
			item.CoverageDebt = 0
		}
		item.LastSelectedAt = now
		return
	}
	item.ConsecutiveMiss++
	item.CoverageDebt++
	if item.CoverageDebt > 12 {
		item.CoverageDebt = 12
	}
}

func (s *Store) RecordTrigger(roomID int64, stageID string, startedAt time.Time, category string, decision TriggerDecision) {
	if s == nil || roomID <= 0 || strings.TrimSpace(stageID) == "" || strings.TrimSpace(decision.Key) == "" {
		return
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	stage := s.ensureStageLocked(roomID, stageID, startedAt, now)
	stage.decisionCount++
	s.recordProbabilityLocked(
		stage,
		category,
		decision.Key,
		decision.Name,
		float64(decision.Probability),
		decision.Triggered,
		now,
	)
	s.publishStageLocked(roomID)
}

func (s *Store) StageStats(roomID int64) StageStats {
	if s == nil || roomID <= 0 {
		return StageStats{RoomID: roomID, Items: []ProbabilityStat{}}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stageStatsLocked(roomID)
}

func (s *Store) StageStatsForTenant(roomID, tenantID int64) StageStats {
	if s == nil || roomID <= 0 {
		return StageStats{RoomID: roomID, Items: []ProbabilityStat{}}
	}
	result := s.StageStats(roomID)
	policy := s.Resolve(tenantID)
	return mergePolicyCatalog(result, policy)
}

func (s *Store) stageStatsLocked(roomID int64) StageStats {
	stage := s.stages[roomID]
	if stage == nil {
		return StageStats{
			RoomID:           roomID,
			Items:            []ProbabilityStat{},
			InteractionItems: append([]InteractionWindowStat(nil), s.interactionStats[roomID]...),
		}
	}
	result := StageStats{
		RoomID:           stage.roomID,
		StageID:          stage.stageID,
		StartedAt:        stage.startedAt,
		UpdatedAt:        stage.updatedAt,
		DecisionCount:    stage.decisionCount,
		Items:            make([]ProbabilityStat, 0, len(stage.items)),
		InteractionItems: append([]InteractionWindowStat(nil), s.interactionStats[roomID]...),
	}
	for _, item := range stage.items {
		copy := item.ProbabilityStat
		if copy.Samples > 0 {
			copy.AverageProbability = math.Round((item.sum/float64(copy.Samples))*100) / 100
		}
		result.Items = append(result.Items, copy)
	}
	sort.SliceStable(result.Items, func(i, j int) bool {
		if result.Items[i].Category == result.Items[j].Category {
			return result.Items[i].Key < result.Items[j].Key
		}
		return result.Items[i].Category < result.Items[j].Category
	})
	return result
}

func mergePolicyCatalog(result StageStats, policy Policy) StageStats {
	filtered := result.Items[:0]
	for _, item := range result.Items {
		if strings.EqualFold(strings.TrimSpace(item.Category), "interaction") {
			continue
		}
		filtered = append(filtered, item)
	}
	result.Items = filtered
	byIdentity := make(map[string]int, len(result.Items))
	for i := range result.Items {
		identity := strings.ToLower(strings.TrimSpace(result.Items[i].Category)) + ":" + strings.TrimSpace(result.Items[i].Key)
		byIdentity[identity] = i
	}

	ensure := func(category, key, name string, enabled bool, configured float64) {
		category = strings.ToLower(strings.TrimSpace(category))
		key = strings.TrimSpace(key)
		if category == "" || key == "" {
			return
		}
		identity := category + ":" + key
		if index, ok := byIdentity[identity]; ok {
			result.Items[index].Enabled = enabled
			result.Items[index].ConfiguredProbability = configured
			if strings.TrimSpace(result.Items[index].Name) == "" {
				result.Items[index].Name = strings.TrimSpace(name)
			}
			return
		}
		byIdentity[identity] = len(result.Items)
		result.Items = append(result.Items, ProbabilityStat{
			Category:              category,
			Key:                   key,
			Name:                  strings.TrimSpace(name),
			Enabled:               enabled,
			ConfiguredProbability: configured,
		})
	}

	for _, rule := range policy.Rules {
		if strings.EqualFold(strings.TrimSpace(rule.Category), "interaction") {
			continue
		}
		ensure(rule.Category, rule.Key, rule.Name, rule.Enabled, float64(rule.BaseProbability))
	}
	for _, option := range policy.Addressing {
		ensure("addressing", option.Key, option.Text, option.Enabled, float64(option.Probability))
	}

	order := func(category string) int {
		switch strings.ToLower(strings.TrimSpace(category)) {
		case "interaction":
			return 0
		case "interrupt":
			return 1
		case "resume":
			return 2
		case "addressing":
			return 3
		default:
			return 9
		}
	}
	sort.SliceStable(result.Items, func(i, j int) bool {
		leftOrder := order(result.Items[i].Category)
		rightOrder := order(result.Items[j].Category)
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return result.Items[i].Key < result.Items[j].Key
	})
	return result
}

func (s *Store) UpdateInteractionStats(roomID int64, items []InteractionWindowStat) {
	if s == nil || roomID <= 0 {
		return
	}
	copyItems := append([]InteractionWindowStat(nil), items...)
	sort.SliceStable(copyItems, func(i, j int) bool {
		return copyItems[i].Key < copyItems[j].Key
	})
	s.mu.Lock()
	s.interactionStats[roomID] = copyItems
	if stage := s.stages[roomID]; stage != nil {
		stage.updatedAt = time.Now().UTC()
	}
	s.publishStageLocked(roomID)
	s.mu.Unlock()
}

func (s *Store) SubscribeStats(roomID int64) (<-chan StageStats, func()) {
	ch := make(chan StageStats, 1)
	if s == nil || roomID <= 0 {
		close(ch)
		return ch, func() {}
	}
	s.mu.Lock()
	s.nextSubscriberID++
	id := s.nextSubscriberID
	if s.subscribers[roomID] == nil {
		s.subscribers[roomID] = make(map[uint64]chan StageStats)
	}
	s.subscribers[roomID][id] = ch
	ch <- s.stageStatsLocked(roomID)
	s.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			s.mu.Lock()
			if roomSubscribers := s.subscribers[roomID]; roomSubscribers != nil {
				if existing, ok := roomSubscribers[id]; ok {
					delete(roomSubscribers, id)
					close(existing)
				}
				if len(roomSubscribers) == 0 {
					delete(s.subscribers, roomID)
				}
			}
			s.mu.Unlock()
		})
	}
	return ch, cancel
}

func (s *Store) publishStageLocked(roomID int64) {
	roomSubscribers := s.subscribers[roomID]
	if len(roomSubscribers) == 0 {
		return
	}
	snapshot := s.stageStatsLocked(roomID)
	for _, ch := range roomSubscribers {
		select {
		case ch <- snapshot:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- snapshot:
			default:
			}
		}
	}
}

func (s *Store) ensureStageLocked(roomID int64, stageID string, startedAt, now time.Time) *stageAccumulator {
	stage := s.stages[roomID]
	if stage == nil || stage.stageID != stageID {
		if startedAt.IsZero() {
			startedAt = now
		}
		stage = &stageAccumulator{
			roomID:                 roomID,
			stageID:                stageID,
			startedAt:              startedAt.UTC(),
			updatedAt:              now,
			items:                  make(map[string]*probabilityAccumulator),
			lastSelectedByCategory: make(map[string]string),
			repeatCountByCategory:  make(map[string]int),
		}
		s.stages[roomID] = stage
	}
	if stage.lastSelectedByCategory == nil {
		stage.lastSelectedByCategory = make(map[string]string)
	}
	if stage.repeatCountByCategory == nil {
		stage.repeatCountByCategory = make(map[string]int)
	}
	stage.updatedAt = now
	return stage
}

func (s *Store) recordProbabilityLocked(
	stage *stageAccumulator,
	category, key, name string,
	probability float64,
	applied bool,
	now time.Time,
) {
	if stage == nil {
		return
	}
	category = strings.ToLower(strings.TrimSpace(category))
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if probability < 0 {
		probability = 0
	}
	if probability > 100 {
		probability = 100
	}
	probability = math.Round(probability*100) / 100
	identity := category + ":" + key
	item := stage.items[identity]
	if item == nil {
		item = &probabilityAccumulator{
			ProbabilityStat: ProbabilityStat{
				Category:           category,
				Key:                key,
				Name:               strings.TrimSpace(name),
				MinimumProbability: probability,
				MaximumProbability: probability,
			},
		}
		stage.items[identity] = item
	}
	if strings.TrimSpace(name) != "" {
		item.Name = strings.TrimSpace(name)
	}
	item.Samples++
	if applied {
		item.HitCount++
	}
	item.LastProbability = probability
	if probability < item.MinimumProbability {
		item.MinimumProbability = probability
	}
	if probability > item.MaximumProbability {
		item.MaximumProbability = probability
	}
	item.LastEvaluatedAt = now
	item.sum += probability
}

func adjustWeight(key string, weight int, signals Signals) int {
	key = strings.TrimSpace(key)
	switch key {
	case "welcome_named":
		switch {
		case signals.Entries30s >= 60:
			weight = weight * 20 / 100
		case signals.Entries30s >= 20:
			weight = weight * 50 / 100
		case signals.Entries30s <= 3:
			weight = weight * 180 / 100
		case signals.Entries30s <= 10:
			weight = weight * 130 / 100
		}
	case "welcome_batch":
		switch {
		case signals.Entries30s >= 60:
			// High entry flow lowers total welcome frequency, but batch welcome
			// still stays relatively more likely than named welcome.
			weight = weight * 60 / 100
		case signals.Entries30s >= 20:
			weight = weight * 80 / 100
		case signals.Entries30s <= 3:
			weight = weight * 110 / 100
		case signals.Entries30s <= 10:
			weight = weight * 90 / 100
		}
	case "reply_like":
		switch {
		case signals.Likes30s >= 300:
			weight = weight * 25 / 100
		case signals.Likes30s >= 100:
			weight = weight * 50 / 100
		}
	}
	if strings.EqualFold(strings.TrimSpace(signals.Heat), "OVERHEATED") && key != "reply_chat" {
		weight = weight * 60 / 100
	}
	if weight < 1 {
		return 1
	}
	return weight
}
