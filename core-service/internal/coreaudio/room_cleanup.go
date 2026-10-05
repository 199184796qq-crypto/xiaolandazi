package coreaudio

// ClearRoom releases prepared mainline tracks and prefetch buffers after the
// program has been stopped. In-flight continuations check program identity.
func (c *Client) ClearRoom(roomID int64) {
	c.mu.Lock()
	if program := c.programs[roomID]; program != nil {
		program.Running = false
	}
	delete(c.programs, roomID)
	c.mu.Unlock()
	c.cancelRoomAudioMirror(roomID)
}
