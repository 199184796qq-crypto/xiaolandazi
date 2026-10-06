// Package mainlinebrain contains the provider-neutral state contract for the
// continuous livestream mainline. It does not call an LLM, synthesize audio,
// or control Core. Those adapters are added after this state boundary is
// stable.
package mainlinebrain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type Mode string

const (
	ModePreview    Mode = "preview"
	ModeContinuous Mode = "continuous"
)

type Status string

const (
	StatusIdle    Status = "idle"
	StatusRunning Status = "running"
	StatusPaused  Status = "paused"
	StatusStopped Status = "stopped"
)

type MemoryTier string

const (
	MemoryPinned MemoryTier = "pinned"
	MemoryHot    MemoryTier = "hot"
	MemoryWarm   MemoryTier = "warm"
	MemoryCool   MemoryTier = "cool"
	MemoryCold   MemoryTier = "cold"
)

type MemoryCommitMode string

const (
	MemoryOnAccepted MemoryCommitMode = "accepted"
	MemoryOnPlayed   MemoryCommitMode = "played"
)

// MemoryItem is intentionally a content memory record, not the formal fact
// store. Formal facts and locked rules must be supplied separately on every
// generation request so a fuzzy memory can never replace current facts.
type MemoryItem struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind,omitempty"`
	Topic      string    `json:"topic,omitempty"`
	Purpose    string    `json:"purpose,omitempty"`
	Angle      string    `json:"angle,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	ExactText  string    `json:"exact_text,omitempty"`
	FactKeys   []string  `json:"fact_keys,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Importance float64   `json:"importance,omitempty"`
	Pinned     bool      `json:"pinned,omitempty"`
}

type MemoryPolicy struct {
	HotWindow  time.Duration `json:"hot_window"`
	WarmWindow time.Duration `json:"warm_window"`
	CoolWindow time.Duration `json:"cool_window"`
	MaxHot     int           `json:"max_hot"`
	MaxWarm    int           `json:"max_warm"`
	MaxCool    int           `json:"max_cool"`
	MaxCold    int           `json:"max_cold"`
}

func DefaultMemoryPolicy() MemoryPolicy {
	return MemoryPolicy{
		HotWindow:  2 * time.Minute,
		WarmWindow: 10 * time.Minute,
		CoolWindow: 30 * time.Minute,
		MaxHot:     8,
		MaxWarm:    12,
		MaxCool:    16,
		MaxCold:    12,
	}
}

type MemorySnapshot struct {
	Pinned []MemoryItem `json:"pinned,omitempty"`
	Hot    []MemoryItem `json:"hot,omitempty"`
	Warm   []MemoryItem `json:"warm,omitempty"`
	Cool   []MemoryItem `json:"cool,omitempty"`
	Cold   []MemoryItem `json:"cold,omitempty"`
}

func (s MemorySnapshot) All() []MemoryItem {
	result := make([]MemoryItem, 0, len(s.Pinned)+len(s.Hot)+len(s.Warm)+len(s.Cool)+len(s.Cold))
	result = append(result, s.Pinned...)
	result = append(result, s.Hot...)
	result = append(result, s.Warm...)
	result = append(result, s.Cool...)
	result = append(result, s.Cold...)
	return result
}

