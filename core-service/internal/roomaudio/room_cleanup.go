package roomaudio

// ClearRoom differs from a normal stop/reset: late frames or start requests
// from an in-flight producer must never recreate a deleted room's audio engine.
func (e *Engine) ClearRoom(roomID int64) {
	if e == nil || roomID <= 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.deletedRooms == nil {
		e.deletedRooms = make(map[int64]struct{})
	}
	e.deletedRooms[roomID] = struct{}{}
	if state := e.rooms[roomID]; state != nil {
		for ch := range state.subscribers {
			close(ch)
		}
		delete(e.rooms, roomID)
	}
}
