package paidpipeline

// A synchronous preferences message withdraws unplayed queue reservations before
// the PUT returns. In-flight results cannot revive cancelled engine tasks.
func (p *Processor) ApplyInteractionControls(room int64) {
	if p == nil || p.policies == nil {
		return
	}
	pref := p.policies.RoomInteractionPreferences(room)
	if v := p.interactionV2.Load(); v != nil {
		v.mu.Lock()
		scope, ok := v.rooms[room]
		v.mu.Unlock()
		if ok {
			v.engine.SetPolicy(scope, p.v2Policy(scope), p.clock())
			for _, id := range v.engine.Cancelled(room) {
				p.decisions.Remove(room, id)
			}
		}
	}
	p.timeReportMu.Lock()
	if !pref.ControlEnabled("time_report") {
		if state := p.timeReports[room]; state != nil {
			if item, ok := p.decisions.Get(room, state.Decision); ok && item.PlaybackTaskID == "" {
				p.decisions.Remove(room, state.Decision)
			}
			delete(p.timeReports, room)
		}
	}
	p.timeReportMu.Unlock()
}
