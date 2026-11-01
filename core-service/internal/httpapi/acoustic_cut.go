package httpapi

import "livecompanion/core/internal/audioout"

// Both providers use only normalized PCM evidence. Captions remain display
// data; they cannot authorize a live cut. A is preferred within the bounded
// deadline, otherwise the nearest B pause is used.
func chooseAcousticInteractionCut(points []audioout.AcousticPoint, currentMS, maxLead int) (int, bool) {
	for _, grade := range []string{"A", "B"} {
		best := 0
		for _, p := range points {
			lead := p.CutMS - currentMS
			if p.Grade == grade && lead >= 200 && lead <= maxLead && (best == 0 || p.CutMS < best) {
				best = p.CutMS
			}
		}
		if best > 0 {
			return best, true
		}
	}
	return 0, false
}
