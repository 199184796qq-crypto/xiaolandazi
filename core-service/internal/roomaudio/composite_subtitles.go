package roomaudio

import (
	"fmt"
	"strings"
)

type CompositeCue struct {
	SegmentID      string `json:"segment_id"`
	Source         Source `json:"source"`
	Text           string `json:"text"`
	StartMS        int64  `json:"start_ms"`
	EndMS          int64  `json:"end_ms"`
	transcriptOnly bool
}

// Only PCM frames that entered the single output stream advance subtitle time.
func recordCompositeCue(state *roomState, frame Frame, cursor int) {
	timeline := state.mainlineTimeline
	if frame.Source == SourceInterrupt {
		timeline = state.interruptTimeline
	}
	i := speechSegmentIndex(timeline, state.currentSegmentID)
	if i < 0 {
		return
	}
	segment := timeline[i]
	if cursor >= 0 && (cursor < segment.StartMS || cursor >= segment.EndMS) {
		return
	}
	end := frame.PTSMS + int64(frame.DurationMS)
	n := len(state.compositeCues)
	if n > 0 {
		last := &state.compositeCues[n-1]
		if last.SegmentID == segment.SegmentID && last.Source == frame.Source && last.Text == segment.Text && last.EndMS == frame.PTSMS &&
			last.transcriptOnly == (frame.Source == SourceInterrupt && state.interruptTranscriptOnly[segment.SegmentID]) {
			last.EndMS = end
			return
		}
	}
	state.compositeCues = append(state.compositeCues, CompositeCue{segment.SegmentID, frame.Source, segment.Text, frame.PTSMS, end,
		frame.Source == SourceInterrupt && state.interruptTranscriptOnly[segment.SegmentID]})
}

// A bridge is part of the active interrupt stream and gets its own caption segment.
// Appending never resets the existing media cursor or the mainline resume position.
func (e *Engine) AppendInterruptSpeechSegment(roomID int64, segment SpeechSegment) {
	e.appendInterruptSegment(roomID, segment, false)
}

func (e *Engine) AppendInterruptTranscriptSegment(roomID int64, segment SpeechSegment) {
	e.appendInterruptSegment(roomID, segment, true)
}

func (e *Engine) appendInterruptSegment(roomID int64, segment SpeechSegment, transcriptOnly bool) {
	if e == nil || roomID <= 0 || len(cloneSpeechTimeline([]SpeechSegment{segment})) != 1 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	state := e.rooms[roomID]
	if state == nil || state.activeSource != SourceInterrupt {
		return
	}
	if speechSegmentIndex(state.interruptTimeline, segment.SegmentID) >= 0 {
		return
	}
	for i := range state.interruptTimeline {
		if state.interruptTimeline[i].EndMS > segment.StartMS {
			state.interruptTimeline[i].EndMS = segment.StartMS
		}
	}
	state.interruptTimeline = append(cloneSpeechTimeline(state.interruptTimeline), segment)
	if state.interruptTranscriptOnly == nil {
		state.interruptTranscriptOnly = make(map[string]bool)
	}
	state.interruptTranscriptOnly[segment.SegmentID] = transcriptOnly
	state.updatedAt = e.now().UTC()
}
func (e *Engine) CompositeCues(roomID int64) []CompositeCue {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if state := e.rooms[roomID]; state != nil && (state.phase != PhaseIdle || len(state.compositeCues) > 0) {
		return captionCues(state.compositeCues)
	}
	return captionCues(e.lastCompositeCues[roomID])
}

func captionCues(cues []CompositeCue) []CompositeCue {
	var result []CompositeCue
	for _, cue := range cues {
		if !cue.transcriptOnly {
			result = append(result, cue)
		}
	}
	return result
}
func (e *Engine) CompositeSRT(roomID int64) string {
	var b strings.Builder
	for i, cue := range e.CompositeCues(roomID) {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, srtTime(cue.StartMS), srtTime(cue.EndMS), cue.Text)
	}
	return b.String()
}
func srtTime(ms int64) string {
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}
