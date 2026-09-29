package speechruntime

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
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
	Status         Status     `json:"status"`
	Text           string     `json:"text,omitempty"`
	QuestionText   string     `json:"question_text,omitempty"`
	ReplyText      string     `json:"reply_text,omitempty"`
	ResumeStrategy string     `json:"resume_strategy,omitempty"`
	BridgeText     string     `json:"bridge_text,omitempty"`
	BridgeUsed     bool       `json:"bridge_used,omitempty"`
	Source         string     `json:"source,omitempty"`
	AudioURL       string     `json:"audio_url,omitempty"`
	DecisionID     string     `json:"decision_id,omitempty"`
	MissionID      string     `json:"mission_id,omitempty"`
	SpeechTaskID   string     `json:"speech_task_id,omitempty"`
	SwitchAtMS     *int       `json:"switch_at_ms,omitempty"`
	DurationMS     int        `json:"duration_ms,omitempty"`
	CurrentMS      int        `json:"current_ms,omitempty"`
	Timeline       []Segment  `json:"timeline,omitempty"`
	CurrentSegment *Segment   `json:"current_segment,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
}

type Segment struct {
	SegmentID string `json:"segment_id"`
	Index     int    `json:"index"`
	StartMS   int    `json:"start_ms"`
	EndMS     int    `json:"end_ms"`
	Text      string `json:"text"`
}

type Snapshot struct {
	RoomID    int64      `json:"room_id"`
	Revision  uint64     `json:"revision"`
	Mainline  TrackState `json:"mainline"`
	Interrupt TrackState `json:"interrupt"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type UpdateInput struct {
	Track          Track      `json:"track"`
	Status         Status     `json:"status"`
	Text           string     `json:"text,omitempty"`
	QuestionText   string     `json:"question_text,omitempty"`
	ReplyText      string     `json:"reply_text,omitempty"`
	ResumeStrategy string     `json:"resume_strategy,omitempty"`
	BridgeText     string     `json:"bridge_text,omitempty"`
	BridgeUsed     bool       `json:"bridge_used,omitempty"`
	Source         string     `json:"source,omitempty"`
	AudioURL       string     `json:"audio_url,omitempty"`
	DecisionID     string     `json:"decision_id,omitempty"`
	MissionID      string     `json:"mission_id,omitempty"`
	SpeechTaskID   string     `json:"speech_task_id,omitempty"`
	SwitchAtMS     *int       `json:"switch_at_ms,omitempty"`
	DurationMS     int        `json:"duration_ms,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
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
	return snapshotLocked(roomID, state, r.now().UTC()), nil
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
		durationMS := input.DurationMS
		if durationMS <= 0 {
			durationMS = target.DurationMS
		}
		text := strings.TrimSpace(input.Text)
		if text == "" {
			text = strings.TrimSpace(input.ReplyText)
		}
		if durationMS <= 0 && text != "" {
			durationMS = utf8.RuneCountInString(text) * 230
			if durationMS < 3000 {
				durationMS = 3000
			}
		}
		timeline := buildTimeline(text, durationMS)
		*target = TrackState{
			Status:         status,
			Text:           strings.TrimSpace(input.Text),
			QuestionText:   strings.TrimSpace(input.QuestionText),
			ReplyText:      strings.TrimSpace(input.ReplyText),
			ResumeStrategy: strings.TrimSpace(input.ResumeStrategy),
			BridgeText:     strings.TrimSpace(input.BridgeText),
			BridgeUsed:     input.BridgeUsed,
			Source:         strings.TrimSpace(input.Source),
			AudioURL:       strings.TrimSpace(input.AudioURL),
			DecisionID:     strings.TrimSpace(input.DecisionID),
			MissionID:      strings.TrimSpace(input.MissionID),
			SpeechTaskID:   strings.TrimSpace(input.SpeechTaskID),
			SwitchAtMS:     input.SwitchAtMS,
			DurationMS:     durationMS,
			Timeline:       timeline,
			StartedAt:      startedAt,
			UpdatedAt:      timePtr(now),
		}
	}

	state.revision++
	state.updatedAt = timePtr(now)
	return snapshotLocked(roomID, state, now), nil
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

func snapshotLocked(roomID int64, state *roomState, now time.Time) Snapshot {
	mainline := hydrateProgress(state.mainline, now)
	if mainline.Status == "" {
		mainline.Status = StatusIdle
	}
	interrupt := hydrateProgress(state.interrupt, now)
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

func hydrateProgress(state TrackState, now time.Time) TrackState {
	if state.DurationMS <= 0 || state.StartedAt == nil {
		return state
	}
	currentMS := state.CurrentMS
	switch state.Status {
	case StatusPlaying:
		currentMS = int(now.Sub(state.StartedAt.UTC()).Milliseconds())
		if currentMS < 0 {
			currentMS = 0
		}
		if currentMS > state.DurationMS {
			currentMS = state.DurationMS
		}
	case StatusCompleted:
		currentMS = state.DurationMS
	}
	state.CurrentMS = currentMS
	state.CurrentSegment = nil
	for i := range state.Timeline {
		segment := state.Timeline[i]
		if currentMS >= segment.StartMS && (currentMS < segment.EndMS || (i == len(state.Timeline)-1 && currentMS <= segment.EndMS)) {
			copy := segment
			state.CurrentSegment = &copy
			break
		}
	}
	return state
}

func buildTimeline(text string, durationMS int) []Segment {
	text = strings.TrimSpace(text)
	if text == "" || durationMS <= 0 {
		return nil
	}
	parts := splitTimelineText(text)
	if len(parts) == 0 {
		parts = []string{text}
	}
	totalRunes := 0
	for _, part := range parts {
		totalRunes += maxInt(1, utf8.RuneCountInString(part))
	}
	result := make([]Segment, 0, len(parts))
	start := 0
	usedRunes := 0
	for i, part := range parts {
		usedRunes += maxInt(1, utf8.RuneCountInString(part))
		end := durationMS
		if i < len(parts)-1 {
			end = int(float64(durationMS) * float64(usedRunes) / float64(totalRunes))
		}
		if end <= start {
			end = start + 1
		}
		result = append(result, Segment{
			SegmentID: fmt.Sprintf("interrupt-%03d", i+1),
			Index:     i,
			StartMS:   start,
			EndMS:     end,
			Text:      part,
		})
		start = end
	}
	return result
}

func splitTimelineText(text string) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	const hardMax = 34
	result := make([]string, 0, 4)
	buffer := make([]rune, 0, hardMax)
	flush := func() {
		value := strings.TrimSpace(string(buffer))
		if value != "" {
			result = append(result, value)
		}
		buffer = buffer[:0]
	}
	for _, r := range runes {
		buffer = append(buffer, r)
		if (strings.ContainsRune("，。！？!?；;、", r) && len(buffer) >= 12) || len(buffer) >= hardMax {
			flush()
		}
	}
	flush()
	return result
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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
