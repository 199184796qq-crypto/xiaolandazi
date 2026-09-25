package audioscheduler

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type SlotID string

const (
	SlotA SlotID = "A"
	SlotB SlotID = "B"
)

type SlotState string

const (
	SlotEmpty     SlotState = "EMPTY"
	SlotPreparing SlotState = "PREPARING"
	SlotReady     SlotState = "READY"
	SlotPlaying   SlotState = "PLAYING"
	SlotSuspended SlotState = "SUSPENDED"
)

type SourceKind string

const (
	SourceLocalFile     SourceKind = "LOCAL_FILE"
	SourceGeneratedFile SourceKind = "GENERATED_FILE"
	SourceCachedAudio   SourceKind = "CACHED_AUDIO"
	SourceTTSStream     SourceKind = "TTS_STREAM"
)

type SegmentRole string

const (
	RoleMainline    SegmentRole = "MAINLINE"
	RoleInteraction SegmentRole = "INTERACTION"
	RoleBridge      SegmentRole = "BRIDGE"
	RoleSystem      SegmentRole = "SYSTEM"
)

type PlaybackStatus string

const (
	PlaybackReady     PlaybackStatus = "READY"
	PlaybackPlaying   PlaybackStatus = "PLAYING"
	PlaybackProgress  PlaybackStatus = "PROGRESS"
	PlaybackCompleted PlaybackStatus = "COMPLETED"
	PlaybackFailed    PlaybackStatus = "FAILED"
)

type SourceRef struct {
	Kind     SourceKind
	URI      string
	StreamID string
}

type MainlineCursor struct {
	PlanID    string
	TrackID   string
	UnitID    string
	SegmentID string
	OffsetMS  int64
}

func (c MainlineCursor) Empty() bool {
	return strings.TrimSpace(c.PlanID) == "" &&
		strings.TrimSpace(c.TrackID) == "" &&
		strings.TrimSpace(c.UnitID) == "" &&
		strings.TrimSpace(c.SegmentID) == "" &&
		c.OffsetMS == 0
}

type Segment struct {
	ID         string
	Role       SegmentRole
	Source     SourceRef
	DurationMS int64
	Mainline   MainlineCursor
	CreatedAt  time.Time
}

type StopPoint struct {
	SegmentID   string
	OffsetMS    int64
	SafePointID string
}

type SwitchRequest struct {
	ID          string
	TargetSlot  SlotID
	Stop        StopPoint
	Resume      MainlineCursor
	Reason      string
	RequestedAt time.Time
	ExpiresAt   time.Time
}

type PlaybackObservation struct {
	SegmentID  string
	Generation uint64
	Status     PlaybackStatus
	ProgressMS int64
	ObservedAt time.Time
}

type SlotSnapshot struct {
	ID         SlotID
	State      SlotState
	Generation uint64
	Segment    *Segment
	ReadyAt    time.Time
}

type OutputSnapshot struct {
	Sequence   uint64
	Slot       SlotID
	Segment    *Segment
	Generation uint64
	Status     PlaybackStatus
	ProgressMS int64
	SampledAt  time.Time
}

type RoomSnapshot struct {
	RoomID            int64
	Revision          uint64
	ActiveSlot        SlotID
	Slots             [2]SlotSnapshot
	Output            OutputSnapshot
	PendingSwitch     *SwitchRequest
	SuspendedMainline *MainlineCursor
}

type SwitchResult struct {
	Switched       bool
	SwitchID       string
	FromSlot       SlotID
	ToSlot         SlotID
	OutputSequence uint64
	Output         OutputSnapshot
}

type slotState struct {
	id         SlotID
	state      SlotState
	generation uint64
	segment    *Segment
	readyAt    time.Time
}

type roomState struct {
	id                int64
	revision          uint64
	outputSequence    uint64
	active            SlotID
	slots             map[SlotID]*slotState
	pending           *SwitchRequest
	suspendedMainline *MainlineCursor
	last              PlaybackObservation
}

type Registry struct {
	mu    sync.RWMutex
	rooms map[int64]*roomState
	now   func() time.Time
}

func NewRegistry() *Registry {
	return newRegistry(time.Now)
}

func newRegistry(now func() time.Time) *Registry {
	return &Registry{rooms: make(map[int64]*roomState), now: now}
}

func validSlot(slot SlotID) bool {
	return slot == SlotA || slot == SlotB
}

func otherSlot(slot SlotID) SlotID {
	if slot == SlotA {
		return SlotB
	}
	return SlotA
}

