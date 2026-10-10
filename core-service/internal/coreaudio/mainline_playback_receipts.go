package coreaudio

import "livecompanion/core/internal/audioout"

// A track use begins at its first accepted PCM frame, never at staging or hub
// publication. Resume does not count twice. Late mirrors cannot count new runs.
func (c *Client) recordActualTrackStart(p *programState, task, track string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p == nil || c.programs[p.RoomID] != p || !p.Running || !p.Sequential || p.CurrentTaskID != task || track == "" {
		return
	}
	if p.ActualTrackSeen == nil {
		p.ActualTrackSeen = map[string]bool{}
	}
	if p.ActualTrackSeen[track] {
		return
	}
	p.ActualTrackSeen[track] = true
	p.ActualTrackSequence++
	p.ActualTrackReceipts = append(p.ActualTrackReceipts, audioout.MainlinePlaybackReceipt{Sequence: p.ActualTrackSequence, TrackID: track, StartedAt: c.now().UTC()})
	if len(p.ActualTrackReceipts) > 256 {
		p.ActualTrackReceipts = append([]audioout.MainlinePlaybackReceipt(nil), p.ActualTrackReceipts[len(p.ActualTrackReceipts)-256:]...)
	}
}
