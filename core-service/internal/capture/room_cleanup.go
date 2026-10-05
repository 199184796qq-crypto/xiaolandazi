package capture

// ClearRoom stops recording and drops its runtime slot. Finished recording
// files remain historical artifacts; no deleted room can keep ffmpeg running.
func (m *Manager) ClearRoom(roomID int64) {
	m.mu.Lock()
	state := m.rooms[roomID]
	delete(m.rooms, roomID)
	m.mu.Unlock()
	if state != nil && state.recording != nil {
		rec := state.recording
		if rec.cmd != nil && rec.cmd.Process != nil {
			_ = rec.cmd.Process.Kill()
		}
	}
}
