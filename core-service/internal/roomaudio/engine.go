package roomaudio

import (
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	SampleRate       = 24000
	Channels         = 1
	BytesPerSample   = 2
	FrameDurationMS  = 20
	SamplesPerFrame  = SampleRate * FrameDurationMS / 1000
	PCMBytesPerFrame = SamplesPerFrame * Channels * BytesPerSample
)

type Phase string

const (
	PhaseIdle               Phase = "idle"
	PhaseMainline           Phase = "mainline"
	PhasePreparingInterrupt Phase = "preparing_interrupt"
	PhaseArmed              Phase = "armed"
	PhaseInterrupt          Phase = "interrupt"
	PhasePreparingResume    Phase = "preparing_resume"
	PhaseResume             Phase = "resume"
	PhasePaused             Phase = "paused"
	PhaseError              Phase = "error"
)

type Source string

const (
	SourceNone      Source = ""
	SourceMainline  Source = "mainline"
	SourceInterrupt Source = "interrupt"
)

type Frame struct {
	RoomID     int64     `json:"room_id"`
	Sequence   uint64    `json:"sequence"`
	PTSMS      int64     `json:"pts_ms"`
	DurationMS int       `json:"duration_ms"`
	Source     Source    `json:"source"`
	PCM        []byte    `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
}

type Snapshot struct {
	RoomID              int64     `json:"room_id"`
	Phase               Phase     `json:"phase"`
	ActiveSource        Source    `json:"active_source,omitempty"`
	Sequence            uint64    `json:"sequence"`
	OutputPTSMS         int64     `json:"output_pts_ms"`
	MainlineCursorMS    int       `json:"mainline_cursor_ms"`
	CurrentSegmentID    string    `json:"current_segment_id,omitempty"`
	PlannedCutSegmentID string    `json:"planned_cut_segment_id,omitempty"`
	ResumeSegmentID     string    `json:"resume_segment_id,omitempty"`
	PausedFrom          Phase     `json:"paused_from,omitempty"`
	Subscribers         int       `json:"subscribers"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type Metrics struct {
	Rooms       int `json:"rooms"`
	Subscribers int `json:"subscribers"`
}

type roomState struct {
	roomID              int64
	phase               Phase
	activeSource        Source
	sequence            uint64
	outputPTSMS         int64
	mainlineCursorMS    int
	currentSegmentID    string
	plannedCutSegmentID string
	resumeSegmentID     string
	pausedFrom          Phase
	updatedAt           time.Time
	subscribers         map[chan Frame]struct{}
}

type Engine struct {
	mu    sync.RWMutex
	rooms map[int64]*roomState
	now   func() time.Time
}

func New() *Engine {
	return &Engine{
		rooms: make(map[int64]*roomState),
		now:   time.Now,
	}
}

func (e *Engine) Snapshot(roomID int64) Snapshot {
	if e == nil || roomID <= 0 {
		return Snapshot{RoomID: roomID, Phase: PhaseIdle}
	}
	e.mu.RLock()
	state := e.rooms[roomID]
	if state == nil {
		e.mu.RUnlock()
		return Snapshot{RoomID: roomID, Phase: PhaseIdle}
	}
	snapshot := snapshotLocked(state)
	e.mu.RUnlock()
	return snapshot
}

func (e *Engine) Metrics() Metrics {
	if e == nil {
		return Metrics{}
	}
	e.mu.RLock()
	metrics := Metrics{Rooms: len(e.rooms)}
	for _, state := range e.rooms {
		metrics.Subscribers += len(state.subscribers)
	}
	e.mu.RUnlock()
	return metrics
}

func (e *Engine) StartMainline(roomID int64, segmentID string) (Snapshot, error) {
	if roomID <= 0 {
		return Snapshot{}, errors.New("room_id must be positive")
	}
	e.mu.Lock()
	state := e.ensureRoomLocked(roomID)
	state.phase = PhaseMainline
	state.activeSource = SourceMainline
	state.mainlineCursorMS = 0
	state.currentSegmentID = strings.TrimSpace(segmentID)
	state.plannedCutSegmentID = ""
	state.resumeSegmentID = ""
	state.pausedFrom = ""
	state.updatedAt = e.now().UTC()
	snapshot := snapshotLocked(state)
	e.mu.Unlock()
	return snapshot, nil
}

func (e *Engine) PrepareInterrupt(roomID int64) (Snapshot, error) {
	return e.transition(roomID, []Phase{PhaseMainline, PhaseResume}, PhasePreparingInterrupt, SourceMainline, "", "")
}

func (e *Engine) ArmInterrupt(roomID int64, cutSegmentID, resumeSegmentID string) (Snapshot, error) {
	cutSegmentID = strings.TrimSpace(cutSegmentID)
	if cutSegmentID == "" {
		return Snapshot{}, errors.New("cut_segment_id is required")
	}
	return e.transition(roomID, []Phase{PhasePreparingInterrupt, PhaseMainline}, PhaseArmed, SourceMainline, cutSegmentID, strings.TrimSpace(resumeSegmentID))
}

func (e *Engine) StartInterrupt(roomID int64) (Snapshot, error) {
	return e.transition(roomID, []Phase{PhaseArmed, PhasePreparingInterrupt, PhaseMainline}, PhaseInterrupt, SourceInterrupt, "", "")
}

func (e *Engine) PrepareResume(roomID int64, resumeSegmentID string) (Snapshot, error) {
	return e.transition(roomID, []Phase{PhaseInterrupt}, PhasePreparingResume, SourceInterrupt, "", strings.TrimSpace(resumeSegmentID))
}

func (e *Engine) StartResume(roomID int64, segmentID string) (Snapshot, error) {
	segmentID = strings.TrimSpace(segmentID)
	e.mu.Lock()
	defer e.mu.Unlock()
	state := e.rooms[roomID]
	if state == nil || state.phase != PhasePreparingResume {
		return Snapshot{}, errors.New("room is not preparing resume")
	}
	state.phase = PhaseResume
	state.activeSource = SourceMainline
	if segmentID != "" {
		state.currentSegmentID = segmentID
	} else if state.resumeSegmentID != "" {
		state.currentSegmentID = state.resumeSegmentID
	}
	state.updatedAt = e.now().UTC()
	return snapshotLocked(state), nil
}

func (e *Engine) CompleteResume(roomID int64) (Snapshot, error) {
	return e.transition(roomID, []Phase{PhaseResume}, PhaseMainline, SourceMainline, "", "")
}

func (e *Engine) Pause(roomID int64) (Snapshot, error) {
	if roomID <= 0 {
		return Snapshot{}, errors.New("room_id must be positive")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	state := e.rooms[roomID]
	if state == nil || state.phase == PhaseIdle {
		return Snapshot{}, errors.New("room audio engine is idle")
	}
	if state.phase == PhasePaused {
		return snapshotLocked(state), nil
	}
	state.pausedFrom = state.phase
	state.phase = PhasePaused
	state.updatedAt = e.now().UTC()
	return snapshotLocked(state), nil
}

func (e *Engine) Resume(roomID int64) (Snapshot, error) {
	if roomID <= 0 {
		return Snapshot{}, errors.New("room_id must be positive")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	state := e.rooms[roomID]
	if state == nil || state.phase != PhasePaused {
		return Snapshot{}, errors.New("room audio engine is not paused")
	}
	phase := state.pausedFrom
	if phase == "" || phase == PhasePaused {
		phase = PhaseMainline
	}
	state.phase = phase
	state.pausedFrom = ""
	state.updatedAt = e.now().UTC()
	return snapshotLocked(state), nil
}

func (e *Engine) Reset(roomID int64) {
	if e == nil || roomID <= 0 {
		return
	}
	e.mu.Lock()
	state := e.rooms[roomID]
	if state != nil {
		for ch := range state.subscribers {
			close(ch)
		}
		delete(e.rooms, roomID)
	}
	e.mu.Unlock()
}

func (e *Engine) PublishPCM(roomID int64, source Source, pcm []byte, segmentID string) (Frame, error) {
	return e.PublishPCMAt(roomID, source, pcm, segmentID, -1)
}

func (e *Engine) PublishPCMAt(roomID int64, source Source, pcm []byte, segmentID string, sourceCursorMS int) (Frame, error) {
	if e == nil || roomID <= 0 {
		return Frame{}, errors.New("room_id must be positive")
	}
	if len(pcm) != PCMBytesPerFrame {
		return Frame{}, errors.New("pcm frame must be exactly 20ms s16le mono 24khz")
	}
	e.mu.Lock()
	state := e.ensureRoomLocked(roomID)
	if state.phase == PhasePaused {
		e.mu.Unlock()
		return Frame{}, errors.New("room audio engine is paused")
	}
	if state.phase == PhaseIdle && source == SourceMainline {
		state.phase = PhaseMainline
		state.activeSource = SourceMainline
	}
	if source == SourceNone || source != state.activeSource {
		e.mu.Unlock()
		return Frame{}, errors.New("source is not the active foreground source")
	}
	state.sequence++
	frame := Frame{
		RoomID:     roomID,
		Sequence:   state.sequence,
		PTSMS:      state.outputPTSMS,
		DurationMS: FrameDurationMS,
		Source:     source,
		PCM:        append([]byte(nil), pcm...),
		CreatedAt:  e.now().UTC(),
	}
	state.outputPTSMS += FrameDurationMS
	if source == SourceMainline && sourceCursorMS >= 0 {
		state.mainlineCursorMS = sourceCursorMS + FrameDurationMS
	}
	if value := strings.TrimSpace(segmentID); value != "" {
		state.currentSegmentID = value
	}
	state.updatedAt = frame.CreatedAt
	subscribers := make([]chan Frame, 0, len(state.subscribers))
	for ch := range state.subscribers {
		subscribers = append(subscribers, ch)
	}
	e.mu.Unlock()

	for _, ch := range subscribers {
		select {
		case ch <- frame:
		default:
		}
	}
	return frame, nil
}

func (e *Engine) Subscribe(roomID int64) (<-chan Frame, func(), error) {
	if e == nil || roomID <= 0 {
		return nil, nil, errors.New("room_id must be positive")
	}
	ch := make(chan Frame, 64)
	e.mu.Lock()
	state := e.ensureRoomLocked(roomID)
	state.subscribers[ch] = struct{}{}
	e.mu.Unlock()
	cancel := func() {
		e.mu.Lock()
		state := e.rooms[roomID]
		if state != nil {
			if _, ok := state.subscribers[ch]; ok {
				delete(state.subscribers, ch)
				close(ch)
			}
		}
		e.mu.Unlock()
	}
	return ch, cancel, nil
}

func (e *Engine) transition(roomID int64, allowed []Phase, next Phase, source Source, cutSegmentID, resumeSegmentID string) (Snapshot, error) {
	if roomID <= 0 {
		return Snapshot{}, errors.New("room_id must be positive")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	state := e.rooms[roomID]
	if state == nil {
		return Snapshot{}, errors.New("room audio engine is idle")
	}
	allowedNow := false
	for _, phase := range allowed {
		if state.phase == phase {
			allowedNow = true
			break
		}
	}
	if !allowedNow {
		return Snapshot{}, errors.New("invalid room audio phase transition")
	}
	state.phase = next
	state.activeSource = source
	if value := strings.TrimSpace(cutSegmentID); value != "" {
		state.plannedCutSegmentID = value
	}
	if value := strings.TrimSpace(resumeSegmentID); value != "" {
		state.resumeSegmentID = value
	}
	if next == PhaseMainline {
		state.plannedCutSegmentID = ""
		state.resumeSegmentID = ""
	}
	state.updatedAt = e.now().UTC()
	return snapshotLocked(state), nil
}

func (e *Engine) ensureRoomLocked(roomID int64) *roomState {
	state := e.rooms[roomID]
	if state != nil {
		return state
	}
	state = &roomState{
		roomID:      roomID,
		phase:       PhaseIdle,
		updatedAt:   e.now().UTC(),
		subscribers: make(map[chan Frame]struct{}),
	}
	e.rooms[roomID] = state
	return state
}

func snapshotLocked(state *roomState) Snapshot {
	return Snapshot{
		RoomID:              state.roomID,
		Phase:               state.phase,
		ActiveSource:        state.activeSource,
		Sequence:            state.sequence,
		OutputPTSMS:         state.outputPTSMS,
		MainlineCursorMS:    state.mainlineCursorMS,
		CurrentSegmentID:    state.currentSegmentID,
		PlannedCutSegmentID: state.plannedCutSegmentID,
		ResumeSegmentID:     state.resumeSegmentID,
		PausedFrom:          state.pausedFrom,
		Subscribers:         len(state.subscribers),
		UpdatedAt:           state.updatedAt,
	}
}
