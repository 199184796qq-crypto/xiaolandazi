package audioout

import (
	"strings"
	"time"
	"unicode"
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
	DecisionID       string       `json:"decision_id,omitempty"`
	ProgramID        string       `json:"program_id"`
	TaskID           string       `json:"task_id"`
	Token            string       `json:"token"`
	BeforeText       string       `json:"before_text"`
	ReplyText        string       `json:"reply_text"`
	Units            []ReturnUnit `json:"units"`
	SkippedUnits     []string     `json:"skipped_units,omitempty"`
	AudioStartedAt   time.Time    `json:"audio_started_at"`
	AnswerDurationMS int          `json:"answer_duration_ms"`
	Deadline         time.Time    `json:"deadline"`
	BridgeText       string       `json:"bridge_text,omitempty"`
	BridgeStaged     bool         `json:"bridge_staged"`
}

// Similar topic/vector distance can nominate a unit, but cannot prove that
// its new prices, qualifiers or promises have already been answered.
func CompleteUnitCovered(answer, unit string) bool {
	normalize := func(text string) string {
		var b strings.Builder
		for _, r := range text {
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				b.WriteRune(unicode.ToLower(r))
			}
		}
		return b.String()
	}
	covered, target := normalize(answer), normalize(unit)
	return len([]rune(target)) >= 4 && strings.Contains(covered, target)
}
