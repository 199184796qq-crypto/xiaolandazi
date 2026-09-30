package speechmission

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type State string

const (
	StateCreated             State = "CREATED"
	StatePlanningInteraction State = "PLANNING_INTERACTION"
	StatePlanningInterrupt   State = "PLANNING_INTERRUPT"
	StatePlanningResume      State = "PLANNING_RESUME"
	StatePlanningExpression  State = "PLANNING_EXPRESSION"
	StateGeneratingText      State = "GENERATING_TEXT"
	StateValidatingText      State = "VALIDATING_TEXT"
	StateSynthesizingTTS     State = "SYNTHESIZING_TTS"
	StateWaitingCutPoint     State = "WAITING_CUT_POINT"
	StateDispatched          State = "DISPATCHED"
	StateCompleted           State = "COMPLETED"
	StateFailed              State = "FAILED"
	StateCancelled           State = "CANCELLED"
	StateExpired             State = "EXPIRED"
	StateSuperseded          State = "SUPERSEDED"
)

type EventContext struct {
	Kind          string   `json:"kind,omitempty"`
	Topic         string   `json:"topic,omitempty"`
	Title         string   `json:"title,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	Questions     []string `json:"questions,omitempty"`
	Nicknames     []string `json:"nicknames,omitempty"`
	EventCount    int      `json:"event_count,omitempty"`
	WindowSeconds int      `json:"window_seconds,omitempty"`
}

type MainlineContext struct {
	Before          string `json:"before,omitempty"`
	After           string `json:"after,omitempty"`
	ResumeSegmentID string `json:"resume_segment_id,omitempty"`
	SwitchAtMS      int    `json:"switch_at_ms,omitempty"`
}

type QuestionDebtPlan struct {
	Topic           string    `json:"topic,omitempty"`
	FirstSeenAt     time.Time `json:"first_seen_at,omitempty"`
	RepeatCount     int       `json:"repeat_count,omitempty"`
	UniqueUsers     int       `json:"unique_users,omitempty"`
	WaitingSeconds  int       `json:"waiting_seconds,omitempty"`
	BusinessValue   float64   `json:"business_value,omitempty"`
	CurrentPriority float64   `json:"current_priority,omitempty"`
}

type InteractionDecisionPlan struct {
	Handle           bool              `json:"handle"`
	PrimaryEvent     string            `json:"primary_event,omitempty"`
	MergedEventIDs   []int64           `json:"merged_event_ids,omitempty"`
	EventValue       float64           `json:"event_value,omitempty"`
	ValueLevel       string            `json:"value_level,omitempty"`
	Reason           string            `json:"reason,omitempty"`
	DeadlineAt       time.Time         `json:"deadline_at,omitempty"`
	BudgetLevel      string            `json:"budget_level,omitempty"`
	BudgetAllowed    bool              `json:"budget_allowed"`
	Heat             string            `json:"heat,omitempty"`
	PreferenceFactor float64           `json:"preference_factor,omitempty"`
	QuestionDebt     *QuestionDebtPlan `json:"question_debt,omitempty"`
}

type InteractionPlan struct {
	Kind          string                  `json:"kind,omitempty"`
	Goal          string                  `json:"goal,omitempty"`
	EventCount    int                     `json:"event_count,omitempty"`
	WindowSeconds int                     `json:"window_seconds,omitempty"`
	Decision      InteractionDecisionPlan `json:"decision"`
	Required      bool                    `json:"required"`
}

type InterruptPlan struct {
	Strategy string `json:"strategy,omitempty"`
	Name     string `json:"name,omitempty"`
	Guidance string `json:"guidance,omitempty"`
	Required bool   `json:"required"`
}

type ResumePlan struct {
	Strategy              string   `json:"strategy,omitempty"`
	Name                  string   `json:"name,omitempty"`
	Guidance              string   `json:"guidance,omitempty"`
	ResumeMainline        string   `json:"resume_mainline,omitempty"`
	ResumeSegmentID       string   `json:"resume_segment_id,omitempty"`
	CutAfterSegment       string   `json:"cut_after_segment,omitempty"`
	OriginalResumeSegment string   `json:"original_resume_segment,omitempty"`
	CoveredSegments       []string `json:"covered_segments,omitempty"`
	PlannedResumeSegment  string   `json:"planned_resume_segment,omitempty"`
	ActualResumeSegment   string   `json:"actual_resume_segment,omitempty"`
	SkipCount             int      `json:"skip_count,omitempty"`
	PlannedResumeAtMS     int      `json:"planned_resume_at_ms,omitempty"`
	ActualResumeAtMS      int      `json:"actual_resume_at_ms,omitempty"`
	ResumeReason          string   `json:"resume_reason,omitempty"`
	ResumePreview         string   `json:"resume_preview,omitempty"`
	SkippedPreviews       []string `json:"skipped_previews,omitempty"`
	DedupTriggered        bool     `json:"dedup_triggered,omitempty"`
	DuplicateScore        float64  `json:"duplicate_score,omitempty"`
	BridgeText            string   `json:"bridge_text,omitempty"`
	Required              bool     `json:"required"`
}

type OpeningPlan struct {
	Intent   string `json:"intent,omitempty"`
	Name     string `json:"name,omitempty"`
	Guidance string `json:"guidance,omitempty"`
	Required bool   `json:"required"`
}

type AddressingPlan struct {
	Mode              string   `json:"mode,omitempty"`
	Candidate         string   `json:"candidate,omitempty"`
	Key               string   `json:"key,omitempty"`
	Preference        string   `json:"preference,omitempty"`
	NamedCandidates   []string `json:"named_candidates,omitempty"`
	SelectedNames     []string `json:"selected_names,omitempty"`
	PreferredTerms    []string `json:"preferred_terms,omitempty"`
	BlockedTerms      []string `json:"blocked_terms,omitempty"`
	GroupLabel        string   `json:"group_label,omitempty"`
	MaxNamedCount     int      `json:"max_named_count,omitempty"`
	RecentNamePenalty float64  `json:"recent_name_penalty,omitempty"`
	TargetRate        int      `json:"target_rate,omitempty"`
	SelectedByRate    bool     `json:"selected_by_rate"`
	Optional          bool     `json:"optional"`
}

type HumanTraitPlan struct {
	Persona          string `json:"persona,omitempty"`
	Emotion          string `json:"emotion,omitempty"`
	Pace             string `json:"pace,omitempty"`
	Humor            string `json:"humor,omitempty"`
	MaxReactionCount int    `json:"max_reaction_count,omitempty"`
	Instruction      string `json:"instruction,omitempty"`
}

type HumanStatePlan struct {
	Heat        string     `json:"heat,omitempty"`
	Progress    string     `json:"progress,omitempty"`
	Atmosphere  string     `json:"atmosphere,omitempty"`
	MissionKind string     `json:"mission_kind,omitempty"`
	HostState   string     `json:"host_state,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type HumanReactionPlan struct {
	Strategy        string    `json:"strategy,omitempty"`
	Kind            string    `json:"kind,omitempty"`
	Delivery        string    `json:"delivery,omitempty"`
	Instruction     string    `json:"instruction,omitempty"`
	AssetKey        string    `json:"asset_key,omitempty"`
	MaxCount        int       `json:"max_count,omitempty"`
	Enabled         bool      `json:"enabled"`
	Reason          string    `json:"reason,omitempty"`
	Source          string    `json:"source,omitempty"`
	RuleID          string    `json:"rule_id,omitempty"`
	Intensity       float64   `json:"intensity,omitempty"`
	Channel         string    `json:"channel,omitempty"`
	CooldownSeconds int64     `json:"cooldown_seconds,omitempty"`
	ExpiresAt       time.Time `json:"expires_at,omitempty"`
}