// BuildMemorySnapshot projects exact session memory into progressively less
// detailed windows. The source slice is not mutated. A pinned record is never
// downgraded, and a non-pinned record loses exact wording before it loses its
// topic or summary.
func BuildMemorySnapshot(items []MemoryItem, now time.Time, policy MemoryPolicy) MemorySnapshot {
	if now.IsZero() {
		now = time.Now()
	}
	policy = normalizeMemoryPolicy(policy)
	ordered := append([]MemoryItem(nil), items...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := memoryTimestamp(ordered[i]), memoryTimestamp(ordered[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		if ordered[i].Importance != ordered[j].Importance {
			return ordered[i].Importance > ordered[j].Importance
		}
		return ordered[i].ID < ordered[j].ID
	})

	var snapshot MemorySnapshot
	for _, original := range ordered {
		if strings.TrimSpace(original.ID) == "" {
			continue
		}
		if original.Pinned {
			snapshot.Pinned = append(snapshot.Pinned, redactMemory(original, MemoryPinned))
			continue
		}
		age := now.Sub(memoryTimestamp(original))
		if age < 0 {
			age = 0
		}
		switch {
		case age < policy.HotWindow:
			snapshot.Hot = append(snapshot.Hot, redactMemory(original, MemoryHot))
		case age < policy.WarmWindow:
			snapshot.Warm = append(snapshot.Warm, redactMemory(original, MemoryWarm))
		case age < policy.CoolWindow:
			snapshot.Cool = append(snapshot.Cool, redactMemory(original, MemoryCool))
		default:
			snapshot.Cold = append(snapshot.Cold, redactMemory(original, MemoryCold))
		}
	}
	snapshot.Hot = trimMemory(snapshot.Hot, policy.MaxHot)
	snapshot.Warm = trimMemory(snapshot.Warm, policy.MaxWarm)
	snapshot.Cool = trimMemory(snapshot.Cool, policy.MaxCool)
	snapshot.Cold = trimMemory(snapshot.Cold, policy.MaxCold)
	return snapshot
}

func normalizeMemoryPolicy(policy MemoryPolicy) MemoryPolicy {
	defaults := DefaultMemoryPolicy()
	if policy.HotWindow <= 0 {
		policy.HotWindow = defaults.HotWindow
	}
	if policy.WarmWindow <= policy.HotWindow {
		policy.WarmWindow = defaults.WarmWindow
	}
	if policy.CoolWindow <= policy.WarmWindow {
		policy.CoolWindow = defaults.CoolWindow
	}
	if policy.MaxHot <= 0 {
		policy.MaxHot = defaults.MaxHot
	}
	if policy.MaxWarm <= 0 {
		policy.MaxWarm = defaults.MaxWarm
	}
	if policy.MaxCool <= 0 {
		policy.MaxCool = defaults.MaxCool
	}
	if policy.MaxCold <= 0 {
		policy.MaxCold = defaults.MaxCold
	}
	return policy
}

func memoryTimestamp(item MemoryItem) time.Time {
	if !item.LastSeenAt.IsZero() {
		return item.LastSeenAt
	}
	return item.CreatedAt
}

func redactMemory(item MemoryItem, tier MemoryTier) MemoryItem {
	item.FactKeys = append([]string(nil), item.FactKeys...)
	switch tier {
	case MemoryPinned, MemoryHot:
		return item
	case MemoryWarm:
		item.ExactText = ""
	case MemoryCool:
		item.ExactText = ""
		item.Angle = ""
	case MemoryCold:
		item.ExactText = ""
		item.Angle = ""
		item.Summary = ""
		item.FactKeys = nil
	}
	return item
}

func trimMemory(items []MemoryItem, max int) []MemoryItem {
	if max <= 0 || len(items) <= max {
		return items
	}
	return items[:max]
}

type SignalOrigin string

const (
	SignalObserved  SignalOrigin = "observed"
	SignalInferred  SignalOrigin = "inferred"
	SignalSimulated SignalOrigin = "simulated"
	SignalUnknown   SignalOrigin = "unknown"
)

// RoomSignals never carries claims that may be spoken as facts. It is a
// scheduling input only, and its origin is persisted so simulation cannot be
// confused with a real room observation.
type RoomSignals struct {
	ObservedAt    time.Time    `json:"observed_at,omitempty"`
	Origin        SignalOrigin `json:"origin"`
	Confidence    float64      `json:"confidence,omitempty"`
	OnlineCount   *int         `json:"online_count,omitempty"`
	NewEntries30s *int         `json:"new_entries_30s,omitempty"`
	Comments30s   *int         `json:"comments_30s,omitempty"`
	Questions60s  *int         `json:"questions_60s,omitempty"`
	Orders60s     *int         `json:"orders_60s,omitempty"`
	Clicks60s     *int         `json:"clicks_60s,omitempty"`
}

type DirectiveStatus string

const (
	DirectiveActive    DirectiveStatus = "active"
	DirectiveCancelled DirectiveStatus = "cancelled"
	DirectiveCompleted DirectiveStatus = "completed"
	DirectiveExpired   DirectiveStatus = "expired"
)

type Directive struct {
	ID           string          `json:"id"`
	Text         string          `json:"text"`
	ParsedIntent string          `json:"parsed_intent,omitempty"`
	Topic        string          `json:"topic,omitempty"`
	Purpose      string          `json:"purpose,omitempty"`
	Priority     int             `json:"priority,omitempty"`
	StartsAt     time.Time       `json:"starts_at,omitempty"`
	ExpiresAt    time.Time       `json:"expires_at,omitempty"`
	Status       DirectiveStatus `json:"status"`
	CreatedAt    time.Time       `json:"created_at"`
	ConsumedAt   *time.Time      `json:"consumed_at,omitempty"`
	Source       string          `json:"source,omitempty"`
}

func (d Directive) IsActiveAt(now time.Time) bool {
	if d.Status != DirectiveActive || strings.TrimSpace(d.Text) == "" {
		return false
	}
	if !d.StartsAt.IsZero() && now.Before(d.StartsAt) {
		return false
	}
	return d.ExpiresAt.IsZero() || now.Before(d.ExpiresAt)
}

type EngagementKind string

const (
	EngagementCommentPrompt EngagementKind = "comment_prompt"
	EngagementPoll          EngagementKind = "poll"
	EngagementConfirmation  EngagementKind = "confirmation"
	EngagementLinkGuidance  EngagementKind = "link_guidance"
	EngagementFollowPrompt  EngagementKind = "follow_prompt"
)

type EngagementStatus string

const (
	EngagementIssued    EngagementStatus = "issued"
	EngagementResponded EngagementStatus = "responded"
	EngagementExpired   EngagementStatus = "expired"
	EngagementSkipped   EngagementStatus = "skipped"
)

type Engagement struct {
	ID             string           `json:"id"`
	Kind           EngagementKind   `json:"kind"`
	Purpose        string           `json:"purpose,omitempty"`
	Prompt         string           `json:"prompt"`
	Status         EngagementStatus `json:"status"`
	IssuedAt       time.Time        `json:"issued_at"`
	ExpiresAt      time.Time        `json:"expires_at"`
	CooldownUntil  time.Time        `json:"cooldown_until,omitempty"`
	ResponseCounts map[string]int   `json:"response_counts,omitempty"`
	RespondedAt    *time.Time       `json:"responded_at,omitempty"`
}

type ConversionLevel string

const (
	ConversionNatural  ConversionLevel = "natural"
	ConversionModerate ConversionLevel = "moderate"
	ConversionFocused  ConversionLevel = "focused"
	ConversionStrong   ConversionLevel = "strong"
)

type ConversionState struct {
	BaseIntensity int             `json:"base_intensity,omitempty"`
	Pressure      int             `json:"pressure"`
	Level         ConversionLevel `json:"level"`
	LastAction    string          `json:"last_action,omitempty"`
	LastActionAt  time.Time       `json:"last_action_at,omitempty"`
	CooldownUntil time.Time       `json:"cooldown_until,omitempty"`
}

type ConversionPressureInput struct {
	BaseIntensity      int     `json:"base_intensity"`
	OfferValidity      float64 `json:"offer_validity"`
	ReadinessSignal    float64 `json:"readiness_signal"`
	RepetitionPenalty  float64 `json:"repetition_penalty"`
	UncertaintyPenalty float64 `json:"uncertainty_penalty"`
}

func ComputeConversionPressure(input ConversionPressureInput) int {
	base := clampInt(input.BaseIntensity, 0, 100)
	score := float64(base) + 20*clamp01(input.OfferValidity) + 20*clamp01(input.ReadinessSignal)
	score -= 25 * clamp01(input.RepetitionPenalty)
	score -= 15 * clamp01(input.UncertaintyPenalty)
	return clampInt(int(score+0.5), 0, 100)
}

func ConversionLevelForPressure(pressure int) ConversionLevel {
	switch {
	case pressure >= 75:
		return ConversionStrong
	case pressure >= 50:
		return ConversionFocused
	case pressure >= 25:
		return ConversionModerate
	default:
		return ConversionNatural
	}
}

type SegmentStatus string

const (
	SegmentAccepted  SegmentStatus = "accepted"
	SegmentQueued    SegmentStatus = "queued"
	SegmentPlayed    SegmentStatus = "played"
	SegmentDiscarded SegmentStatus = "discarded"
	SegmentFailed    SegmentStatus = "failed"
)

type SegmentTask struct {
	ID                 string          `json:"id"`
	SessionID          string          `json:"session_id"`
	Sequence           int             `json:"sequence"`
	Topic              string          `json:"topic,omitempty"`
	Purpose            string          `json:"purpose,omitempty"`
	PrimaryFactKeys    []string        `json:"primary_fact_keys,omitempty"`
	SupportFactKeys    []string        `json:"support_fact_keys,omitempty"`
	PreviousTail       string          `json:"previous_tail,omitempty"`
	TargetChars        int             `json:"target_chars,omitempty"`
	MinChars           int             `json:"min_chars,omitempty"`
	MaxChars           int             `json:"max_chars,omitempty"`
	OpeningAllowed     bool            `json:"opening_allowed"`
	ClosingAllowed     bool            `json:"closing_allowed"`
	ActiveEngagementID string          `json:"active_engagement_id,omitempty"`
	ConversionLevel    ConversionLevel `json:"conversion_level,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
}

type SegmentRecord struct {
	Task        SegmentTask   `json:"task"`
	Text        string        `json:"text"`
	ActualChars int           `json:"actual_chars"`
	Status      SegmentStatus `json:"status"`
	AcceptedAt  time.Time     `json:"accepted_at,omitempty"`
	QueuedAt    time.Time     `json:"queued_at,omitempty"`
	PlayedAt    time.Time     `json:"played_at,omitempty"`
	DiscardedAt time.Time     `json:"discarded_at,omitempty"`
	Failure     string        `json:"failure,omitempty"`
}

type SessionState struct {
	Version           int64            `json:"version"`
	ID                string           `json:"id"`
	TenantID          int64            `json:"tenant_id,omitempty"`
	RoomID            int64            `json:"room_id,omitempty"`
	PlanID            int64            `json:"plan_id,omitempty"`
	Mode              Mode             `json:"mode"`
	Status            Status           `json:"status"`
	MemoryCommitMode  MemoryCommitMode `json:"memory_commit_mode"`
	StartedAt         time.Time        `json:"started_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
	Round             int              `json:"round"`
	NextSequence      int              `json:"next_sequence"`
	CurrentTopic      string           `json:"current_topic,omitempty"`
	CurrentPurpose    string           `json:"current_purpose,omitempty"`
	PreviousTail      string           `json:"previous_tail,omitempty"`
	Memory            []MemoryItem     `json:"memory,omitempty"`
	Segments          []SegmentRecord  `json:"segments,omitempty"`
	Directive         *Directive       `json:"directive,omitempty"`
	Engagement        *Engagement      `json:"engagement,omitempty"`
	EngagementHistory []Engagement     `json:"engagement_history,omitempty"`
	Conversion        ConversionState  `json:"conversion"`
	Signals           RoomSignals      `json:"signals"`
}

func NewSessionState(id string, mode Mode, now time.Time) SessionState {
	if now.IsZero() {
		now = time.Now()
	}
	if mode == "" {
		mode = ModePreview
	}
	return SessionState{
		Version:          1,
		ID:               strings.TrimSpace(id),
		Mode:             mode,
		Status:           StatusIdle,
		MemoryCommitMode: memoryCommitModeFor(mode),
		StartedAt:        now,
		UpdatedAt:        now,
		NextSequence:     1,
		Conversion:       ConversionState{Level: ConversionNatural},
	}
}

func memoryCommitModeFor(mode Mode) MemoryCommitMode {
	if mode == ModeContinuous {
		return MemoryOnPlayed
	}
	return MemoryOnAccepted
}

func (s *SessionState) Touch(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	s.UpdatedAt = now
	s.Version++
}

func (s SessionState) MemorySnapshot(now time.Time, policy MemoryPolicy) MemorySnapshot {
	return BuildMemorySnapshot(s.Memory, now, policy)
}

func (s *SessionState) AddMemory(item MemoryItem) error {
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		return errors.New("memory id is required")
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = s.UpdatedAt
	}
	if item.LastSeenAt.IsZero() {
		item.LastSeenAt = item.CreatedAt
	}
	for index := range s.Memory {
		if s.Memory[index].ID == item.ID {
			s.Memory[index] = item
			touchAt := s.UpdatedAt
			if touchAt.IsZero() || item.LastSeenAt.After(touchAt) {
				touchAt = item.LastSeenAt
			}
			s.Touch(touchAt)
			return nil
		}
	}
	s.Memory = append(s.Memory, item)
	touchAt := s.UpdatedAt
	if touchAt.IsZero() || item.LastSeenAt.After(touchAt) {
		touchAt = item.LastSeenAt
	}
	s.Touch(touchAt)
	return nil
}

