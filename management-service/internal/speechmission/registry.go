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
	EventCount    int      `json:"event_count,omitempty"`
	WindowSeconds int      `json:"window_seconds,omitempty"`
}

type MainlineContext struct {
	Before          string `json:"before,omitempty"`
	After           string `json:"after,omitempty"`
	ResumeSegmentID string `json:"resume_segment_id,omitempty"`
	SwitchAtMS      int    `json:"switch_at_ms,omitempty"`
}

type InteractionPlan struct {
	Kind          string `json:"kind,omitempty"`
	Goal          string `json:"goal,omitempty"`
	EventCount    int    `json:"event_count,omitempty"`
	WindowSeconds int    `json:"window_seconds,omitempty"`
	Required      bool   `json:"required"`
}

type InterruptPlan struct {
	Strategy string `json:"strategy,omitempty"`
	Name     string `json:"name,omitempty"`
	Guidance string `json:"guidance,omitempty"`
	Required bool   `json:"required"`
}

type ResumePlan struct {
	Strategy          string   `json:"strategy,omitempty"`
	Name              string   `json:"name,omitempty"`
	Guidance          string   `json:"guidance,omitempty"`
	ResumeMainline    string   `json:"resume_mainline,omitempty"`
	ResumeSegmentID   string   `json:"resume_segment_id,omitempty"`
	PlannedResumeAtMS int      `json:"planned_resume_at_ms,omitempty"`
	ActualResumeAtMS  int      `json:"actual_resume_at_ms,omitempty"`
	ResumeReason      string   `json:"resume_reason,omitempty"`
	ResumePreview     string   `json:"resume_preview,omitempty"`
	SkippedPreviews   []string `json:"skipped_previews,omitempty"`
	DedupTriggered    bool     `json:"dedup_triggered,omitempty"`
	DuplicateScore    float64  `json:"duplicate_score,omitempty"`
	BridgeText        string   `json:"bridge_text,omitempty"`
	Required          bool     `json:"required"`
}

type AddressingPlan struct {
	Candidate string `json:"candidate,omitempty"`
	Key       string `json:"key,omitempty"`
	Optional  bool   `json:"optional"`
}

type HumanStylePlan struct {
	Mode     string `json:"mode,omitempty"`
	Strategy string `json:"strategy,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Delivery string `json:"delivery,omitempty"`
	Enabled  bool   `json:"enabled"`
	Guidance string `json:"guidance,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Emotion  string `json:"emotion,omitempty"`
	Pace     string `json:"pace,omitempty"`
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
	input.AppliedStages = append([]string(nil), input.AppliedStages...)
	input.Constraints = append([]Constraint(nil), input.Constraints...)
	input.Resume.SkippedPreviews = append([]string(nil), input.Resume.SkippedPreviews...)
	input.Trace = append([]TraceEvent(nil), input.Trace...)
	if input.PlanFrozenAt != nil {
		value := input.PlanFrozenAt.UTC()
		input.PlanFrozenAt = &value
	}
	return input
}