type HumanStylePlan struct {
	Mode     string            `json:"mode,omitempty"`
	Strategy string            `json:"strategy,omitempty"`
	Kind     string            `json:"kind,omitempty"`
	Delivery string            `json:"delivery,omitempty"`
	Enabled  bool              `json:"enabled"`
	Guidance string            `json:"guidance,omitempty"`
	Reason   string            `json:"reason,omitempty"`
	Emotion  string            `json:"emotion,omitempty"`
	Pace     string            `json:"pace,omitempty"`
	Trait    HumanTraitPlan    `json:"trait"`
	State    HumanStatePlan    `json:"state"`
	Reaction HumanReactionPlan `json:"reaction"`
}

type Constraint struct {
	Stage    string `json:"stage,omitempty"`
	Key      string `json:"key,omitempty"`
	Name     string `json:"name,omitempty"`
	Guidance string `json:"guidance,omitempty"`
	Required bool   `json:"required"`
}

type TTSDirective struct {
	Provider    string  `json:"provider,omitempty"`
	Model       string  `json:"model,omitempty"`
	VoiceID     string  `json:"voice_id,omitempty"`
	Rate        float64 `json:"rate,omitempty"`
	Instruction string  `json:"instruction,omitempty"`
	AudioURL    string  `json:"audio_url,omitempty"`
}

type TraceEvent struct {
	At     time.Time `json:"at"`
	State  State     `json:"state"`
	Action string    `json:"action,omitempty"`
	Note   string    `json:"note,omitempty"`
}

