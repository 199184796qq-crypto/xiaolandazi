// Package agentwork is the paid AI runtime gate.
// It must never gate collection, event storage, question clustering,
// statistics or other free deterministic base processing.
package agentwork

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type State string
type Mode string
type StopReason string

// Management reconciles paid runtime every 10 seconds. Renewing with half of
// the one-minute lease still available leaves several retry opportunities if
// one DB/Core round-trip is slow.
const LeaseRenewThresholdSeconds uint64 = 30

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateWorking  State = "working"
	StateStopping State = "stopping"

	StopReasonManual         StopReason = "manual"
	StopReasonManualPause    StopReason = "manual_pause"
	StopReasonQuotaExhausted StopReason = "quota_exhausted"
	StopReasonLiveFinished   StopReason = "live_finished"
	StopReasonCoreRestart    StopReason = "core_restart"
	StopReasonSystemError    StopReason = "system_error"

	ModeAnchor  Mode = "anchor"
	ModeControl Mode = "control"
)

type Snapshot struct {
	BootID                string     `json:"boot_id"`
	RoomID                int64      `json:"room_id"`
	State                 State      `json:"state"`
	StopReason            StopReason `json:"stop_reason,omitempty"`
	Mode                  Mode       `json:"mode"`
	PlanID                int64      `json:"plan_id,omitempty"`
	PlanName              string     `json:"plan_name,omitempty"`
	HotRevision           uint64     `json:"hot_revision"`
	HotModules            []string   `json:"hot_modules,omitempty"`
	HotUpdatedAt          *time.Time `json:"hot_updated_at,omitempty"`
	WorkingSeconds        uint64     `json:"working_seconds"`
	LeaseUntil            *time.Time `json:"lease_until,omitempty"`
	LeaseRemainingSeconds uint64     `json:"lease_remaining_seconds"`
	LeaseRenewalDue       bool       `json:"lease_renewal_due"`
	WorkingSince          *time.Time `json:"working_since,omitempty"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type roomState struct {
	roomID       int64
	state        State
	mode         Mode
	planID       int64
	planName     string
	hotRevision  uint64
	hotModules   []string
	hotUpdatedAt time.Time
	stopReason   StopReason
	accumulated  time.Duration
	workingSince time.Time
	leaseUntil   time.Time
	updatedAt    time.Time
}

type Registry struct {
	mu     sync.Mutex
	rooms  map[int64]*roomState
	now    func() time.Time
	bootID string
}

func New() *Registry {
	return newRegistry(time.Now)
}

func newRegistry(now func() time.Time) *Registry {
	if now == nil {
		now = time.Now
	}
	return &Registry{
		rooms:  make(map[int64]*roomState),
		now:    now,
		bootID: newBootID(now().UTC()),
	}
}

func newBootID(now time.Time) string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return fmt.Sprintf("core-%d-%s", now.UnixMilli(), hex.EncodeToString(raw[:]))
	}
	return fmt.Sprintf("core-%d-%d", now.UnixMilli(), os.Getpid())
}

func (r *Registry) BootID() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bootID
}

func (r *Registry) Set(roomID int64, state State) (Snapshot, error) {
	return r.SetWithBase(roomID, state, 0)
}

func (r *Registry) SetWithBase(
	roomID int64,
	state State,
	baseWorkingSeconds uint64,
) (Snapshot, error) {
	return r.SetWithReason(roomID, state, "", baseWorkingSeconds)
}

// SetWithReason changes the Core-owned agent lifecycle state. Set and
// SetWithBase remain compatibility wrappers for existing callers.
func (r *Registry) SetWithReason(
	roomID int64,
	state State,
	reason StopReason,
	baseWorkingSeconds uint64,
) (Snapshot, error) {
	if roomID <= 0 {
		return Snapshot{}, errors.New("room_id must be positive")
	}
	state = State(strings.ToLower(strings.TrimSpace(string(state))))
	switch state {
	case StateStopped, StateStarting, StateWorking, StateStopping:
	default:
		return Snapshot{}, errors.New("state must be stopped, starting, working or stopping")
	}
	reason = NormalizeStopReason(reason)

	now := r.now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()

	current := r.rooms[roomID]
	if current == nil {
		current = &roomState{
			roomID:    roomID,
			state:     StateStopped,
			mode:      ModeControl,
			updatedAt: now,
		}
		r.rooms[roomID] = current
	}

	r.accrueLocked(current, now)
	if (state == StateStarting || state == StateWorking) && !current.leaseUntil.After(now) {
		return Snapshot{}, errors.New("paid work lease required before working state")
	}

	if current.state == StateStopped && (state == StateStarting || state == StateWorking) {
		current.accumulated = time.Duration(baseWorkingSeconds) * time.Second
	} else {
		currentSeconds := uint64(current.accumulated / time.Second)
		if baseWorkingSeconds > currentSeconds {
			current.accumulated += time.Duration(baseWorkingSeconds-currentSeconds) * time.Second
		}
	}

	if current.state != state || current.stopReason != reason {
		current.state = state
		if state == StateStopped || state == StateStopping {
			current.stopReason = reason
		} else if state == StateStarting || state == StateWorking {
			current.stopReason = ""
		}
		current.updatedAt = now
	}

	switch state {
	case StateWorking:
		if current.workingSince.IsZero() {
			current.workingSince = now
		}
	case StateStarting:
		current.workingSince = time.Time{}
	case StateStopping, StateStopped:
		current.workingSince = time.Time{}
		current.leaseUntil = time.Time{}
	}

	return r.snapshotLocked(current, now), nil
}

func (r *Registry) GrantLease(roomID int64, seconds uint64) (Snapshot, error) {
	if roomID <= 0 {
		return Snapshot{}, errors.New("room_id must be positive")
	}
	if seconds == 0 {
		return Snapshot{}, errors.New("lease seconds must be positive")
	}
	now := r.now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.rooms[roomID]
	if current == nil {
		current = &roomState{roomID: roomID, state: StateStopped, mode: ModeControl, updatedAt: now}
		r.rooms[roomID] = current
	}
	r.accrueLocked(current, now)
	base := now
	if current.leaseUntil.After(now) {
		base = current.leaseUntil
	}
	current.leaseUntil = base.Add(time.Duration(seconds) * time.Second)
	current.updatedAt = now
	if current.state == StateWorking && current.workingSince.IsZero() {
		current.workingSince = now
	}
	return r.snapshotLocked(current, now), nil
}

func (r *Registry) Has(roomID int64) bool {
	if roomID <= 0 {
		return false
	}
	r.mu.Lock()
	_, ok := r.rooms[roomID]
	r.mu.Unlock()
	return ok
}

func (r *Registry) Get(roomID int64) Snapshot {
	now := r.now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()

	current := r.rooms[roomID]
	if current == nil {
		return Snapshot{BootID: r.bootID, RoomID: roomID, State: StateStopped, Mode: ModeControl}
	}
	r.expireLeaseLocked(current, now)
	return r.snapshotLocked(current, now)
}

func (r *Registry) SetMode(roomID int64, mode Mode) (Snapshot, error) {
	if roomID <= 0 {
		return Snapshot{}, errors.New("room_id must be positive")
	}
	mode = Mode(strings.ToLower(strings.TrimSpace(string(mode))))
	switch mode {
	case ModeAnchor, ModeControl:
	default:
		return Snapshot{}, errors.New("mode must be anchor or control")
	}
	now := r.now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.rooms[roomID]
	if current == nil {
		current = &roomState{roomID: roomID, state: StateStopped, mode: ModeControl, updatedAt: now}
		r.rooms[roomID] = current
	}
	r.accrueLocked(current, now)
	if current.mode != mode {
		current.mode = mode
		current.updatedAt = now
	}
	if current.state == StateWorking && current.workingSince.IsZero() {
		current.workingSince = now
	}
	return r.snapshotLocked(current, now), nil
}

func (r *Registry) Mode(roomID int64) Mode {
	return r.Get(roomID).Mode
}

func (r *Registry) SetPlan(roomID, planID int64, planName string) (Snapshot, error) {
	if roomID <= 0 {
		return Snapshot{}, errors.New("room_id must be positive")
	}
	if planID < 0 {
		return Snapshot{}, errors.New("plan_id must not be negative")
	}
	planName = strings.TrimSpace(planName)
	now := r.now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.rooms[roomID]
	if current == nil {
		current = &roomState{roomID: roomID, state: StateStopped, mode: ModeControl, updatedAt: now}
		r.rooms[roomID] = current
	}
	r.accrueLocked(current, now)
	if current.planID != planID || current.planName != planName {
		current.planID = planID
		current.planName = planName
		current.updatedAt = now
	}
	if current.state == StateWorking && current.workingSince.IsZero() && current.leaseUntil.After(now) {
		current.workingSince = now
	}
	return r.snapshotLocked(current, now), nil
}

func (r *Registry) TouchHotReload(roomID int64, modules []string) (Snapshot, error) {
	if roomID <= 0 {
		return Snapshot{}, errors.New("room_id must be positive")
	}
	normalized := make([]string, 0, len(modules))
	seen := map[string]struct{}{}
	for _, module := range modules {
		module = strings.ToLower(strings.TrimSpace(module))
		if module == "" {
			continue
		}
		if _, exists := seen[module]; exists {
			continue
		}
		seen[module] = struct{}{}
		normalized = append(normalized, module)
	}
	if len(normalized) == 0 {
		return Snapshot{}, errors.New("hot_reload requires at least one module")
	}
	now := r.now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.rooms[roomID]
	if current == nil {
		current = &roomState{roomID: roomID, state: StateStopped, mode: ModeControl, updatedAt: now}
		r.rooms[roomID] = current
	}
	r.accrueLocked(current, now)
	if current.state == StateWorking && current.workingSince.IsZero() && current.leaseUntil.After(now) {
		current.workingSince = now
	}
	current.hotRevision++
	current.hotModules = append(current.hotModules[:0], normalized...)
	current.hotUpdatedAt = now
	current.updatedAt = now
	return r.snapshotLocked(current, now), nil
}

func (r *Registry) IsWorking(roomID int64) bool {
	return r.Get(roomID).State == StateWorking
}

// StartAgent is the Core-owned paid-agent start command. A valid lease must
// already exist; callers cannot force a working state without paid runway.
func (r *Registry) StartAgent(roomID int64, baseWorkingSeconds uint64) (Snapshot, error) {
	if _, err := r.SetWithReason(roomID, StateStarting, "", baseWorkingSeconds); err != nil {
		return Snapshot{}, err
	}
	return r.SetWithReason(roomID, StateWorking, "", baseWorkingSeconds)
}

// StopAgent is the single paid-agent stop entry point.
// Callers should provide the business reason instead of mutating state directly.
func (r *Registry) StopAgent(roomID int64, reason StopReason) (Snapshot, error) {
	current := r.Get(roomID)
	if current.State == StateStopped {
		if normalized := NormalizeStopReason(reason); normalized != "" && current.StopReason == "" {
			return r.SetWithReason(roomID, StateStopped, normalized, current.WorkingSeconds)
		}
		return current, nil
	}
	if _, err := r.SetWithReason(roomID, StateStopping, reason, current.WorkingSeconds); err != nil {
		return Snapshot{}, err
	}
	return r.SetWithReason(roomID, StateStopped, reason, current.WorkingSeconds)
}

// Snapshots returns all Core-owned paid-agent states. Reading snapshots also
// advances lease expiry, so a Core-local watcher can clean up TTS/audio even
// when Management or the web UI is disconnected.
func (r *Registry) Snapshots() []Snapshot {
	now := r.now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]Snapshot, 0, len(r.rooms))
	for _, current := range r.rooms {
		r.expireLeaseLocked(current, now)
		result = append(result, r.snapshotLocked(current, now))
	}
	return result
}

func (r *Registry) Clear(roomID int64) {
	r.mu.Lock()
	delete(r.rooms, roomID)
	r.mu.Unlock()
}

func (r *Registry) accrueLocked(current *roomState, now time.Time) {
	if current == nil || current.state != StateWorking {
		return
	}
	if current.leaseUntil.IsZero() || current.workingSince.IsZero() {
		current.state = StateStopped
		current.stopReason = StopReasonSystemError
		current.workingSince = time.Time{}
		current.leaseUntil = time.Time{}
		current.updatedAt = now
		return
	}
	if now.Before(current.workingSince) {
		now = current.workingSince
	}
	end := now
	if !current.leaseUntil.IsZero() && end.After(current.leaseUntil) {
		end = current.leaseUntil
	}
	if end.After(current.workingSince) {
		current.accumulated += end.Sub(current.workingSince)
	}
	current.workingSince = time.Time{}
	if !current.leaseUntil.IsZero() && !now.Before(current.leaseUntil) {
		current.state = StateStopped
		current.stopReason = StopReasonQuotaExhausted
		current.updatedAt = current.leaseUntil
		current.leaseUntil = time.Time{}
	}
}

func (r *Registry) expireLeaseLocked(current *roomState, now time.Time) {
	if current == nil {
		return
	}
	if current.state == StateStopping {
		current.state = StateStopped
		if current.stopReason == "" {
			current.stopReason = StopReasonSystemError
		}
		current.workingSince = time.Time{}
		current.leaseUntil = time.Time{}
		current.updatedAt = now
		return
	}
	if current.state != StateWorking && current.state != StateStarting {
		return
	}
	if current.state == StateWorking && (current.leaseUntil.IsZero() || current.workingSince.IsZero()) {
		current.state = StateStopped
		current.stopReason = StopReasonSystemError
		current.workingSince = time.Time{}
		current.leaseUntil = time.Time{}
		current.updatedAt = now
		return
	}
	if now.Before(current.leaseUntil) {
		return
	}
	if current.state == StateWorking {
		r.accrueLocked(current, now)
		return
	}
	current.state = StateStopped
	current.stopReason = StopReasonQuotaExhausted
	current.workingSince = time.Time{}
	current.leaseUntil = time.Time{}
	current.updatedAt = now
}

func (r *Registry) snapshotLocked(current *roomState, now time.Time) Snapshot {
	if current == nil {
		return Snapshot{BootID: r.bootID, State: StateStopped, Mode: ModeControl}
	}
	if current.mode == "" {
		current.mode = ModeControl
	}
	total := current.accumulated
	var workingSince *time.Time
	if current.state == StateWorking && !current.workingSince.IsZero() {
		start := current.workingSince
		if now.Before(start) {
			now = start
		}
		total += now.Sub(start)
		value := start
		workingSince = &value
	}
	var leaseUntil *time.Time
	var hotUpdatedAt *time.Time
	if !current.hotUpdatedAt.IsZero() {
		value := current.hotUpdatedAt
		hotUpdatedAt = &value
	}
	var leaseRemaining uint64
	if !current.leaseUntil.IsZero() && current.leaseUntil.After(now) {
		value := current.leaseUntil
		leaseUntil = &value
		remaining := current.leaseUntil.Sub(now)
		leaseRemaining = uint64((remaining + time.Second - 1) / time.Second)
	}
	leaseRenewalDue := current.state == StateWorking &&
		leaseRemaining > 0 &&
		leaseRemaining <= LeaseRenewThresholdSeconds
	return Snapshot{
		BootID:                r.bootID,
		RoomID:                current.roomID,
		State:                 current.state,
		StopReason:            current.stopReason,
		Mode:                  current.mode,
		PlanID:                current.planID,
		PlanName:              current.planName,
		HotRevision:           current.hotRevision,
		HotModules:            append([]string(nil), current.hotModules...),
		HotUpdatedAt:          hotUpdatedAt,
		WorkingSeconds:        uint64(total / time.Second),
		WorkingSince:          workingSince,
		LeaseUntil:            leaseUntil,
		LeaseRemainingSeconds: leaseRemaining,
		LeaseRenewalDue:       leaseRenewalDue,
		UpdatedAt:             current.updatedAt,
	}
}

// NormalizeStopReason keeps the Core vocabulary stable while accepting the
// historical reasons sent by the Management service.
func NormalizeStopReason(reason StopReason) StopReason {
	switch strings.ToLower(strings.TrimSpace(string(reason))) {
	case "manual", "manual_stop":
		return StopReasonManual
	case "manual_pause":
		return StopReasonManualPause
	case "quota_exhausted", "quota_unavailable":
		return StopReasonQuotaExhausted
	case "live_finished", "room_offline":
		return StopReasonLiveFinished
	case "core_restart", "core_runtime_reset":
		return StopReasonCoreRestart
	case "system_error", "core_start_failed", "core_lease_failed":
		return StopReasonSystemError
	default:
		return StopReason(strings.ToLower(strings.TrimSpace(string(reason))))
	}
}
