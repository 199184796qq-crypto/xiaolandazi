package room

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"livecompanion/core/internal/model"
)

const CollectionGrace = 60 * time.Second

var ErrCollectorReplaced = errors.New("collector report belongs to a stopped or replaced room worker")

// A reporter identifies one worker, not a room status or a reconnect attempt.
type CollectorReporter struct {
	TenantID, RoomID int64
	Generation       uint64
}

type LifecycleSnapshot struct {
	Status, Collection       string
	DisconnectedAt, Deadline *time.Time
	EndRevision              uint64
}

type lifecycleRoom struct {
	// Transitions are ordered, but status readers never wait for event draining.
	transition                    sync.Mutex
	mu                            sync.Mutex
	room                          model.Room
	generation, endRevision       uint64
	status, collection, endReason string
	disconnectedAt                time.Time
	timer                         *time.Timer
	enabled, owned, retired       bool
}

// LifecycleManager alone decides room status. Collectors only report evidence.
// Worker tokens and timers do not resume paid sessions after restart.
type LifecycleManager struct {
	mu      sync.Mutex
	rooms   map[Key]*lifecycleRoom
	persist func(context.Context, int64, int64, string) error
	onEnded func(model.Room, string)
	now     func() time.Time
	closed  bool
}

func newLifecycleManager(persist func(context.Context, int64, int64, string) error) *LifecycleManager {
	return &LifecycleManager{rooms: map[Key]*lifecycleRoom{}, persist: persist, now: time.Now}
}

func (m *LifecycleManager) entry(room model.Room) *lifecycleRoom {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	key := Key{TenantID: room.TenantID, RoomID: room.ID}
	if current := m.rooms[key]; current != nil {
		return current
	}
	status := strings.ToLower(strings.TrimSpace(room.Status))
	if status == "" {
		status = "pending"
	}
	current := &lifecycleRoom{room: room, status: status, enabled: room.MonitorEnabled}
	m.rooms[key] = current
	return current
}

func (m *LifecycleManager) lookup(reporter CollectorReporter) *lifecycleRoom {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	return m.rooms[Key{TenantID: reporter.TenantID, RoomID: reporter.RoomID}]
}

func (m *LifecycleManager) SetEndObserver(observer func(model.Room, string)) {
	m.mu.Lock()
	m.onEnded = observer
	m.mu.Unlock()
}

// Session event observers can query status: the data mutex is not held.
// The transition gate orders old session_end before new live evidence.
func (m *LifecycleManager) notifyEnded(room model.Room, reason string) {
	m.mu.Lock()
	observer := m.onEnded
	m.mu.Unlock()
	if observer != nil {
		observer(room, reason)
	}
}

func (m *LifecycleManager) BeginCollection(ctx context.Context, room model.Room) (CollectorReporter, error) {
	current := m.entry(room)
	if current == nil {
		return CollectorReporter{}, ErrCollectorReplaced
	}
	current.transition.Lock()
	defer current.transition.Unlock()
	current.mu.Lock()
	defer current.mu.Unlock()
	if current.retired || !current.enabled || !room.MonitorEnabled {
		return CollectorReporter{}, ErrCollectorReplaced
	}
	if current.status != "live" && current.status != "offline" {
		if err := m.setLocked(ctx, current, "connecting"); err != nil {
			return CollectorReporter{}, err
		}
	}
	current.generation++
	current.owned = true
	reporter := CollectorReporter{room.TenantID, room.ID, current.generation}
	current.room = room
	current.room.Status = current.status
	current.collection = "connecting"
	// Worker replacement inherits the original deadline, not another 60s.
	if !current.disconnectedAt.IsZero() && current.status != "offline" {
		current.collection = "reconnecting"
		m.armLocked(current, reporter, max(time.Duration(0), current.disconnectedAt.Add(CollectionGrace).Sub(m.now())))
	}
	return reporter, nil
}

func validReporter(current *lifecycleRoom, reporter CollectorReporter) bool {
	return current != nil && !current.retired && current.enabled && current.owned &&
		reporter.Generation != 0 && current.generation == reporter.Generation
}

func (m *LifecycleManager) report(ctx context.Context, reporter CollectorReporter, kind string) (LifecycleSnapshot, error) {
	return m.reportAndApply(ctx, reporter, kind, nil)
}

// Queue admission and terminal transitions share one gate. Old captured
// events cannot slip behind session_end after their live evidence is accepted.
func (m *LifecycleManager) reportAndApply(ctx context.Context, reporter CollectorReporter, kind string, apply func(LifecycleSnapshot) error) (LifecycleSnapshot, error) {
	current := m.lookup(reporter)
	if current == nil {
		return LifecycleSnapshot{}, ErrCollectorReplaced
	}
	current.transition.Lock()
	defer current.transition.Unlock()
	current.mu.Lock()
	before := current.endRevision
	err := m.reportLocked(ctx, current, reporter, kind)
	result := snapshotLocked(current)
	ended, endedRoom, reason := before != current.endRevision, current.room, current.endReason
	current.mu.Unlock()
	if ended {
		m.notifyEnded(endedRoom, reason)
	}
	if err == nil && apply != nil {
		err = apply(result)
	}
	return result, err
}