func validRole(role SegmentRole) bool {
	switch role {
	case RoleMainline, RoleInteraction, RoleBridge, RoleSystem:
		return true
	default:
		return false
	}
}

func validSource(source SourceRef) bool {
	switch source.Kind {
	case SourceLocalFile, SourceGeneratedFile, SourceCachedAudio:
		return strings.TrimSpace(source.URI) != ""
	case SourceTTSStream:
		return strings.TrimSpace(source.StreamID) != "" || strings.TrimSpace(source.URI) != ""
	default:
		return false
	}
}

func validateSegment(segment Segment) error {
	if strings.TrimSpace(segment.ID) == "" {
		return errors.New("segment_id is required")
	}
	if !validRole(segment.Role) {
		return fmt.Errorf("invalid segment role %q", segment.Role)
	}
	if !validSource(segment.Source) {
		return errors.New("audio source is incomplete")
	}
	if segment.DurationMS < 0 {
		return errors.New("duration_ms cannot be negative")
	}
	if segment.Mainline.OffsetMS < 0 {
		return errors.New("mainline offset cannot be negative")
	}
	return nil
}

func (r *Registry) roomLocked(roomID int64) (*roomState, error) {
	if roomID <= 0 {
		return nil, errors.New("room_id must be positive")
	}
	room := r.rooms[roomID]
	if room == nil {
		room = &roomState{
			id: roomID,
			slots: map[SlotID]*slotState{
				SlotA: {id: SlotA, state: SlotEmpty},
				SlotB: {id: SlotB, state: SlotEmpty},
			},
		}
		r.rooms[roomID] = room
	}
	return room, nil
}

