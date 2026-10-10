package coreaudio

import "livecompanion/core/internal/audioout"

// Evidence comes from emitted PCM positions, not generation, queue insertion,
// wall-clock estimates or the requested post-interaction resume offset.
type playbackCoverage struct {
	Ranges   [][2]int
	Duration int
	Terminal bool
}

func mergePlaybackRanges(values [][2]int, start, end int) ([][2]int, int64) {
	if end <= start {
		return values, 0
	}
	before := int64(0)
	for _, v := range values {
		before += int64(v[1] - v[0])
	}
	values = append(values, [2]int{start, end})
	for i := len(values) - 1; i > 0 && values[i][0] < values[i-1][0]; i-- {
		values[i], values[i-1] = values[i-1], values[i]
	}
	out := [][2]int{}
	for _, v := range values {
		if len(out) > 0 && v[0] <= out[len(out)-1][1] {
			out[len(out)-1][1] = max(out[len(out)-1][1], v[1])
		} else {
			out = append(out, v)
		}
	}
	after := int64(0)
	for _, v := range out {
		after += int64(v[1] - v[0])
	}
	return out, after - before
}
func savePlaybackReceipt(p *programState, id string, coverage playbackCoverage) {
	ms := int64(0)
	for _, r := range coverage.Ranges {
		ms += int64(r[1] - r[0])
	}
	complete := len(coverage.Ranges) == 1 && coverage.Ranges[0][0] == 0 && coverage.Ranges[0][1] >= coverage.Duration
	p.PlaybackSequence++
	r := audioout.MainlinePlaybackReceipt{Sequence: p.PlaybackSequence, TrackID: id, PlayedMS: ms, Complete: complete, Terminal: coverage.Terminal}
	out := make([]audioout.MainlinePlaybackReceipt, 0, len(p.PlaybackReceipts)+1)
	for _, old := range p.PlaybackReceipts {
		if old.TrackID != id {
			out = append(out, old)
		}
	}
	out = append(out, r)
	p.PlaybackReceipts = out[max(0, len(out)-256):]
}
func finishPlaybackReceipt(p *programState, id string) {
	if p.PlaybackCoverage == nil {
		p.PlaybackCoverage = map[string]playbackCoverage{}
	}
	v, ok := p.PlaybackCoverage[id]
	if v.Terminal {
		return
	}
	if !ok {
		// A completely skipped queued track also releases its reservation,
		// without pretending that any audio was played.
		for _, track := range p.Tracks {
			if track.ID == id {
				v.Duration = track.DurationMS
				break
			}
		}
		if v.Duration <= 0 {
			return
		}
	}
	v.Terminal = true
	p.PlaybackCoverage[id] = v
	savePlaybackReceipt(p, id, v)
}
func (c *Client) recordMainlinePCM(p *programState, taskID, trackID string, start, end, duration int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.programs[p.RoomID] != p || !p.Running || p.CurrentTaskID != taskID || !p.Sequential {
		return
	}
	if p.PlaybackCoverage == nil {
		p.PlaybackCoverage = map[string]playbackCoverage{}
	}
	v := p.PlaybackCoverage[trackID]
	if v.Terminal {
		return
	}
	v.Duration = duration
	var delta int64
	v.Ranges, delta = mergePlaybackRanges(v.Ranges, max(0, start), min(duration, end))
	p.MainlinePlayedMS += delta
	p.PlaybackCoverage[trackID] = v
	// One bounded latest receipt per track. Progress is published at one-second
	// granularity, and the final frame is always recorded immediately.
	oldMS := int64(0)
	for _, r := range p.PlaybackReceipts {
		if r.TrackID == trackID {
			oldMS = r.PlayedMS
		}
	}
	ms := int64(0)
	for _, span := range v.Ranges {
		ms += int64(span[1] - span[0])
	}
	if end >= duration {
		v.Terminal = true
		p.PlaybackCoverage[trackID] = v
		savePlaybackReceipt(p, trackID, v)
	} else if ms-oldMS >= 1000 {
		savePlaybackReceipt(p, trackID, v)
	}
}
