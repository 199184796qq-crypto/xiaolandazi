package collector

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"livecompanion/core/internal/model"
	roomstore "livecompanion/core/internal/room"
)

const (
	runtimeFlushInterval = time.Second
	runtimeWriteTimeout  = 2 * time.Second
	runtimeLiveInterval  = 5 * time.Second
)

type runtimeUpdateStats struct {
	Pending int
	Writes  uint64
	Errors  uint64
}

type runtimeUpdate struct {
	room        model.Room
	markLive    bool
	onlineCount *uint64
}

type runtimeUpdater struct {
	rooms *roomstore.Store

	mu            sync.Mutex
	pending       map[int64]runtimeUpdate
	lastLiveTouch map[int64]time.Time

	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	writes atomic.Uint64
	errors atomic.Uint64
}

func newRuntimeUpdater(rooms *roomstore.Store) *runtimeUpdater {
	u := &runtimeUpdater{
		rooms:         rooms,
		pending:       make(map[int64]runtimeUpdate),
		lastLiveTouch: make(map[int64]time.Time),
		stop:          make(chan struct{}),
	}
	u.wg.Add(1)
	go u.loop()
	return u
}

// Touch only mutates local memory. The cloud MySQL write is coalesced and
// performed by the background updater at most once per live interval.
func (u *runtimeUpdater) Touch(room model.Room) {
	now := time.Now().UTC()
	u.mu.Lock()
	last := u.lastLiveTouch[room.ID]
	if !last.IsZero() && now.Sub(last) < runtimeLiveInterval {
		u.mu.Unlock()
		return
	}
	u.lastLiveTouch[room.ID] = now
	update := u.pending[room.ID]
	update.room = room
	update.markLive = true
	u.pending[room.ID] = update
	u.mu.Unlock()
}

// SetOnlineCount keeps only the latest observed value for a room until the
// next background flush, preventing high-frequency room metrics from issuing
// one SQL UPDATE per frame/event.
func (u *runtimeUpdater) SetOnlineCount(room model.Room, value uint64) {
	if value == 0 {
		return
	}
	u.mu.Lock()
	update := u.pending[room.ID]
	update.room = room
	copyValue := value
	update.onlineCount = &copyValue
	u.pending[room.ID] = update
	u.mu.Unlock()
}

func (u *runtimeUpdater) ClearRoom(roomID int64) {
	u.mu.Lock()
	delete(u.pending, roomID)
	delete(u.lastLiveTouch, roomID)
	u.mu.Unlock()
}

func (u *runtimeUpdater) Stats() runtimeUpdateStats {
	u.mu.Lock()
	pending := len(u.pending)
	u.mu.Unlock()
	return runtimeUpdateStats{
		Pending: pending,
		Writes:  u.writes.Load(),
		Errors:  u.errors.Load(),
	}
}

func (u *runtimeUpdater) Close() {
	u.stopOnce.Do(func() { close(u.stop) })
	u.wg.Wait()
}

func (u *runtimeUpdater) loop() {
	defer u.wg.Done()
	ticker := time.NewTicker(runtimeFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-u.stop:
			u.flush()
			return
		case <-ticker.C:
			u.flush()
		}
	}
}

func (u *runtimeUpdater) flush() {
	u.mu.Lock()
	if len(u.pending) == 0 {
		u.mu.Unlock()
		return
	}
	pending := make([]runtimeUpdate, 0, len(u.pending))
	for roomID, update := range u.pending {
		pending = append(pending, update)
		delete(u.pending, roomID)
	}
	u.mu.Unlock()

	for _, update := range pending {
		if update.markLive {
			ctx, cancel := context.WithTimeout(context.Background(), runtimeWriteTimeout)
			err := u.rooms.MarkLive(ctx, update.room.TenantID, update.room.ID)
			cancel()
			if err != nil {
				u.errors.Add(1)
				log.Printf("collector room=%d async mark live failed: %v", update.room.ID, err)
			} else {
				u.writes.Add(1)
			}
		}
		if update.onlineCount != nil {
			ctx, cancel := context.WithTimeout(context.Background(), runtimeWriteTimeout)
			err := u.rooms.SetOnlineCount(ctx, update.room.TenantID, update.room.ID, *update.onlineCount)
			cancel()
			if err != nil {
				u.errors.Add(1)
				log.Printf("collector room=%d async online count failed: %v", update.room.ID, err)
			} else {
				u.writes.Add(1)
			}
		}
	}
}
