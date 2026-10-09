package httpapi

import (
	"sort"
	"strings"

	"livecompanion/core/internal/audioout"
)

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

func preparationCutPlans(program audioout.RoomProgramSnapshot, currentMS int) []interactionCutPlan {
	window := program.FutureWindow
	if len(window) == 0 && program.Task != nil {
		window = []audioout.ProgramWindowTrack{{TrackID: program.TrackID, TrackIndex: program.TrackIndex, DurationMS: program.Task.DurationMS, Text: program.TrackText, Timeline: program.Timeline, SafePoints: program.SafePoints}}
	}
	var candidates []interactionCutPlan
	for i, track := range window {
		trackProgram := audioout.RoomProgramSnapshot{TrackText: track.Text, Timeline: track.Timeline, SafePoints: track.SafePoints}
		for _, point := range effectiveRoomProgramSafePoints(trackProgram) {
			if !completeSentenceCut(trackProgram, point) {
				continue
			}
			lead := track.OffsetMS + point.CutMS - currentMS
			if lead < 3000 || lead > 60000 || point.CutMS > track.DurationMS {
				continue
			}
			before, after, segment := roomProgramCutContext(trackProgram, point.CutMS)
			if after == "" && i+1 < len(window) {
				next := window[i+1]
				if len(next.Timeline) > 0 {
					segment = next.Timeline[0].SegmentID
					for _, unit := range next.Timeline[:min(2, len(next.Timeline))] {
						after += strings.TrimSpace(unit.Text) + " "
					}
					after = strings.TrimSpace(after)
				} else {
					after = strings.TrimSpace(next.Text)
				}
			}
			// A cut with no buffered return unit is not a preparation opportunity.
			if before == "" || after == "" {
				continue
			}
			candidates = append(candidates, interactionCutPlan{TrackID: track.TrackID, TrackIndex: track.TrackIndex, CutMS: point.CutMS, LeadMS: lead, PointGrade: point.Grade, CurrentText: before, ResumeText: after, ResumeSegmentID: segment})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].LeadMS < candidates[j].LeadMS })
	var plans []interactionCutPlan
	for _, level := range []string{"A", "B", "C"} {
		for _, candidate := range candidates {
			if level != "C" && candidate.LeadMS < 24000 {
				continue
			}
			if level == "A" && candidate.PointGrade != "A" {
				continue
			}
			if level == "B" && candidate.PointGrade != "B" {
				continue
			}
			if level == "C" && candidate.LeadMS < 24000 && len(candidates) > 0 && candidates[len(candidates)-1].LeadMS >= 24000 {
				continue
			}
			candidate.Level = level
			plans = append(plans, candidate)
			break
		}
	}
	return plans
}

// End-of-track return context is the next complete buffered semantic unit.
func preparationContext(program audioout.RoomProgramSnapshot, currentMS int) (interactionCutPlan, []interactionCutPlan, bool) {
	plans := preparationCutPlans(program, currentMS)
	if len(plans) == 0 {
		return interactionCutPlan{}, nil, false
	}
	return plans[0], plans, true
}

func actualInteractionCutLevel(program audioout.RoomProgramSnapshot, cutMS int) string {
	for _, point := range effectiveRoomProgramSafePoints(program) {
		if point.CutMS == cutMS {
			if point.Grade == "A" || point.Grade == "B" {
				return point.Grade
			}
			return "C"
		}
	}
	return "C"
}

func actualCutLevel(program audioout.RoomProgramSnapshot, cut *int) string {
	if cut == nil {
		return ""
	}
	return actualInteractionCutLevel(program, *cut)
}

func completeSentenceCut(program audioout.RoomProgramSnapshot, point audioout.ProgramSafePoint) bool {
	// A/B are approved sentence boundaries; degraded C needs explicit evidence.
	if point.Grade == "A" || point.Grade == "B" {
		return true
	}
	for _, unit := range program.Timeline {
		if unit.EndMS == point.CutMS && unit.SafeCut {
			return true
		}
	}
	text := strings.TrimSpace(point.LeftPreview)
	return strings.HasSuffix(text, ".") || strings.HasSuffix(text, "!") || strings.HasSuffix(text, "?") || strings.HasSuffix(text, "。") || strings.HasSuffix(text, "！") || strings.HasSuffix(text, "？") || strings.HasSuffix(text, "；")
}
