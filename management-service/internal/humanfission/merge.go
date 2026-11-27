package humanfission

import (
	"errors"
	"slices"
	"strings"
)

type MergeRequest struct {
	First         int    `json:"first"`
	Second        int    `json:"second"`
	FirstVersion  string `json:"first_version"`
	SecondVersion string `json:"second_version"`
}

var ErrMergeSelection = errors.New("请选择相邻的两片语音")

func (m MergeRequest) Adjacent() bool {
	return m.First > 0 && m.Second > m.First && m.Second-m.First == 1
}

func (m MergeRequest) Matches(parts []Part) bool {
	return m.Adjacent() && m.Second <= len(parts) &&
		m.FirstVersion == parts[m.First-1].Version() && m.SecondVersion == parts[m.Second-1].Version()
}

// JoinParts joins the two source PCM ranges without cutting or inserting
// samples. Each subtitle uses the matching offset on the joined timeline.
func JoinParts(first, second Part, firstSamples, secondSamples int) (Part, error) {
	offset, duration := MS(firstSamples), MS(firstSamples+secondSamples)
	if offset <= 0 || duration <= offset {
		return Part{}, errors.New("语音片没有足够的声音可合并")
	}
	combined := Part{Start: first.Start, End: second.End, Duration: duration, Text: first.Text + second.Text, Edited: true}
	var cues []Cue
	var words []TimedWord
	var spoken strings.Builder
	for index, part := range []Part{first, second} {
		at, end := int64(0), offset
		if index == 1 {
			at, end = offset, duration
		}
		lines, err := ReadSRT(part.SRT)
		if err != nil {
			return Part{}, err
		}
		for _, line := range lines {
			line.Start, line.End = min(end, line.Start+at), min(end, line.End+at)
			if line.End > line.Start {
				cues = append(cues, line)
			}
		}
		for _, word := range part.Words {
			word.Start, word.End = min(end, word.Start+at), min(end, word.End+at)
			if word.Start >= at && word.End > word.Start {
				words = append(words, word)
				spoken.WriteString(speechText(word.Text))
			}
		}
	}
	if len(cues) == 0 {
		return Part{}, errors.New("合并后的语音片文稿为空")
	}
	combined.SRT = SRT(cues)
	if spoken.String() == speechText(combined.Text) {
		combined.Words = words
	}
	return combined, nil
}

// MergeParts keeps a recipe only when each use of the two source parts is
// already a consecutive forward pair. Otherwise substitution would change
// what the recipe says. Unaffected recipes keep their use history.
func (s *State) MergeParts(number int, merged Part) {
	s.Parts = slices.Replace(s.Parts, number-1, number+1, merged)
	old := s.Recipes
	s.Recipes = nil
	last := s.Last
	s.Last = ""
	for _, recipe := range old {
		order := mergedRecipeOrder(recipe.Order, number)
		if s.AddPlans([][]int{order}) == 0 {
			continue
		}
		next := &s.Recipes[len(s.Recipes)-1]
		next.Used = recipe.Used
		if last == recipe.ID {
			s.Last = next.ID
		}
	}
	s.Render, s.Renders = nil, nil
	s.Exhausted, s.ExhaustedAt = false, ""
}

func mergedRecipeOrder(old []int, first int) []int {
	order := make([]int, 0, len(old))
	for i := 0; i < len(old); i++ {
		n := old[i]
		switch {
		case n == first:
			if i+1 >= len(old) || old[i+1] != first+1 {
				return nil
			}
			i++
		case n == first+1:
			return nil
		case n > first+1:
			n--
		}
		order = append(order, n)
	}
	return order
}
