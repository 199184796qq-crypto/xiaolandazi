package audiohub

func (h *Hub) ClearRoom(roomID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.deletedRooms == nil {
		h.deletedRooms = make(map[int64]struct{})
	}
	h.deletedRooms[roomID] = struct{}{}
	for id, state := range h.tasks {
		if state.task.RoomID == roomID {
			delete(h.tasks, id)
		}
	}
	for id, receiver := range h.receivers {
		if receiver.RoomID == roomID {
			delete(h.receivers, id)
		}
	}
	delete(h.roomLatest, roomID)
	for ch := range h.subscribers[roomID] {
		close(ch)
	}
	for ch := range h.controls[roomID] {
		close(ch)
	}
	delete(h.subscribers, roomID)
	delete(h.controls, roomID)
}