// Prepare loads any supported audio source into one of the two room buffers.
// The active slot can never be overwritten in place.
func (r *Registry) Prepare(roomID int64, slot SlotID, segment Segment) (RoomSnapshot, error) {
	if !validSlot(slot) {
		return RoomSnapshot{}, errors.New("slot must be A or B")
	}
	if err := validateSegment(segment); err != nil {
		return RoomSnapshot{}, err
	}
	segment.ID = strings.TrimSpace(segment.ID)
	if segment.CreatedAt.IsZero() {
		segment.CreatedAt = r.now().UTC()
	} else {
		segment.CreatedAt = segment.CreatedAt.UTC()
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	room, err := r.roomLocked(roomID)
	if err != nil {
		return RoomSnapshot{}, err
	}
	if room.active == slot {
		return RoomSnapshot{}, fmt.Errorf("slot %s is active and cannot be prepared", slot)
	}
	s := room.slots[slot]
	s.generation++
	s.state = SlotPreparing
	s.segment = cloneSegment(&segment)
	s.readyAt = time.Time{}
	room.revision++
	return r.snapshotLocked(room, r.now().UTC()), nil
}

// PrepareStandby selects the inactive buffer automatically. This is the normal
// double-buffer path used while a room already has sound playing.
func (r *Registry) PrepareStandby(roomID int64, segment Segment) (RoomSnapshot, SlotID, error) {
	r.mu.RLock()
	room := r.rooms[roomID]
	active := SlotID("")
	if room != nil {
		active = room.active
	}
	r.mu.RUnlock()

	target := SlotA
	if active == SlotA {
		target = SlotB
	} else if active == SlotB {
		target = SlotA
	}
	snapshot, err := r.Prepare(roomID, target, segment)
	return snapshot, target, err
}

// PrepareResume loads the semantic mainline position saved when an interaction
// took audio focus. A stale or unrelated mainline segment is rejected.
func (r *Registry) PrepareResume(roomID int64, segment Segment) (RoomSnapshot, SlotID, error) {
	if segment.Role != RoleMainline {
		return RoomSnapshot{}, "", errors.New("resume segment must be MAINLINE")
	}

	r.mu.RLock()
	room := r.rooms[roomID]
	var resume *MainlineCursor
	active := SlotID("")
	if room != nil {
		active = room.active
		if room.suspendedMainline != nil {
			copy := *room.suspendedMainline
			resume = &copy
		}
	}
	r.mu.RUnlock()

	if resume == nil {
		return RoomSnapshot{}, "", errors.New("room has no suspended mainline")
	}
	if !cursorMatches(segment.Mainline, *resume) {
		return RoomSnapshot{}, "", errors.New("resume segment does not match suspended mainline cursor")
	}

	target := SlotA
	if active == SlotA {
		target = SlotB
	} else if active == SlotB {
		target = SlotA
	}
	snapshot, err := r.Prepare(roomID, target, segment)
	return snapshot, target, err
}

func cursorMatches(actual, expected MainlineCursor) bool {
	if expected.PlanID != "" && actual.PlanID != expected.PlanID {
		return false
	}
	if expected.TrackID != "" && actual.TrackID != expected.TrackID {
		return false
	}
	if expected.UnitID != "" && actual.UnitID != expected.UnitID {
		return false
	}
	if expected.SegmentID != "" && actual.SegmentID != expected.SegmentID {
		return false
	}
	if expected.OffsetMS > 0 && actual.OffsetMS != expected.OffsetMS {
		return false
	}
	return true
}

func (r *Registry) MarkReady(roomID int64, slot SlotID, segmentID string, generation uint64) (RoomSnapshot, SwitchResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	room, err := r.roomLocked(roomID)
	if err != nil {
		return RoomSnapshot{}, SwitchResult{}, err
	}
	s, err := requirePreparedSlot(room, slot, segmentID, generation)
	if err != nil {
		return RoomSnapshot{}, SwitchResult{}, err
	}
	s.state = SlotReady
	s.readyAt = r.now().UTC()
	room.revision++
	result := r.maybeSwitchLocked(room, s.readyAt)
	return r.snapshotLocked(room, s.readyAt), result, nil
}

func requirePreparedSlot(room *roomState, slot SlotID, segmentID string, generation uint64) (*slotState, error) {
	if !validSlot(slot) {
		return nil, errors.New("slot must be A or B")
	}
	s := room.slots[slot]
	if s == nil || s.segment == nil {
		return nil, fmt.Errorf("slot %s has no prepared segment", slot)
	}
	if s.segment.ID != strings.TrimSpace(segmentID) || generation == 0 || s.generation != generation {
		return nil, errors.New("stale slot readiness")
	}
	if s.state != SlotPreparing && s.state != SlotReady {
		return nil, fmt.Errorf("slot %s is %s, not preparing", slot, s.state)
	}
	return s, nil
}

func requireReadySlot(room *roomState, slot SlotID, segmentID string, generation uint64) (*slotState, error) {
	if !validSlot(slot) {
		return nil, errors.New("slot must be A or B")
	}
	s := room.slots[slot]
	if s == nil || s.segment == nil || s.state != SlotReady {
		return nil, fmt.Errorf("slot %s is not ready", slot)
	}
	if s.segment.ID != strings.TrimSpace(segmentID) || generation == 0 || s.generation != generation {
		return nil, errors.New("stale target slot")
	}
	return s, nil
}

// Start opens the room's single output. A and B may both be READY, but only one
// may be PLAYING.
func (r *Registry) Start(roomID int64, slot SlotID, segmentID string, generation uint64) (RoomSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	room, err := r.roomLocked(roomID)
	if err != nil {
		return RoomSnapshot{}, err
	}
	if room.active != "" {
		return RoomSnapshot{}, errors.New("room already has an active output")
	}
	s, err := requireReadySlot(room, slot, segmentID, generation)
	if err != nil {
		return RoomSnapshot{}, err
	}
	now := r.now().UTC()
	s.state = SlotPlaying
	room.active = slot
	room.outputSequence++
	room.revision++
	room.last = PlaybackObservation{
		SegmentID: s.segment.ID, Generation: s.generation,
		Status: PlaybackPlaying, ProgressMS: 0, ObservedAt: now,
	}
	return r.snapshotLocked(room, now), nil
}

// ArmSwitch stores the cut selected by the control layer. Switching is atomic
// only after the active output reaches that cut and the standby slot is READY.
func (r *Registry) ArmSwitch(roomID int64, request SwitchRequest) (RoomSnapshot, SwitchResult, error) {
	request.ID = strings.TrimSpace(request.ID)
	request.Stop.SegmentID = strings.TrimSpace(request.Stop.SegmentID)
	if request.ID == "" {
		return RoomSnapshot{}, SwitchResult{}, errors.New("switch_id is required")
	}
	if !validSlot(request.TargetSlot) {
		return RoomSnapshot{}, SwitchResult{}, errors.New("target_slot must be A or B")
	}
	if request.Stop.OffsetMS < 0 {
		return RoomSnapshot{}, SwitchResult{}, errors.New("stop offset cannot be negative")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	room, err := r.roomLocked(roomID)
	if err != nil {
		return RoomSnapshot{}, SwitchResult{}, err
	}
	if room.active == "" {
		return RoomSnapshot{}, SwitchResult{}, errors.New("room has no active output")
	}
	if request.TargetSlot == room.active {
		return RoomSnapshot{}, SwitchResult{}, errors.New("target slot is already active")
	}
	active := room.slots[room.active]
	if active == nil || active.segment == nil {
		return RoomSnapshot{}, SwitchResult{}, errors.New("active slot has no segment")
	}
	if request.Stop.SegmentID == "" {
		request.Stop.SegmentID = active.segment.ID
	}
	if request.Stop.SegmentID != active.segment.ID {
		return RoomSnapshot{}, SwitchResult{}, errors.New("stop point does not belong to active segment")
	}
	if request.RequestedAt.IsZero() {
		request.RequestedAt = r.now().UTC()
	} else {
		request.RequestedAt = request.RequestedAt.UTC()
	}
	if !request.ExpiresAt.IsZero() {
		request.ExpiresAt = request.ExpiresAt.UTC()
	}
	copyRequest := request
	room.pending = &copyRequest
	room.revision++
	now := r.now().UTC()
	result := r.maybeSwitchLocked(room, now)
	return r.snapshotLocked(room, now), result, nil
}

// Observe accepts progress only from the authoritative room output reference.
// Listener devices are followers; they must never move the room clock.
func (r *Registry) Observe(roomID int64, observation PlaybackObservation) (RoomSnapshot, SwitchResult, error) {
	if observation.ProgressMS < 0 {
		return RoomSnapshot{}, SwitchResult{}, errors.New("progress_ms cannot be negative")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	room, err := r.roomLocked(roomID)
	if err != nil {
		return RoomSnapshot{}, SwitchResult{}, err
	}
	if room.active == "" {
		return RoomSnapshot{}, SwitchResult{}, errors.New("room has no active output")
	}
	active := room.slots[room.active]
	if active == nil || active.segment == nil {
		return RoomSnapshot{}, SwitchResult{}, errors.New("active slot has no segment")
	}
	if observation.SegmentID != active.segment.ID || observation.Generation != active.generation {
		return RoomSnapshot{}, SwitchResult{}, errors.New("stale playback observation")
	}
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = r.now().UTC()
	} else {
		observation.ObservedAt = observation.ObservedAt.UTC()
	}
	if observation.ProgressMS < room.last.ProgressMS &&
		room.last.SegmentID == observation.SegmentID &&
		room.last.Generation == observation.Generation {
		return RoomSnapshot{}, SwitchResult{}, errors.New("playback progress moved backwards")
	}
	if active.segment.DurationMS > 0 && observation.ProgressMS > active.segment.DurationMS {
		observation.ProgressMS = active.segment.DurationMS
	}
	room.last = observation
	room.revision++
	result := r.maybeSwitchLocked(room, observation.ObservedAt)
	return r.snapshotLocked(room, observation.ObservedAt), result, nil
}

func (r *Registry) maybeSwitchLocked(room *roomState, now time.Time) SwitchResult {
	if room.pending == nil || room.active == "" {
		return SwitchResult{Output: r.outputLocked(room, now)}
	}
	request := room.pending
	if !request.ExpiresAt.IsZero() && now.After(request.ExpiresAt) {
		room.pending = nil
		room.revision++
		return SwitchResult{Output: r.outputLocked(room, now)}
	}
	active := room.slots[room.active]
	target := room.slots[request.TargetSlot]
	if active == nil || active.segment == nil || target == nil || target.segment == nil || target.state != SlotReady {
		return SwitchResult{Output: r.outputLocked(room, now)}
	}
	if room.last.SegmentID != active.segment.ID || room.last.Generation != active.generation {
		return SwitchResult{Output: r.outputLocked(room, now)}
	}
	if request.Stop.SegmentID != active.segment.ID || room.last.ProgressMS < request.Stop.OffsetMS {
		return SwitchResult{Output: r.outputLocked(room, now)}
	}

	from := room.active
	active.state = SlotSuspended
	target.state = SlotPlaying
	room.active = request.TargetSlot
	room.outputSequence++
	room.revision++
	if !request.Resume.Empty() {
		resume := request.Resume
		room.suspendedMainline = &resume
	}
	if target.segment.Role == RoleMainline && room.suspendedMainline != nil &&
		cursorMatches(target.segment.Mainline, *room.suspendedMainline) {
		room.suspendedMainline = nil
	}
	room.last = PlaybackObservation{
		SegmentID: target.segment.ID, Generation: target.generation,
		Status: PlaybackPlaying, ProgressMS: 0, ObservedAt: now,
	}
	switchID := request.ID
	room.pending = nil
	output := r.outputLocked(room, now)
	return SwitchResult{
		Switched: true, SwitchID: switchID,
		FromSlot: from, ToSlot: room.active,
		OutputSequence: room.outputSequence, Output: output,
	}
}

// CompleteActive normally hands over to an already-prepared standby segment.
// If no handoff is armed, the room becomes idle.
func (r *Registry) CompleteActive(roomID int64, segmentID string, generation uint64) (RoomSnapshot, SwitchResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	room, err := r.roomLocked(roomID)
	if err != nil {
		return RoomSnapshot{}, SwitchResult{}, err
	}
	if room.active == "" {
		return RoomSnapshot{}, SwitchResult{}, errors.New("room has no active output")
	}
	active := room.slots[room.active]
	if active == nil || active.segment == nil ||
		active.segment.ID != strings.TrimSpace(segmentID) || active.generation != generation {
		return RoomSnapshot{}, SwitchResult{}, errors.New("stale completion")
	}
	now := r.now().UTC()
	progress := active.segment.DurationMS
	if progress < room.last.ProgressMS {
		progress = room.last.ProgressMS
	}
	room.last = PlaybackObservation{
		SegmentID: active.segment.ID, Generation: active.generation,
		Status: PlaybackCompleted, ProgressMS: progress, ObservedAt: now,
	}
	room.revision++
	result := r.maybeSwitchLocked(room, now)
	if result.Switched {
		return r.snapshotLocked(room, now), result, nil
	}
	active.state = SlotSuspended
	room.active = ""
	room.outputSequence++
	room.revision++
	return r.snapshotLocked(room, now), SwitchResult{Output: r.outputLocked(room, now)}, nil
}

func (r *Registry) Snapshot(roomID int64) (RoomSnapshot, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	room := r.rooms[roomID]
	if room == nil {
		return RoomSnapshot{}, false
	}
	return r.snapshotLocked(room, r.now().UTC()), true
}

func (r *Registry) StandbySlot(roomID int64) SlotID {
	r.mu.RLock()
	defer r.mu.RUnlock()
	room := r.rooms[roomID]
	if room == nil || room.active == "" {
		return SlotA
	}
	return otherSlot(room.active)
}

func (r *Registry) snapshotLocked(room *roomState, now time.Time) RoomSnapshot {
	snapshot := RoomSnapshot{
		RoomID: room.id, Revision: room.revision, ActiveSlot: room.active,
		Slots:  [2]SlotSnapshot{slotSnapshot(room.slots[SlotA]), slotSnapshot(room.slots[SlotB])},
		Output: r.outputLocked(room, now),
	}
	if room.pending != nil {
		pending := *room.pending
		snapshot.PendingSwitch = &pending
	}
	if room.suspendedMainline != nil {
		resume := *room.suspendedMainline
		snapshot.SuspendedMainline = &resume
	}
	return snapshot
}

func (r *Registry) outputLocked(room *roomState, now time.Time) OutputSnapshot {
	output := OutputSnapshot{Sequence: room.outputSequence, SampledAt: now}
	if room.active == "" {
		return output
	}
	s := room.slots[room.active]
	if s == nil || s.segment == nil {
		return output
	}
	progress := room.last.ProgressMS
	if (room.last.Status == PlaybackPlaying || room.last.Status == PlaybackProgress) &&
		!room.last.ObservedAt.IsZero() && now.After(room.last.ObservedAt) {
		progress += now.Sub(room.last.ObservedAt).Milliseconds()
	}
	if progress < 0 {
		progress = 0
	}
	if s.segment.DurationMS > 0 && progress > s.segment.DurationMS {
		progress = s.segment.DurationMS
	}
	output.Slot = room.active
	output.Segment = cloneSegment(s.segment)
	output.Generation = s.generation
	output.Status = room.last.Status
	output.ProgressMS = progress
	return output
}

func slotSnapshot(s *slotState) SlotSnapshot {
	if s == nil {
		return SlotSnapshot{}
	}
	return SlotSnapshot{
		ID: s.id, State: s.state, Generation: s.generation,
		Segment: cloneSegment(s.segment), ReadyAt: s.readyAt,
	}
}

func cloneSegment(segment *Segment) *Segment {
	if segment == nil {
		return nil
	}
	copy := *segment
	return &copy
}
