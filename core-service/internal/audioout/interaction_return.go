package audioout

import (
	"time"
)

type ReturnUnit struct {
	ID         string `json:"id"`
	TrackID    string `json:"track_id"`
	TrackIndex int    `json:"track_index"`
	StartMS    int    `json:"start_ms"`
	EndMS      int    `json:"end_ms"`
	Text       string `json:"text"`
}
type InteractionReturnSnapshot struct {
	DecisionID             string       `json:"decision_id,omitempty"`
	ProgramID              string       `json:"program_id"`
	TaskID                 string       `json:"task_id"`
	Token                  string       `json:"token"`
	BeforeText             string       `json:"before_text"`
	ReplyText              string       `json:"reply_text"`
	Units                  []ReturnUnit `json:"units"`
	SkippedUnits           []string     `json:"skipped_units,omitempty"`
	AudioStartedAt         time.Time    `json:"audio_started_at"`
	AnswerDurationMS       int          `json:"answer_duration_ms"`
	Deadline               time.Time    `json:"deadline"`
	AllowConsecutiveRepeat bool         `json:"allow_consecutive_repeat,omitempty"`
	ReturnAllowsRepeat     bool         `json:"return_allows_repeat,omitempty"`
	BridgeDropReason       string       `json:"bridge_drop_reason,omitempty"`
	BridgeText             string       `json:"bridge_text,omitempty"`
	BridgeStaged           bool         `json:"bridge_staged"`
}

// Only the immediately preceding complete sentence (or the entire answer)
// can prove an exact duplicate. Earlier mentions and changed punctuation do not.
func CompleteUnitCovered(answer, unit string) bool {
	units := SpeechUnits(answer)
	return SameSpeech(answer, unit) || (len(units) > 0 && SameSpeech(units[len(units)-1], unit))
}

func (s InteractionReturnSnapshot) DuplicatesBridge(text string) bool {
	entry := ""
	if len(s.Units) > 0 {
		entry = s.Units[0].Text
	}
	return BridgeDuplicatesAdjacent(text, s.ReplyText, entry, s.AllowConsecutiveRepeat || s.ReturnAllowsRepeat)
}
