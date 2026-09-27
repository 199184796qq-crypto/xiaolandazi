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

const (
	StateStopped State = "stopped"
	StateWorking State = "working"
	StatePaused  State = "paused"

	ModeAnchor  Mode = "anchor"
	ModeControl Mode = "control"
)

type Snapshot struct {
	BootID                string     `json:"boot_id"`
	RoomID                int64      `json:"room_id"`
	State                 State      `json:"state"`
	Mode                  Mode       `json:"mode"`
	PlanID                int64      `json:"plan_id,omitempty"`
	PlanName              string     `json:"plan_name,omitempty"`
	WorkingSeconds        uint64     `json:"working_seconds"`
	LeaseUntil            *time.Time `json:"lease_until,omitempty"`
	LeaseRemainingSeconds uint64     `json:"lease_remaining_seconds"`
	WorkingSince          *time.Time `json:"working_since,omitempty"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type roomState struct {
	roomID       int64
	state        State
	mode         Mode
	planID       int64
	planName     string
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
	if roomID <= 0 {
		return Snapshot{}, errors.New("room_id must be positive")
	}
	state = State(strings.ToLower(strings.TrimSpace(string(state))))
	switch state {
	case StateStopped, StateWorking, StatePaused:
	default:
		return Snapshot{}, errors.New("state must be stopped, working or paused")
	}

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
	if state == StateWorking && !current.leaseUntil.After(now) {
		return Snapshot{}, errors.New("paid work lease required before working state")
	}

	if current.state == StateStopped && state == StateWorking {
		current.accumulated = time.Duration(baseWorkingSeconds) * time.Second
	} else {
		currentSeconds := uint64(current.accumulated / time.Second)
		if baseWorkingSeconds > currentSeconds {
			current.accumulated += time.Duration(baseWorkingSeconds-currentSeconds) * time.Second
		}
	}

	if current.state != state {
		current.state = state
		current.updatedAt = now
	}

	if state == StateWorking {
		if current.workingSince.IsZero() {
			current.workingSince = now
		}
	} else {
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

func (r *Registry) IsWorking(roomID int64) bool {
	return r.Get(roomID).State == StateWorking
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
		current.updatedAt = current.leaseUntil
		current.leaseUntil = time.Time{}
	}
}

func (r *Registry) expireLeaseLocked(current *roomState, now time.Time) {
	if current == nil || current.state != StateWorking {
		return
	}
	if current.leaseUntil.IsZero() || current.workingSince.IsZero() {
		current.state = StateStopped
		current.workingSince = time.Time{}
		current.leaseUntil = time.Time{}
		current.updatedAt = now
		return
	}
	if now.Before(current.leaseUntil) {
		return
	}
	r.accrueLocked(current, now)
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
	var leaseRemaining uint64
	if !current.leaseUntil.IsZero() && current.leaseUntil.After(now) {
		value := current.leaseUntil
		leaseUntil = &value
		remaining := current.leaseUntil.Sub(now)
		leaseRemaining = uint64((remaining + time.Second - 1) / time.Second)
	}
	return Snapshot{
		BootID:                r.bootID,
		RoomID:                current.roomID,
		State:                 current.state,
		Mode:                  current.mode,
		PlanID:                current.planID,
		PlanName:              current.planName,
		WorkingSeconds:        uint64(total / time.Second),
		WorkingSince:          workingSince,
		LeaseUntil:            leaseUntil,
		LeaseRemainingSeconds: leaseRemaining,
		UpdatedAt:             current.updatedAt,
	}
}
