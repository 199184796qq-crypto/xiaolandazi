package speechruntime

import (
	"errors"
	"strings"
	"sync"
	"time"
)

type Track string

const (
	TrackMainline  Track = "mainline"
	TrackInterrupt Track = "interrupt"
)

type Status string

const (
	StatusIdle      Status = "idle"
	StatusReady     Status = "ready"
	StatusPlaying   Status = "playing"
	StatusPaused    Status = "paused"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

type TrackState struct {
	Status       Status     `json:"status"`
	Text         string     `json:"text,omitempty"`
	QuestionText string     `json:"question_text,omitempty"`
	ReplyText    string     `json:"reply_text,omitempty"`
	Source       string     `json:"source,omitempty"`
	AudioURL     string     `json:"audio_url,omitempty"`
	DecisionID   string     `json:"decision_id,omitempty"`
	SpeechTaskID string     `json:"speech_task_id,omitempty"`
	SwitchAtMS   *int       `json:"switch_at_ms,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
}

type Snapshot struct {
	RoomID    int64      `json:"room_id"`
	Revision  uint64     `json:"revision"`
	Mainline  TrackState `json:"mainline"`
	Interrupt TrackState `json:"interrupt"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type UpdateInput struct {
	Track        Track      `json:"track"`
	Status       Status     `json:"status"`
	Text         string     `json:"text,omitempty"`
	QuestionText string     `json:"question_text,omitempty"`
	ReplyText    string     `json:"reply_text,omitempty"`
	Source       string     `json:"source,omitempty"`
	AudioURL     string     `json:"audio_url,omitempty"`
	DecisionID   string     `json:"decision_id,omitempty"`
	SpeechTaskID string     `json:"speech_task_id,omitempty"`
	SwitchAtMS   *int       `json:"switch_at_ms,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
}

type roomState struct {
	revision  uint64
	mainline  TrackState
	interrupt TrackState
	updatedAt *time.Time
}

type Registry struct {
	mu    sync.RWMutex
	rooms map[int64]*roomState
	now   func() time.Time
}

func New() *Registry {
	return newRegistry(time.Now)
}

func newRegistry(now func() time.Time) *Registry {
	if now == nil {
		now = time.Now
	}
	return &Registry{
		rooms: make(map[int64]*roomState),
		now:   now,
	}
}

func (r *Registry) Snapshot(roomID int64) (Snapshot, error) {
	if roomID <= 0 {
		return Snapshot{}, errors.New("room_id must be positive")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	state := r.rooms[roomID]
	if state == nil {
		return Snapshot{
			RoomID:    roomID,
			Mainline:  TrackState{Status: StatusIdle},
			Interrupt: TrackState{Status: StatusIdle},
		}, nil
	}
	return snapshotLocked(roomID, state), nil
}

func (r *Registry) Update(roomID int64, input UpdateInput) (Snapshot, error) {
	if roomID <= 0 {
		return Snapshot{}, errors.New("room_id must be positive")
	}
	track, err := normalizeTrack(input.Track)
	if err != nil {
		return Snapshot{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.rooms[roomID]
	if state == nil {
		state = &roomState{
			mainline:  TrackState{Status: StatusIdle},
			interrupt: TrackState{Status: StatusIdle},
		}
		r.rooms[roomID] = state
	}

	target := &state.mainline
	if track == TrackInterrupt {
		target = &state.interrupt
	}

	status := input.Status
	if status == "" {
		status = target.Status
		if status == "" {
			status = StatusReady
		}
	}
	if !validStatus(status) {
		return Snapshot{}, errors.New("invalid speech runtime status")
	}

	now := r.now().UTC()
	if status == StatusIdle {
		*target = TrackState{Status: StatusIdle, UpdatedAt: timePtr(now)}
	} else {
		startedAt := input.StartedAt
		if startedAt == nil && target.StartedAt != nil {
			value := target.StartedAt.UTC()
			startedAt = &value
		}
		if startedAt == nil && status == StatusPlaying {
			startedAt = timePtr(now)
		}
		*target = TrackState{
			Status:       status,
			Text:         strings.TrimSpace(input.Text),
			QuestionText: strings.TrimSpace(input.QuestionText),
			ReplyText:    strings.TrimSpace(input.ReplyText),
			Source:       strings.TrimSpace(input.Source),
			AudioURL:     strings.TrimSpace(input.AudioURL),
			DecisionID:   strings.TrimSpace(input.DecisionID),
			SpeechTaskID: strings.TrimSpace(input.SpeechTaskID),
			SwitchAtMS:   input.SwitchAtMS,
			StartedAt:    startedAt,
			UpdatedAt:    timePtr(now),
		}
	}

	state.revision++
	state.updatedAt = timePtr(now)
	return snapshotLocked(roomID, state), nil
}

func (r *Registry) Reset(roomID int64) {
	if roomID <= 0 {
		return
	}
	r.mu.Lock()
	delete(r.rooms, roomID)
	r.mu.Unlock()
}

func normalizeTrack(track Track) (Track, error) {
	switch Track(strings.ToLower(strings.TrimSpace(string(track)))) {
	case TrackMainline:
		return TrackMainline, nil
	case TrackInterrupt:
		return TrackInterrupt, nil
	default:
		return "", errors.New("track must be mainline or interrupt")
	}
}

func validStatus(status Status) bool {
	switch status {
	case StatusIdle, StatusReady, StatusPlaying, StatusPaused, StatusCompleted, StatusFailed:
		return true
	default:
		return false
	}
}

func snapshotLocked(roomID int64, state *roomState) Snapshot {
	mainline := state.mainline
	if mainline.Status == "" {
		mainline.Status = StatusIdle
	}
	interrupt := state.interrupt
	if interrupt.Status == "" {
		interrupt.Status = StatusIdle
	}
	return Snapshot{
		RoomID:    roomID,
		Revision:  state.revision,
		Mainline:  mainline,
		Interrupt: interrupt,
		UpdatedAt: cloneTime(state.updatedAt),
	}
}

func timePtr(value time.Time) *time.Time {
	copy := value
	return &copy
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