func (s *SessionState) SetDirective(d Directive, now time.Time) error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Text) == "" {
		return errors.New("directive id and text are required")
	}
	if d.Priority < 0 || d.Priority > 100 {
		return errors.New("directive priority must be between 0 and 100")
	}
	if !d.StartsAt.IsZero() && !d.ExpiresAt.IsZero() && !d.ExpiresAt.After(d.StartsAt) {
		return errors.New("directive expiry must be after its start")
	}
	if d.Status == "" {
		d.Status = DirectiveActive
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	s.Directive = &d
	s.Touch(now)
	return nil
}

func (s *SessionState) ActiveDirective(now time.Time) *Directive {
	if s.Directive == nil {
		return nil
	}
	if !s.Directive.IsActiveAt(now) {
		if s.Directive.Status == DirectiveActive && !s.Directive.ExpiresAt.IsZero() && !now.Before(s.Directive.ExpiresAt) {
			s.Directive.Status = DirectiveExpired
			s.Touch(now)
		}
		return nil
	}
	copy := *s.Directive
	return &copy
}

func (s *SessionState) IssueEngagement(item Engagement, now time.Time) error {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Prompt) == "" {
		return errors.New("engagement id and prompt are required")
	}
	if s.Engagement != nil && s.Engagement.Status == EngagementIssued && now.Before(s.Engagement.ExpiresAt) {
		return errors.New("an active engagement is already waiting")
	}
	if s.Engagement != nil && !s.Engagement.CooldownUntil.IsZero() && now.Before(s.Engagement.CooldownUntil) {
		return errors.New("engagement is in cooldown")
	}
	if item.IssuedAt.IsZero() {
		item.IssuedAt = now
	}
	if item.ExpiresAt.IsZero() {
		item.ExpiresAt = item.IssuedAt.Add(12 * time.Second)
	}
	if !item.ExpiresAt.After(item.IssuedAt) {
		return errors.New("engagement expiry must be after issue time")
	}
	item.Status = EngagementIssued
	s.Engagement = &item
	s.Touch(now)
	return nil
}

