package events

import (
	"sync"

	"livecompanion/core/internal/model"
)

type Hub struct {
	mu   sync.RWMutex
	subs map[int64]map[chan model.RoomEvent]struct{}
}

func NewHub() *Hub {
	return &Hub{
		subs: make(map[int64]map[chan model.RoomEvent]struct{}),
	}
}

func (h *Hub) Subscribe(roomID int64) (<-chan model.RoomEvent, func()) {
	ch := make(chan model.RoomEvent, 64)

	h.mu.Lock()
	if _, ok := h.subs[roomID]; !ok {
		h.subs[roomID] = make(map[chan model.RoomEvent]struct{})
	}
	h.subs[roomID][ch] = struct{}{}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		roomSubs, ok := h.subs[roomID]
		if !ok {
			return
		}
		if _, ok := roomSubs[ch]; !ok {
			return
		}

		delete(roomSubs, ch)
		close(ch)
		if len(roomSubs) == 0 {
			delete(h.subs, roomID)
		}
	}

	return ch, cancel
}

func (h *Hub) Publish(event model.RoomEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch := range h.subs[event.RoomID] {
		select {
		case ch <- event:
		default:
		}
	}
}
