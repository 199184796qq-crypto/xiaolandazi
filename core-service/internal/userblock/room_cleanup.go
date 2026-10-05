package userblock

func (s *Store) ClearRoom(roomID int64) {
	s.mu.Lock()
	delete(s.rooms, roomID)
	s.mu.Unlock()
}