func (m *LifecycleManager) reportLocked(ctx context.Context, current *lifecycleRoom, reporter CollectorReporter, kind string) error {
	if !validReporter(current, reporter) {
		return ErrCollectorReplaced
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch kind {
	case "connecting":
		current.collection = "connecting"
		if !current.disconnectedAt.IsZero() {
			current.collection = "reconnecting"
		}
	case "live":
		if err := m.setLocked(ctx, current, "live"); err != nil {
			return err
		}
		current.collection = "connected"
		current.disconnectedAt = time.Time{}
		m.cancelTimerLocked(current)
	case "disconnected":
		if current.status == "offline" {
			current.collection = "disconnected"
			return nil
		}
		current.collection = "reconnecting"
		if current.disconnectedAt.IsZero() {
			current.disconnectedAt = m.now().UTC()
			m.armLocked(current, reporter, CollectionGrace)
		}
	case "ended":
		return m.endLocked(ctx, current, "platform_live_ended")
	default:
		return errors.New("unknown collector evidence")
	}
	return nil
}

func (m *LifecycleManager) armLocked(current *lifecycleRoom, reporter CollectorReporter, delay time.Duration) {
	m.cancelTimerLocked(current)
	current.timer = time.AfterFunc(delay, func() {
		if err := m.expire(reporter); err != nil && !errors.Is(err, ErrCollectorReplaced) {
			log.Printf("room lifecycle timeout tenant=%d room=%d: %v", reporter.TenantID, reporter.RoomID, err)
		}
	})
}

func (m *LifecycleManager) cancelTimerLocked(current *lifecycleRoom) {
	if current.timer != nil {
		current.timer.Stop()
		current.timer = nil
	}
}

func (m *LifecycleManager) expire(reporter CollectorReporter) error {
	current := m.lookup(reporter)
	if current == nil {
		return ErrCollectorReplaced
	}
	current.transition.Lock()
	defer current.transition.Unlock()
	current.mu.Lock()
	if !validReporter(current, reporter) || current.disconnectedAt.IsZero() || current.status == "offline" {
		current.mu.Unlock()
		return ErrCollectorReplaced
	}
	remaining := current.disconnectedAt.Add(CollectionGrace).Sub(m.now().UTC())
	if remaining > 0 {
		m.armLocked(current, reporter, remaining)
		current.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	before := current.endRevision
	err := m.endLocked(ctx, current, "collector_lost_60s")
	if err != nil {
		m.armLocked(current, reporter, time.Second)
	}
	ended, endedRoom, reason := before != current.endRevision, current.room, current.endReason
	current.mu.Unlock()
	if ended {
		m.notifyEnded(endedRoom, reason)
	}
	return err
}

func (m *LifecycleManager) setLocked(ctx context.Context, current *lifecycleRoom, status string) error {
	if current.status == status {
		return nil
	}
	if m.persist != nil {
		if err := m.persist(ctx, current.room.TenantID, current.room.ID, status); err != nil {
			return err
		}
	}
	current.status, current.room.Status = status, status
	return nil
}

func (m *LifecycleManager) endLocked(ctx context.Context, current *lifecycleRoom, reason string) error {
	alreadyEnded := current.status == "offline" || current.status == "stopped"
	if err := m.setLocked(ctx, current, "offline"); err != nil {
		return err
	}
	current.collection = "disconnected"
	m.cancelTimerLocked(current)
	if !alreadyEnded {
		current.endRevision++
		current.endReason = reason
	}
	return nil
}

func snapshotLocked(current *lifecycleRoom) LifecycleSnapshot {
	result := LifecycleSnapshot{Status: current.status, Collection: current.collection, EndRevision: current.endRevision}
	if !current.disconnectedAt.IsZero() {
		at := current.disconnectedAt
		deadline := at.Add(CollectionGrace)
		result.DisconnectedAt, result.Deadline = &at, &deadline
	}
	return result
}

func (m *LifecycleManager) Snapshot(room model.Room) LifecycleSnapshot {
	return m.view(room, false)
}

func (m *LifecycleManager) view(room model.Room, freshDatabaseRead bool) LifecycleSnapshot {
	current := m.entry(room)
	if current == nil {
		return LifecycleSnapshot{Status: room.Status, Collection: "stopped"}
	}
	current.mu.Lock()
	defer current.mu.Unlock()
	// Non-owner Core shards read persisted authority, not a first-query cache.
	if freshDatabaseRead && !current.owned && !current.retired {
		current.room, current.status, current.enabled = room, room.Status, room.MonitorEnabled
	}
	return snapshotLocked(current)
}

// SQL receives the status selected by the manager, never by the caller.
func (m *LifecycleManager) changeMonitoring(ctx context.Context, room model.Room, enabled bool, update func(string) error) error {
	current := m.entry(room)
	if current == nil {
		return ErrCollectorReplaced
	}
	current.transition.Lock()
	defer current.transition.Unlock()
	current.mu.Lock()
	if current.retired {
		current.mu.Unlock()
		return ErrCollectorReplaced
	}
	status := current.status
	if !enabled {
		status = "stopped"
	} else if !current.enabled {
		status = "connecting"
	}
	if err := update(status); err != nil {
		current.mu.Unlock()
		return err
	}
	wasEnabled := current.enabled
	current.enabled, current.room.MonitorEnabled = enabled, enabled
	current.status, current.room.Status = status, status
	notify := !enabled && wasEnabled
	if !enabled {
		current.generation++
		current.owned = false
		current.collection = "stopped"
		current.disconnectedAt = time.Time{}
		m.cancelTimerLocked(current)
		if notify {
			current.endRevision++
			current.endReason = "monitor_stopped"
		}
	}
	endedRoom := current.room
	current.mu.Unlock()
	if notify {
		m.notifyEnded(endedRoom, "monitor_stopped")
	}
	return nil
}

// Loss of the collector lease invalidates timers and callbacks. Process
// cancellation does not by itself declare the platform broadcast ended.
func (m *LifecycleManager) ReleaseCollection(reporter CollectorReporter) {
	current := m.lookup(reporter)
	if current == nil {
		return
	}
	current.transition.Lock()
	defer current.transition.Unlock()
	current.mu.Lock()
	defer current.mu.Unlock()
	if current.generation != reporter.Generation {
		return
	}
	current.generation++
	current.owned = false
	m.cancelTimerLocked(current)
}

func (m *LifecycleManager) Forget(tenant *int64, roomID int64) {
	m.mu.Lock()
	var entries []*lifecycleRoom
	for key, current := range m.rooms {
		if key.RoomID == roomID && (tenant == nil || key.TenantID == *tenant) {
			entries = append(entries, current)
		}
	}
	m.mu.Unlock()
	for _, current := range entries {
		current.transition.Lock()
		current.mu.Lock()
		current.retired, current.enabled, current.owned = true, false, false
		current.generation++
		current.status, current.collection = "stopped", "stopped"
		m.cancelTimerLocked(current)
		current.mu.Unlock()
		current.transition.Unlock()
	}
}

func (m *LifecycleManager) Close() {
	m.mu.Lock()
	m.closed = true
	entries := make([]*lifecycleRoom, 0, len(m.rooms))
	for _, current := range m.rooms {
		entries = append(entries, current)
	}
	m.mu.Unlock()
	for _, current := range entries {
		current.transition.Lock()
		current.mu.Lock()
		current.generation++
		current.retired, current.enabled, current.owned = true, false, false
		m.cancelTimerLocked(current)
		current.mu.Unlock()
		current.transition.Unlock()
	}
}

func (s *Store) Lifecycle() *LifecycleManager { return s.lifecycle }

func (s *Store) decorateLifecycle(item model.Room) model.Room {
	state := s.lifecycle.view(item, true)
	item.Status, item.CollectorState = state.Status, state.Collection
	item.CollectorDisconnectedAt, item.CollectorDeadline = state.DisconnectedAt, state.Deadline
	return item
}

func (s *Store) ReportCollection(ctx context.Context, reporter CollectorReporter, kind string) (LifecycleSnapshot, error) {
	return s.lifecycle.report(ctx, reporter, kind)
}

func (s *Store) ApplyCollectedEvent(ctx context.Context, reporter CollectorReporter, apply func(LifecycleSnapshot) error) error {
	_, err := s.lifecycle.reportAndApply(ctx, reporter, "live", apply)
	return err
}

// A development-only injector reuses the token, never steals a real worker.
func (s *Store) ReportDevelopmentLive(ctx context.Context, room model.Room) error {
	current := s.lifecycle.entry(room)
	if current == nil {
		return ErrCollectorReplaced
	}
	current.mu.Lock()
	reporter := CollectorReporter{room.TenantID, room.ID, current.generation}
	owned := current.owned
	current.mu.Unlock()
	if !owned {
		var err error
		reporter, err = s.lifecycle.BeginCollection(ctx, room)
		if err != nil {
			return err
		}
	}
	_, err := s.ReportCollection(ctx, reporter, "live")
	return err
}
