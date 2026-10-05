package events

func (h *Hub) ClearRoom(roomID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[roomID] {
		close(ch)
	}
	delete(h.subs, roomID)
}
