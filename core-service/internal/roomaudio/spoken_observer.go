package roomaudio

import "time"

// SpokenSegment is emitted on the first accepted PCM frame of a caption
// segment. It proves server output began, not that a physical device heard it.
type SpokenSegment struct {
	RoomID          int64
	Sequence        uint64
	Source          Source
	SegmentID, Text string
	At              time.Time
	RemainingMS     int
}

// This observer is for bounded in-memory work only. It runs outside the audio
// lock and must never call a model, TTS, storage or a remote API.
func (e *Engine) SetSpokenObserver(observer func(SpokenSegment)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spokenObserver = observer
}
func notifySpoken(observer func(SpokenSegment), segment SpokenSegment) {
	// A diagnostic/scheduling observer cannot break the foreground audio stream.
	defer func() { _ = recover() }()
	observer(segment)
}
