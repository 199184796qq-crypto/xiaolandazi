package anchorcycle

// Absolute adoption indices stay stable when formal runs compact old debug
// payloads. Immutable request/response audit remains in the database journal.
func (s *State) TotalAccepted() int { return s.AcceptedOffset + len(s.Accepted) }

func (s *State) CompactDeliveredHistory() []string {
	if s.Surface != "formal_live" || s.Delivery == nil {
		return nil
	}
	keep := max(128, s.Config.PointGap+5, s.Config.CombinationGap+5)
	remove := len(s.Accepted) - keep
	if remove <= 0 {
		return nil
	}
	var ids []string
	for _, segment := range s.Accepted[:remove] {
		ids = append(ids, segment.Selected.ID)
	}
	s.Accepted = append([]Segment(nil), s.Accepted[remove:]...)
	s.AcceptedOffset += remove
	return ids
}
