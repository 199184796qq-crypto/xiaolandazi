package roomaudio

// CancelPreparedInterrupt releases only this pending mainline boundary. It
// preserves the live PCM cursor, timeline and subscribers; a playing answer or
// another cut reservation cannot be reset by an old request's cancellation.
func (e *Engine) CancelPreparedInterrupt(roomID int64, cut string) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.rooms[roomID]
	if s == nil || s.activeSource != SourceMainline || (s.phase != PhasePreparingInterrupt && s.phase != PhaseArmed) {
		return false
	}
	if s.phase == PhaseArmed && s.plannedCutSegmentID != cut {
		return false
	}
	s.phase = PhaseMainline
	s.plannedCutSegmentID = ""
	s.resumeSegmentID = ""
	s.interruptTimeline = nil
	s.interruptCursorMS = 0
	s.updatedAt = e.now().UTC()
	return true
}
