package collector

import (
	"context"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	eventstore "livecompanion/core/internal/events"
	"livecompanion/core/internal/model"
)

const (
	eventHighQueueSize    = 1024
	eventNormalQueueSize  = 4096
	eventPersistQueueSize = 5000
	eventPersistBatchSize = 256
	eventPersistInterval  = 75 * time.Millisecond
	eventPersistTimeout   = 2 * time.Second
)

type eventPipelineStats struct {
	QueueDepth     int
	IngressDropped uint64
	PersistDropped uint64
	Delivered      uint64
	Persisted      uint64
	PersistErrors  uint64
}

type eventPipeline struct {
	store            *eventstore.Store
	hub              *eventstore.Hub
	publicLogEnabled bool

	mu     sync.Mutex
	rooms  map[int64]*roomEventPipeline
	closed bool

	ingressDropped atomic.Uint64
	persistDropped atomic.Uint64
	delivered      atomic.Uint64
	persisted      atomic.Uint64
	persistErrors  atomic.Uint64
}

type queuedCapturedEvent struct {
	event   model.RoomEvent
	persist bool
	publish bool
}

type roomEventPipeline struct {
	parent *eventPipeline
	room   model.Room

	high    chan queuedCapturedEvent
	normal  chan queuedCapturedEvent
	persist chan model.RoomEvent
	stop    chan struct{}

	closed   atomic.Bool
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func newEventPipeline(
	store *eventstore.Store,
	hub *eventstore.Hub,
	publicLogEnabled bool,
) *eventPipeline {
	return &eventPipeline{
		store:            store,
		hub:              hub,
		publicLogEnabled: publicLogEnabled,
		rooms:            make(map[int64]*roomEventPipeline),
	}
}

// Enqueue is the capture hot path. It performs no Redis/MySQL/network I/O and
// never waits for the room worker. High-value events have their own queue so a
// like/member storm cannot crowd chat/questions out of the local fast path.
func (p *eventPipeline) Enqueue(
	room model.Room,
	input model.CreateEventInput,
	persist bool,
	publish bool,
) (model.RoomEvent, bool) {
	event := p.store.BuildEvent(room.TenantID, room.ID, input)
	worker := p.roomPipeline(room)
	if worker == nil || worker.closed.Load() {
		p.ingressDropped.Add(1)
		return event, false
	}

	item := queuedCapturedEvent{event: event, persist: persist, publish: publish}
	if isHighPriorityEvent(event.EventType) {
		select {
		case worker.high <- item:
			return event, true
		default:
			// Borrow normal-queue capacity before dropping a chat/question/order.
			// Low-value floods therefore cannot strand unused normal capacity.
			select {
			case worker.normal <- item:
				return event, true
			default:
				p.ingressDropped.Add(1)
				return event, false
			}
		}
	}
	select {
	case worker.normal <- item:
		return event, true
	default:
		p.ingressDropped.Add(1)
		return event, false
	}
}

func (p *eventPipeline) CloseRoom(roomID int64) {
	p.mu.Lock()
	worker := p.rooms[roomID]
	delete(p.rooms, roomID)
	p.mu.Unlock()
	if worker == nil {
		return
	}
	worker.stopOnce.Do(func() {
		worker.closed.Store(true)
		close(worker.stop)
	})
	worker.wg.Wait()
}

func (p *eventPipeline) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	workers := make([]*roomEventPipeline, 0, len(p.rooms))
	for roomID, worker := range p.rooms {
		delete(p.rooms, roomID)
		workers = append(workers, worker)
	}
	p.mu.Unlock()

	for _, worker := range workers {
		worker.stopOnce.Do(func() {
			worker.closed.Store(true)
			close(worker.stop)
		})
	}
	for _, worker := range workers {
		worker.wg.Wait()
	}
}

func (p *eventPipeline) Stats() eventPipelineStats {
	p.mu.Lock()
	depth := 0
	for _, worker := range p.rooms {
		depth += len(worker.high) + len(worker.normal) + len(worker.persist)
	}
	p.mu.Unlock()
	return eventPipelineStats{
		QueueDepth:     depth,
		IngressDropped: p.ingressDropped.Load(),
		PersistDropped: p.persistDropped.Load(),
		Delivered:      p.delivered.Load(),
		Persisted:      p.persisted.Load(),
		PersistErrors:  p.persistErrors.Load(),
	}
}

func (p *eventPipeline) roomPipeline(room model.Room) *roomEventPipeline {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	if worker := p.rooms[room.ID]; worker != nil {
		return worker
	}
	worker := &roomEventPipeline{
		parent:  p,
		room:    room,
		high:    make(chan queuedCapturedEvent, eventHighQueueSize),
		normal:  make(chan queuedCapturedEvent, eventNormalQueueSize),
		persist: make(chan model.RoomEvent, eventPersistQueueSize),
		stop:    make(chan struct{}),
	}
	worker.wg.Add(2)
	p.rooms[room.ID] = worker
	go worker.dispatchLoop()
	go worker.persistLoop()
	return worker
}

func (r *roomEventPipeline) dispatchLoop() {
	defer r.wg.Done()
	defer close(r.persist)
	for {
		item, ok := r.next()
		if !ok {
			r.drainIngress()
			return
		}
		r.deliver(item)
	}
}

func (r *roomEventPipeline) next() (queuedCapturedEvent, bool) {
	select {
	case <-r.stop:
		return queuedCapturedEvent{}, false
	default:
	}

	select {
	case item := <-r.high:
		return item, true
	default:
	}

	select {
	case <-r.stop:
		return queuedCapturedEvent{}, false
	case item := <-r.high:
		return item, true
	case item := <-r.normal:
		return item, true
	}
}

func (r *roomEventPipeline) drainIngress() {
	for {
		select {
		case item := <-r.high:
			r.deliver(item)
			continue
		default:
		}
		select {
		case item := <-r.normal:
			r.deliver(item)
			continue
		default:
			return
		}
	}
}

func (r *roomEventPipeline) deliver(item queuedCapturedEvent) {
	r.parent.store.Accept(item.event)
	r.parent.delivered.Add(1)
	if item.publish {
		r.parent.hub.Publish(item.event)
	}
	if item.persist && r.parent.publicLogEnabled {
		logPublicEvent(r.room, item.event)
	}
	if !item.persist {
		return
	}
	select {
	case r.persist <- item.event:
	default:
		r.parent.persistDropped.Add(1)
	}
}

func (r *roomEventPipeline) persistLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(eventPersistInterval)
	defer ticker.Stop()
	batch := make([]model.RoomEvent, 0, eventPersistBatchSize)
	lastErrorLog := time.Time{}

	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), eventPersistTimeout)
		err := r.parent.store.PersistBatch(ctx, batch)
		cancel()
		if err != nil {
			r.parent.persistErrors.Add(1)
			r.parent.persistDropped.Add(uint64(len(batch)))
			if lastErrorLog.IsZero() || time.Since(lastErrorLog) >= 5*time.Second {
				log.Printf("collector room=%d async redis persist failed batch=%d: %v", r.room.ID, len(batch), err)
				lastErrorLog = time.Now()
			}
		} else {
			r.parent.persisted.Add(uint64(len(batch)))
		}
		batch = batch[:0]
	}

	for {
		select {
		case event, ok := <-r.persist:
			if !ok {
				flush()
				return
			}
			batch = append(batch, event)
			if len(batch) >= eventPersistBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func isHighPriorityEvent(eventType string) bool {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "chat", "comment", "gift", "follow", "order":
		return true
	default:
		return false
	}
}
