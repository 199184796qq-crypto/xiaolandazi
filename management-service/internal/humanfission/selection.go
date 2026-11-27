package humanfission

import (
	"errors"
	"math"
	"slices"
	"strings"
	"unicode"
)

type TimedWord struct {
	Start int64  `json:"start_ms"`
	End   int64  `json:"end_ms"`
	Text  string `json:"text"`
}

type SplitRequest struct {
	Number  int    `json:"number"`
	Version string `json:"version"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
}

var ErrSplitSelection = errors.New("请选择同一语音片中的一段文字，且保留至少一段未选中的内容")
var ErrSplitAlignment = errors.New("选区边缘无法可靠对齐，请改选完整词或整句")
var ErrSplitQuiet = errors.New("选区附近没有安全切点，请将选区边缘移到有停顿的位置")

func (p Part) Version() string { return Digest(p.Object) }

// Selection uses Unicode code point offsets, matching the browser offsets.
func Selection(text string, start, end int) ([]int, error) {
	chars := []rune(text)
	if start < 0 || end <= start || end > len(chars) || speechText(string(chars[start:end])) == "" {
		return nil, ErrSplitSelection
	}
	bounds := []int{0}
	if speechText(string(chars[:start])) != "" {
		bounds = append(bounds, start)
	}
	if speechText(string(chars[end:])) != "" {
		bounds = append(bounds, end)
	}
	if len(bounds) == 1 {
		return nil, ErrSplitSelection
	}
	return append(bounds, len(chars)), nil
}

func speechText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return r
	}, text)
}

type speechBoundary struct{ before, after int64 }

func timedBoundaries(text string, units []TimedWord, duration int64) map[int]speechBoundary {
	var spoken strings.Builder
	bounds := make(map[int]speechBoundary)
	count := 0
	var previous int64
	for _, unit := range units {
		word := speechText(unit.Text)
		if word == "" {
			continue
		}
		if unit.Start < 0 || unit.End <= unit.Start || unit.End > duration+100 || count > 0 && unit.Start+60 < previous {
			return nil
		}
		if count > 0 {
			bounds[count] = speechBoundary{before: previous, after: unit.Start}
		}
		spoken.WriteString(word)
		count += len([]rune(word))
		previous = unit.End
	}
	if spoken.String() != speechText(text) {
		return nil
	}
	return bounds
}

func splitBoundaries(part Part, bounds []int, duration int64) ([]speechBoundary, error) {
	cues, err := ReadSRT(part.SRT)
	if err != nil {
		return nil, err
	}
	sentences := make([]TimedWord, 0, len(cues))
	for _, cue := range cues {
		sentences = append(sentences, TimedWord{Start: cue.Start, End: cue.End, Text: cue.Text})
	}
	sentenceTimes := timedBoundaries(part.Text, sentences, duration)
	wordTimes := timedBoundaries(part.Text, part.Words, duration)
	chars := []rune(part.Text)
	times := make([]speechBoundary, 0, len(bounds)-2)
	for _, bound := range bounds[1 : len(bounds)-1] {
		count := len([]rune(speechText(string(chars[:bound]))))
		cut, ok := sentenceTimes[count]
		if !ok {
			cut, ok = wordTimes[count]
		}
		if !ok {
			return nil, ErrSplitAlignment
		}
		times = append(times, cut)
	}
	return times, nil
}

type SelectedAudio struct {
	From, To int
	Part     Part
}

// CutSelection requires both a text boundary and a quiet waveform around it.
// It never infers a time by distributing characters evenly across a sentence.
func CutSelection(part Part, pcm []int16, start, end int) ([]SelectedAudio, error) {
	bounds, err := Selection(part.Text, start, end)
	if err != nil {
		return nil, err
	}
	times, err := splitBoundaries(part, bounds, MS(len(pcm)))
	if err != nil {
		return nil, err
	}
	edges := []int{0}
	for _, tm := range times {
		cut, ok := quietSelectionCut(pcm, tm)
		if !ok || cut <= edges[len(edges)-1]+Samples(40) || cut >= len(pcm)-Samples(40) {
			return nil, ErrSplitQuiet
		}
		edges = append(edges, cut)
	}
	edges = append(edges, len(pcm))
	chars := []rune(part.Text)
	cues, err := ReadSRT(part.SRT)
	if err != nil {
		return nil, err
	}
	cut := make([]SelectedAudio, 0, len(edges)-1)
	for i, from := range edges[:len(edges)-1] {
		to := edges[i+1]
		first, limit := bounds[i], bounds[i+1]
		text := string(chars[first:limit])
		duration := MS(to - from)
		subtitles := clipCues(cues, part.Text, first, limit, MS(from), duration)
		child := Part{
			Start: part.Start + MS(from), End: part.Start + MS(to), Duration: duration,
			Text: text, SRT: SRT(subtitles), Edited: true, Words: clipWords(part.Words, part.Text, first, limit, MS(from), duration),
		}
		cut = append(cut, SelectedAudio{From: from, To: to, Part: child})
	}
	return cut, nil
}

func quietSelectionCut(pcm []int16, tm speechBoundary) (int, bool) {
	const window = Rate / 100
	left := max(window, Samples(min(tm.before, tm.after)-70))
	right := min(len(pcm)-window-1, Samples(max(tm.before, tm.after)+70))
	target := Samples((tm.before + tm.after) / 2)
	best, distance := 0, math.MaxInt
	for i := left; i <= right; i += Rate / 400 {
		delta := i - target
		if delta < 0 {
			delta = -delta
		}
		if delta >= distance {
			continue
		}
		var energy int64
		var peak int
		for _, sample := range pcm[i-window : i+window] {
			value := int(sample)
			peak = max(peak, value, -value)
			energy += int64(value * value)
		}
		if peak <= 1150 && energy <= int64(2*window)*300*300 {
			best, distance = i, delta
		}
	}
	return best, distance != math.MaxInt
}

func clipCues(cues []Cue, original string, first, limit int, offset, duration int64) []Cue {
	chars := []rune(original)
	var out []Cue
	at := 0
	for _, cue := range cues {
		next := at + len([]rune(cue.Text))
		left, right := max(at, first), min(next, limit)
		begin, end := max(0, cue.Start-offset), min(duration, cue.End-offset)
		if end > begin && right > left && right <= len(chars) {
			if text := strings.TrimSpace(string(chars[left:right])); text != "" {
				out = append(out, Cue{Start: begin, End: end, Text: text})
			}
		}
		at = next
	}
	var joined strings.Builder
	for _, cue := range out {
		joined.WriteString(cue.Text)
	}
	text := strings.TrimSpace(string(chars[first:limit]))
	if speechText(joined.String()) != speechText(text) {
		return []Cue{{Start: 0, End: duration, Text: text}}
	}
	return out
}

func clipWords(words []TimedWord, text string, first, limit int, offset, duration int64) []TimedWord {
	chars := []rune(text)
	from := len([]rune(speechText(string(chars[:first]))))
	to := len([]rune(speechText(string(chars[:limit]))))
	var out []TimedWord
	at := 0
	for _, word := range words {
		next := at + len([]rune(speechText(word.Text)))
		if next > at && at >= from && next <= to {
			word.Start = max(0, word.Start-offset)
			word.End = min(duration, word.End-offset)
			if word.End > word.Start {
				out = append(out, word)
			}
		}
		at = next
	}
	return out
}

func (s *State) ReplacePart(number int, parts []Part) {
	increase := len(parts) - 1
	s.Parts = slices.Replace(s.Parts, number-1, number, parts...)
	for i := range s.Recipes {
		recipe := &s.Recipes[i]
		var order []int
		for _, n := range recipe.Order {
			switch {
			case n < number:
				order = append(order, n)
			case n > number:
				order = append(order, n+increase)
			default:
				for j := range parts {
					order = append(order, number+j)
				}
			}
		}
		id := Digest([]any{s.Input, order})
		if s.Last == recipe.ID {
			s.Last = id
		}
		recipe.ID, recipe.Order = id, order
	}
	s.Render, s.Renders = nil, nil
	s.Exhausted, s.ExhaustedAt = false, ""
}
