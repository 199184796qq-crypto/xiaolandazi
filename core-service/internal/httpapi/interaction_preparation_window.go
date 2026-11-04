package httpapi

import "livecompanion/core/internal/audioout"

// Offsets in a preparation plan belong to their immutable track. LeadMS is
// measured on the continuous future stream; the playback cursor remains local.
type interactionCutPlan struct {
	Level           string `json:"level"`
	TrackID         string `json:"track_id"`
	TrackIndex      int    `json:"track_index"`
	CutMS           int    `json:"cut_ms"`
	LeadMS          int    `json:"lead_ms"`
	PointGrade      string `json:"point_grade"`
	CurrentText     string `json:"current_text"`
	ResumeText      string `json:"resume_text"`
	ResumeSegmentID string `json:"resume_segment_id"`
}

// Captions and legacy safe_points cannot authorize a cut. The grade is
// obtained exclusively from the current track's verified PCM scan.
func actualInteractionCutLevel(program audioout.RoomProgramSnapshot, cutMS int) string {
	for _, point := range program.AcousticCuts {
		if point.CutMS == cutMS {
			return point.Grade
		}
	}
	return ""
}
func actualCutLevel(program audioout.RoomProgramSnapshot, cut *int) string {
	if cut == nil {
		return ""
	}
	return actualInteractionCutLevel(program, *cut)
}
