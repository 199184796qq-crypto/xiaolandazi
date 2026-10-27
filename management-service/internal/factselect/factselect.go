// Package factselect keeps only the facts that answer the question actually
// asked. Angle-scoped facts come first, then lexical relevance to the question
// and its angles; a fact that talks about a different place is dropped.
package factselect

import (
	"sort"
	"strings"

	"livecompanion/management/internal/factbound"
)

// Fact is the minimal view of a selected fact.
type Fact struct {
	ID   string
	Text string
}

const (
	angleBonus      = 3.0
	questionWeight  = 2.0
	angleTextWeight = 1.0
)

// Select filters facts down to the ones the question needs.
func Select(question string, angleTexts []string, angleFactIDs map[string]bool, facts []Fact, limit int) []Fact {
	if limit <= 0 {
		limit = 3
	}
	questionPlaces := factbound.PlacesIn(question)
	type scored struct {
		fact  Fact
		score float64
	}
	kept := make([]scored, 0, len(facts))
	for _, fact := range facts {
		if strings.TrimSpace(fact.ID) == "" || strings.TrimSpace(fact.Text) == "" {
			continue
		}
		if conflicts(questionPlaces, fact.Text) {
			continue
		}
		score := 0.0
		if angleFactIDs[fact.ID] {
			score += angleBonus
		}
		score += questionWeight * overlap(question, fact.Text)
		for _, angle := range angleTexts {
			score += angleTextWeight * overlap(angle, fact.Text)
		}
		if score <= 0 {
			continue
		}
		kept = append(kept, scored{fact: fact, score: score})
	}
	if len(kept) == 0 {
		// Nothing matched lexically: prefer the angle's own facts, then the
		// model's order, but never hand over the whole selection.
		out := make([]Fact, 0, 2)
		for _, fact := range facts {
			if angleFactIDs[fact.ID] {
				out = append(out, fact)
			}
			if len(out) >= 2 {
				return out
			}
		}
		for _, fact := range facts {
			if len(out) >= 2 {
				break
			}
			seen := false
			for _, item := range out {
				if item.ID == fact.ID {
					seen = true
					break
				}
			}
			if !seen {
				out = append(out, fact)
			}
		}
		return out
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].score > kept[j].score })
	if len(kept) > limit {
		kept = kept[:limit]
	}
	out := make([]Fact, 0, len(kept))
	for _, item := range kept {
		out = append(out, item.fact)
	}
	return out
}

// conflicts reports a fact about a different region than the one asked about.
func conflicts(questionPlaces []string, factText string) bool {
	if len(questionPlaces) == 0 {
		return false
	}
	factPlaces := factbound.PlacesIn(factText)
	if len(factPlaces) == 0 {
		return false
	}
	for _, asked := range questionPlaces {
		for _, mentioned := range factPlaces {
			if asked == mentioned {
				return false
			}
		}
	}
	return true
}

func overlap(query, text string) float64 {
	grams := bigrams(query)
	if len(grams) == 0 || text == "" {
		return 0
	}
	hits := 0
	for gram := range grams {
		if strings.Contains(text, gram) {
			hits++
		}
	}
	return float64(hits) / float64(len(grams))
}

var stopRunes = map[rune]bool{
	'的': true, '了': true, '呢': true, '吗': true, '吧': true, '啊': true, '呀': true,
	'我': true, '你': true, '他': true, '她': true, '它': true, '们': true,
	'这': true, '那': true, '个': true, '是': true, '有': true, '没': true,
}

func bigrams(text string) map[string]bool {
	runes := []rune(strings.TrimSpace(text))
	out := map[string]bool{}
	for i := 0; i+1 < len(runes); i++ {
		left, right := runes[i], runes[i+1]
		if stopRunes[left] && stopRunes[right] {
			continue
		}
		if !isContent(left) || !isContent(right) {
			continue
		}
		out[string(runes[i:i+2])] = true
	}
	return out
}

func isContent(r rune) bool {
	switch {
	case r >= 0x4e00 && r <= 0x9fff:
		return true
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return false
}
