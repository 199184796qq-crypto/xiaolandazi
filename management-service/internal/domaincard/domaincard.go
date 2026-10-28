// Package domaincard stores a per-plan map of question dimensions to the facts
// that can answer them. The card is built when a plan is saved; it is never
// shown in the console and never replaces the approved fact package.
package domaincard

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Fact is the minimal fact view needed to build and validate a card.
type Fact struct {
	ID   string
	Text string
}

// Dimension is one question dimension: audience phrasings, applicability
// conditions, and the exact facts that belong to it.
type Dimension struct {
	Name       string   `json:"name"`
	Phrases    []string `json:"phrases"`
	Conditions []string `json:"conditions,omitempty"`
	FactIDs    []string `json:"fact_ids"`
}

// Card is stored inside the published version's generation context.
type Card struct {
	Domain       string      `json:"domain,omitempty"`
	Product      string      `json:"product,omitempty"`
	Fulfillment  string      `json:"fulfillment,omitempty"`
	Dimensions   []Dimension `json:"dimensions"`
	SourceSHA256 string      `json:"source_sha256,omitempty"`
	BuiltAt      string      `json:"built_at,omitempty"`
}

// FromGeneration reads a card from a version generation context. Missing or
// malformed cards are treated as absent so old versions keep their old path.
func FromGeneration(generation map[string]any) *Card {
	if generation == nil {
		return nil
	}
	raw, ok := generation["domain_card"]
	if !ok || raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var card Card
	if json.Unmarshal(data, &card) != nil || len(card.Dimensions) == 0 {
		return nil
	}
	return &card
}

// Parse decodes model output, tolerating a fenced JSON block.
func Parse(raw string) (*Card, error) {
	text := strings.TrimSpace(raw)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("domain card JSON not found")
	}
	var card Card
	if err := json.Unmarshal([]byte(text[start:end+1]), &card); err != nil {
		return nil, err
	}
	return &card, nil
}

// NormalizeAndValidate makes a model-produced card safe to use:
//   - every dimension keeps at least one phrase;
//   - only existing fact IDs survive;
//   - every supplied fact is represented, missing ones go to "其他".
func NormalizeAndValidate(card *Card, facts []Fact) error {
	if card == nil || len(card.Dimensions) == 0 {
		return fmt.Errorf("domain card has no dimensions")
	}
	known := make(map[string]Fact, len(facts))
	for _, fact := range facts {
		id := strings.TrimSpace(fact.ID)
		if id == "" || strings.TrimSpace(fact.Text) == "" {
			continue
		}
		known[id] = fact
	}
	if len(known) == 0 {
		return fmt.Errorf("domain card has no facts")
	}
	covered := make(map[string]bool, len(known))
	seenNames := make(map[string]bool, len(card.Dimensions))
	dimensions := make([]Dimension, 0, len(card.Dimensions))
	for _, dimension := range card.Dimensions {
		name := strings.TrimSpace(dimension.Name)
		if name == "" || seenNames[name] {
			continue
		}
		seenNames[name] = true
		phrases := cleanStrings(dimension.Phrases)
		if len(phrases) == 0 {
			phrases = []string{name}
		}
		ids := make([]string, 0, len(dimension.FactIDs))
		seenIDs := make(map[string]bool, len(dimension.FactIDs))
		for _, id := range dimension.FactIDs {
			id = strings.TrimSpace(id)
			if id == "" || seenIDs[id] {
				continue
			}
			if _, ok := known[id]; !ok {
				continue
			}
			seenIDs[id] = true
			covered[id] = true
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			continue
		}
		dimensions = append(dimensions, Dimension{
			Name:       name,
			Phrases:    phrases,
			Conditions: cleanStrings(dimension.Conditions),
			FactIDs:    ids,
		})
	}
	if len(dimensions) == 0 {
		return fmt.Errorf("domain card has no usable dimensions")
	}
	var missing []string
	for id := range known {
		if !covered[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		dimensions = append(dimensions, Dimension{
			Name:    "其他",
			Phrases: []string{"其他", "还有别的吗"},
			FactIDs: missing,
		})
	}
	card.Dimensions = dimensions
	return nil
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// Match returns the facts belonging to the question's dimensions. It is a
// recall layer only: applicability/conditions are still judged by the answer
// model.
func Match(question string, card *Card, facts []Fact) []Fact {
	if card == nil || len(card.Dimensions) == 0 {
		return nil
	}
	q := normalize(question)
	if q == "" {
		return nil
	}
	type scored struct {
		dimension Dimension
		score     float64
	}
	candidates := make([]scored, 0, len(card.Dimensions))
	for _, dimension := range card.Dimensions {
		score := dimensionScore(q, dimension)
		if score > 0 {
			candidates = append(candidates, scored{dimension: dimension, score: score})
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	if len(candidates) > 2 {
		candidates = candidates[:2]
	}
	known := make(map[string]Fact, len(facts))
	for _, fact := range facts {
		if id := strings.TrimSpace(fact.ID); id != "" {
			known[id] = fact
		}
	}
	out := make([]Fact, 0, 8)
	seen := make(map[string]bool, 8)
	for _, candidate := range candidates {
		for _, id := range candidate.dimension.FactIDs {
			if fact, ok := known[id]; ok && !seen[id] {
				seen[id] = true
				out = append(out, fact)
				if len(out) >= 10 {
					return out
				}
			}
		}
	}
	return out
}

func dimensionScore(question string, dimension Dimension) float64 {
	score := 0.0
	for _, phrase := range dimension.Phrases {
		p := normalize(phrase)
		if p == "" {
			continue
		}
		if strings.Contains(question, p) {
			score += 3 + float64(len([]rune(p)))/8
			continue
		}
		if len([]rune(p)) >= 3 && strings.Contains(p, question) {
			score += 1
		}
	}
	name := normalize(dimension.Name)
	if name != "" && strings.Contains(question, name) {
		score += 2
	}
	return score
}

func normalize(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		switch r {
		case '吗', '呢', '吧', '啊', '呀', '嘛':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