type Mission struct {
	ID               string          `json:"id"`
	DecisionID       string          `json:"decision_id"`
	TenantID         int64           `json:"tenant_id"`
	RoomID           int64           `json:"room_id"`
	RuntimeSessionID int64           `json:"runtime_session_id,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	PlanFrozenAt     *time.Time      `json:"plan_frozen_at,omitempty"`
	State            State           `json:"state"`
	Event            EventContext    `json:"event"`
	Mainline         MainlineContext `json:"mainline"`
	Interaction      InteractionPlan `json:"interaction"`
	Interrupt        InterruptPlan   `json:"interrupt"`
	Resume           ResumePlan      `json:"resume"`
	Opening          OpeningPlan     `json:"opening"`
	Addressing       AddressingPlan  `json:"addressing"`
	HumanStyle       HumanStylePlan  `json:"human_style"`
	AppliedStages    []string        `json:"applied_stages,omitempty"`
	Constraints      []Constraint    `json:"constraints,omitempty"`
	GeneratedText    string          `json:"generated_text,omitempty"`
	TTS              TTSDirective    `json:"tts"`
	FailureReason    string          `json:"failure_reason,omitempty"`
	Trace            []TraceEvent    `json:"trace,omitempty"`
}

type Registry struct {
	mu       sync.RWMutex
	missions map[string]*Mission
}

func New() *Registry {
	return &Registry{missions: make(map[string]*Mission)}
}

func (r *Registry) Ensure(input Mission) Mission {
	if r == nil {
		return cloneMission(input)
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = strings.TrimSpace(input.DecisionID)
	}
	if id == "" {
		return cloneMission(input)
	}
	now := time.Now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing := r.missions[id]; existing != nil {
		return cloneMission(*existing)
	}
	input.ID = id
	if strings.TrimSpace(input.DecisionID) == "" {
		input.DecisionID = id
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = now
	} else {
		input.CreatedAt = input.CreatedAt.UTC()
	}
	input.UpdatedAt = now
	if input.State == "" {
		input.State = StateCreated
	}
	input.Trace = append(input.Trace, TraceEvent{At: now, State: input.State, Action: "created"})
	copy := cloneMission(input)
	r.missions[id] = &copy
	return cloneMission(copy)
}

func (r *Registry) Update(id string, mutate func(*Mission)) (Mission, bool) {
	if r == nil {
		return Mission{}, false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Mission{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	mission := r.missions[id]
	if mission == nil {
		return Mission{}, false
	}
	if mutate != nil {
		mutate(mission)
	}
	mission.UpdatedAt = time.Now().UTC()
	return cloneMission(*mission), true
}

func (r *Registry) Transition(id string, state State, action, note string) (Mission, bool) {
	return r.Update(id, func(mission *Mission) {
		if state != "" {
			mission.State = state
		}
		now := time.Now().UTC()
		mission.Trace = append(mission.Trace, TraceEvent{
			At:     now,
			State:  mission.State,
			Action: strings.TrimSpace(action),
			Note:   strings.TrimSpace(note),
		})
		if mission.State == StateFailed {
			mission.FailureReason = strings.TrimSpace(note)
		}
	})
}

func (r *Registry) Snapshot(id string) (Mission, bool) {
	if r == nil {
		return Mission{}, false
	}
	id = strings.TrimSpace(id)
	r.mu.RLock()
	mission := r.missions[id]
	if mission == nil {
		r.mu.RUnlock()
		return Mission{}, false
	}
	copy := cloneMission(*mission)
	r.mu.RUnlock()
	return copy, true
}

func (r *Registry) RoomSnapshots(roomID int64) []Mission {
	if r == nil || roomID <= 0 {
		return []Mission{}
	}
	r.mu.RLock()
	result := make([]Mission, 0)
	for _, mission := range r.missions {
		if mission != nil && mission.RoomID == roomID {
			result = append(result, cloneMission(*mission))
		}
	}
	r.mu.RUnlock()
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

func cloneMission(input Mission) Mission {
	input.Event.Questions = append([]string(nil), input.Event.Questions...)
	input.Event.Nicknames = append([]string(nil), input.Event.Nicknames...)
	input.Interaction.Decision.MergedEventIDs = append([]int64(nil), input.Interaction.Decision.MergedEventIDs...)
	if input.Interaction.Decision.QuestionDebt != nil {
		copyDebt := *input.Interaction.Decision.QuestionDebt
		input.Interaction.Decision.QuestionDebt = &copyDebt
	}
	input.AppliedStages = append([]string(nil), input.AppliedStages...)
	input.Constraints = append([]Constraint(nil), input.Constraints...)
	input.Resume.SkippedPreviews = append([]string(nil), input.Resume.SkippedPreviews...)
	input.Resume.CoveredSegments = append([]string(nil), input.Resume.CoveredSegments...)
	input.Addressing.NamedCandidates = append([]string(nil), input.Addressing.NamedCandidates...)
	input.Addressing.SelectedNames = append([]string(nil), input.Addressing.SelectedNames...)
	input.Addressing.PreferredTerms = append([]string(nil), input.Addressing.PreferredTerms...)
	input.Addressing.BlockedTerms = append([]string(nil), input.Addressing.BlockedTerms...)
	if input.HumanStyle.State.ExpiresAt != nil {
		value := input.HumanStyle.State.ExpiresAt.UTC()
		input.HumanStyle.State.ExpiresAt = &value
	}
	input.Trace = append([]TraceEvent(nil), input.Trace...)
	if input.PlanFrozenAt != nil {
		value := input.PlanFrozenAt.UTC()
		input.PlanFrozenAt = &value
	}
	return input
}
