package anchorcycle

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var directionListMarker = regexp.MustCompile(`^\s*([0-9]{1,3})([.、．)])(\s*)(.*)$`)

// Only the selectable view is deduplicated. Raw generations and their original
// program-owned IDs remain in permanent history and model exchange archives.
func DistinctDirectionCards(groups ...[]AngleHook) []AngleHook {
	result := []AngleHook{}
	seen := map[string]bool{}
	for _, cards := range groups {
		for _, card := range cards {
			key := strings.TrimSpace(card.Action)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, card)
		}
	}
	return result
}

// List numbers are presentation, never identifiers. Source scope is frozen by
// the caller. These references record request provenance, not claim citations.
func ParseDirectionContent(raw, generation string, facts []HookFact, products []string, goal string) *AnglePlan {
	parts := []string{}
	current := []string{}
	found := false
	flush := func() {
		if s := strings.TrimSpace(strings.Join(current, "\n")); s != "" {
			parts = append(parts, s)
		}
		current = nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		m := directionListMarker.FindStringSubmatch(line)
		marker := false
		if len(m) > 0 {
			n, _ := strconv.Atoi(m[1])
			marker = n > 0
			if (m[2] == "." || m[2] == "．") && m[3] == "" && len(m[4]) > 0 && m[4][0] >= '0' && m[4][0] <= '9' {
				marker = false
			}
		}
		if marker {
			if found {
				flush()
			} else {
				current = nil
			}
			found = true
			current = append(current, m[4])
		} else if found {
			current = append(current, line)
		}
	}
	if found {
		flush()
	} else if s := strings.TrimSpace(raw); s != "" {
		parts = append(parts, s)
	}
	ids, benefits := []string{}, []string{}
	seen := map[string]bool{}
	for _, f := range facts {
		ids = append(ids, f.ID)
		for _, b := range f.Benefits {
			if !seen[b] {
				seen[b] = true
				benefits = append(benefits, b)
			}
		}
	}
	p := &AnglePlan{Goal: goal, Raw: raw, GenerationID: generation}
	for i, text := range parts {
		p.Cards = append(p.Cards, AngleHook{ID: fmt.Sprintf("%s/%d", generation, i+1), Title: text, Action: text, Facts: append([]string(nil), ids...), Products: append([]string(nil), products...), Benefits: append([]string(nil), benefits...)})
	}
	return p
}

type AngleUseProgress struct {
	Plays            int            `json:"actual_plays"`
	LastSupplementAt int            `json:"last_supplement_at"`
	LastAttemptAt    int            `json:"last_attempt_at"`
	DirectionPlays   map[string]int `json:"direction_plays,omitempty"`
}
type AngleTrackBinding struct {
	PoolKey string `json:"pool_key"`
	HookID  string `json:"hook_id"`
}
type AnglePlaybackReceipt struct {
	Sequence  uint64 `json:"sequence"`
	TrackID   string `json:"track_id"`
	StartedAt string `json:"started_at"`
}
type AnglePlayedUse struct {
	AngleTrackBinding
	TrackID   string `json:"track_id"`
	StartedAt string `json:"started_at"`
}

func (s *State) AngleUsage(key string) *AngleUseProgress {
	if s.AngleProgress == nil {
		s.AngleProgress = map[string]*AngleUseProgress{}
	}
	p := s.AngleProgress[key]
	if p == nil {
		p = &AngleUseProgress{LastAttemptAt: -1, DirectionPlays: map[string]int{}}
		s.AngleProgress[key] = p
	}
	if p.DirectionPlays == nil {
		p.DirectionPlays = map[string]int{}
	}
	return p
}
func (s *State) AngleRefreshThreshold() int {
	if s.Config.AngleRefreshAfterPlays > 0 {
		return s.Config.AngleRefreshAfterPlays
	}
	return 3
}
func (s *State) AngleRefreshDue(key string) bool {
	p := s.AngleUsage(key)
	return p.Plays-p.LastSupplementAt >= s.AngleRefreshThreshold() && p.Plays-p.LastAttemptAt >= s.AngleRefreshThreshold()
}
func (s *State) RegisterAngleTrack(segment Segment) {
	c := segment.Selected
	if c.ID == "" || c.AnglePlan == nil || c.AnglePlan.SourceSHA == "" {
		return
	}
	if s.AngleRegisteredTracks == nil {
		s.AngleRegisteredTracks = map[string]bool{}
	}
	if s.AngleRegisteredTracks[c.ID] {
		return
	}
	s.AngleRegisteredTracks[c.ID] = true
	if s.AngleTrackBindings == nil {
		s.AngleTrackBindings = map[string]AngleTrackBinding{}
	}
	hookID := "unplanned"
	if c.AngleHook != nil {
		hookID = c.AngleHook.ID
	}
	s.AngleTrackBindings[c.ID] = AngleTrackBinding{PoolKey: c.AnglePlan.SourceSHA, HookID: hookID}
}

// Called only with Core PCM receipts for this exact program. A resumed track is
// the same use. Generated/adopted/delivered content never enters this method.
func (s *State) ApplyAnglePlayback(program string, receipts []AnglePlaybackReceipt) []AnglePlayedUse {
	if s.Surface != "formal_live" || s.Delivery == nil || program == "" || s.Delivery.ProgramID != program {
		return nil
	}
	uses := []AnglePlayedUse{}
	for _, r := range receipts {
		if r.Sequence <= s.AngleReceiptSequence {
			continue
		}
		s.AngleReceiptSequence = r.Sequence
		b, ok := s.AngleTrackBindings[r.TrackID]
		if !ok || r.StartedAt == "" {
			continue
		}
		delete(s.AngleTrackBindings, r.TrackID)
		p := s.AngleUsage(b.PoolKey)
		p.Plays++
		p.DirectionPlays[b.HookID]++
		uses = append(uses, AnglePlayedUse{AngleTrackBinding: b, TrackID: r.TrackID, StartedAt: r.StartedAt})
	}
	return uses
}