func (s *SessionState) ObserveEngagement(id string, counts map[string]int, now time.Time) error {
	if s.Engagement == nil || s.Engagement.ID != strings.TrimSpace(id) {
		return errors.New("engagement not found")
	}
	if s.Engagement.Status != EngagementIssued && s.Engagement.Status != EngagementResponded {
		return errors.New("engagement is no longer accepting responses")
	}
	copyCounts := make(map[string]int, len(counts))
	for key, value := range counts {
		if value >= 0 {
			copyCounts[key] = value
		}
	}
	s.Engagement.ResponseCounts = copyCounts
	s.Engagement.Status = EngagementResponded
	s.Engagement.RespondedAt = &now
	s.Engagement.CooldownUntil = now.Add(30 * time.Second)
	s.EngagementHistory = append(s.EngagementHistory, *s.Engagement)
	s.Touch(now)
	return nil
}

func (s *SessionState) ExpireEngagement(now time.Time) {
	if s.Engagement == nil || s.Engagement.Status != EngagementIssued || now.Before(s.Engagement.ExpiresAt) {
		return
	}
	s.Engagement.Status = EngagementExpired
	s.Engagement.CooldownUntil = now.Add(15 * time.Second)
	s.EngagementHistory = append(s.EngagementHistory, *s.Engagement)
	s.Touch(now)
}

func (s *SessionState) AcceptSegment(task SegmentTask, text string, now time.Time) error {
	if strings.TrimSpace(task.ID) == "" {
		return errors.New("segment task id is required")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("segment text is required")
	}
	if task.SessionID != "" && task.SessionID != s.ID {
		return errors.New("segment belongs to another session")
	}
	for _, existing := range s.Segments {
		if existing.Task.ID == task.ID {
			return errors.New("segment task already exists")
		}
	}
	if task.Sequence <= 0 {
		task.Sequence = s.NextSequence
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	record := SegmentRecord{Task: task, Text: text, ActualChars: utf8.RuneCountInString(text), Status: SegmentAccepted, AcceptedAt: now}
	s.Segments = append(s.Segments, record)
	if task.Sequence >= s.NextSequence {
		s.NextSequence = task.Sequence + 1
	}
	s.CurrentTopic = task.Topic
	s.CurrentPurpose = task.Purpose
	s.PreviousTail = runeTail(text, 240)
	if s.MemoryCommitMode == MemoryOnAccepted {
		s.rememberSegment(record, now)
	}
	s.Touch(now)
	return nil
}

func (s *SessionState) QueueSegment(id string, now time.Time) error {
	return s.transitionSegment(id, SegmentQueued, now)
}

func (s *SessionState) MarkPlayed(id string, now time.Time) error {
	for index := range s.Segments {
		if s.Segments[index].Task.ID != strings.TrimSpace(id) {
			continue
		}
		if s.Segments[index].Status != SegmentQueued && s.Segments[index].Status != SegmentAccepted {
			return errors.New("segment is not queued")
		}
		s.Segments[index].Status = SegmentPlayed
		s.Segments[index].PlayedAt = now
		if s.MemoryCommitMode == MemoryOnPlayed {
			s.rememberSegment(s.Segments[index], now)
		}
		s.Touch(now)
		return nil
	}
	return errors.New("segment not found")
}

func (s *SessionState) DiscardSegment(id, reason string, now time.Time) error {
	for index := range s.Segments {
		if s.Segments[index].Task.ID != strings.TrimSpace(id) {
			continue
		}
		if s.Segments[index].Status == SegmentPlayed {
			return errors.New("played segment cannot be discarded")
		}
		s.Segments[index].Status = SegmentDiscarded
		s.Segments[index].DiscardedAt = now
		s.Segments[index].Failure = strings.TrimSpace(reason)
		s.Touch(now)
		return nil
	}
	return errors.New("segment not found")
}

func (s *SessionState) transitionSegment(id string, target SegmentStatus, now time.Time) error {
	for index := range s.Segments {
		if s.Segments[index].Task.ID != strings.TrimSpace(id) {
			continue
		}
		if target == SegmentQueued && s.Segments[index].Status != SegmentAccepted {
			return errors.New("only an accepted segment can be queued")
		}
		s.Segments[index].Status = target
		s.Segments[index].QueuedAt = now
		s.Touch(now)
		return nil
	}
	return errors.New("segment not found")
}

func (s *SessionState) rememberSegment(record SegmentRecord, now time.Time) {
	_ = s.AddMemory(MemoryItem{
		ID:         "segment:" + record.Task.ID,
		Kind:       "segment",
		Topic:      record.Task.Topic,
		Purpose:    record.Task.Purpose,
		Summary:    record.Task.Purpose,
		ExactText:  record.Text,
		FactKeys:   append(append([]string(nil), record.Task.PrimaryFactKeys...), record.Task.SupportFactKeys...),
		CreatedAt:  now,
		LastSeenAt: now,
	})
}

func runeTail(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if limit <= 0 || len(runes) <= limit {
		return string(runes)
	}
	return string(runes[len(runes)-limit:])
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func (s SessionState) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return errors.New("session id is required")
	}
	if s.Mode != ModePreview && s.Mode != ModeContinuous {
		return fmt.Errorf("unsupported session mode %q", s.Mode)
	}
	if s.MemoryCommitMode != MemoryOnAccepted && s.MemoryCommitMode != MemoryOnPlayed {
		return fmt.Errorf("unsupported memory commit mode %q", s.MemoryCommitMode)
	}
	return nil
}
