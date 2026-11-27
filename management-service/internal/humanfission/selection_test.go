package humanfission

import (
	"errors"
	"slices"
	"testing"
)

func TestHumanFissionSelectWordsAndPreserveAudio(t *testing.T) {
	pcm := make([]int16, Samples(1800))
	for i := range pcm {
		pcm[i] = 6500
	}
	clear(pcm[Samples(530):Samples(670)])
	clear(pcm[Samples(1130):Samples(1270)])
	text := "甲句，乙句，丙句。"
	part := Part{Start: 7000, Duration: 1800, Text: text, Object: "original.wav",
		SRT: SRT([]Cue{{Start: 100, End: 1700, Text: text}}),
		Words: []TimedWord{
			{Start: 100, End: 530, Text: "甲句"},
			{Start: 670, End: 1130, Text: "乙句"},
			{Start: 1270, End: 1700, Text: "丙句"},
		},
	}
	for _, tc := range []struct {
		name       string
		start, end int
		texts      []string
	}{
		{"middle", 3, 6, []string{"甲句，", "乙句，", "丙句。"}},
		{"start", 0, 3, []string{"甲句，", "乙句，丙句。"}},
		{"end", 6, 9, []string{"甲句，乙句，", "丙句。"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := CutSelection(part, pcm, tc.start, tc.end)
			if err != nil || len(out) != len(tc.texts) {
				t.Fatal(out, err)
			}
			var joined []int16
			for i, slice := range out {
				if slice.Part.Text != tc.texts[i] || slice.Part.Start != part.Start+MS(slice.From) {
					t.Fatal(slice)
				}
				if _, err := ReadSRT(slice.Part.SRT); err != nil {
					t.Fatal(err)
				}
				joined = append(joined, pcm[slice.From:slice.To]...)
			}
			if !slices.Equal(joined, pcm) {
				t.Fatal("cut omitted or duplicated samples")
			}
			if tc.name == "start" {
				more, err := CutSelection(out[1].Part, pcm[out[1].From:out[1].To], 0, 3)
				if err != nil || len(more) != 2 {
					t.Fatal(more, err)
				}
			}
		})
	}
	if _, err := CutSelection(part, pcm, 1, 6); !errors.Is(err, ErrSplitAlignment) {
		t.Fatal("split an incomplete word", err)
	}
	if _, err := CutSelection(part, pcm, 0, 9); !errors.Is(err, ErrSplitSelection) {
		t.Fatal("accepted the entire part", err)
	}
	legacy := part
	legacy.Words = nil
	if _, err := CutSelection(legacy, pcm, 3, 6); !errors.Is(err, ErrSplitAlignment) {
		t.Fatal("estimated word time without a word timeline", err)
	}
	legacy.SRT = SRT([]Cue{{100, 530, "甲句，"}, {670, 1130, "乙句，"}, {1270, 1700, "丙句。"}})
	if out, err := CutSelection(legacy, pcm, 3, 6); err != nil || len(out) != 3 {
		t.Fatal("sentence-level cuts should work for older recordings", err)
	}
	for i := range pcm {
		pcm[i] = 6500
	}
	if _, err := CutSelection(part, pcm, 3, 6); !errors.Is(err, ErrSplitQuiet) {
		t.Fatal("accepted an unsafe waveform cut", err)
	}
}

func TestHumanFissionSplitRemapsExistingRecipe(t *testing.T) {
	old := Recipe{ID: "old", Order: []int{2, 1, 3, 1}, Used: true}
	state := State{Input: "source", Parts: make([]Part, 3), Recipes: []Recipe{old},
		Last: old.ID, Render: &Render{}, Exhausted: true}
	state.ReplacePart(1, []Part{{Text: "前"}, {Text: "中"}, {Text: "后"}})
	recipe := state.Recipes[0]
	if len(state.Parts) != 5 || !slices.Equal(recipe.Order, []int{4, 1, 2, 3, 5, 1, 2, 3}) ||
		state.Last != recipe.ID || recipe.ID == old.ID || !recipe.Used || state.Exhausted || state.Render != nil {
		t.Fatal(state)
	}
}

func TestHumanFissionJoinPartsShiftsSubtitlesAndWords(t *testing.T) {
	first := Part{Start: 1000, End: 1850, Text: "甲。", SRT: SRT([]Cue{{Start: 80, End: 805, Text: "甲。"}}),
		Words: []TimedWord{{Start: 80, End: 805, Text: "甲"}}}
	second := Part{Start: 1850, End: 3000, Text: "乙。", SRT: SRT([]Cue{{Start: 50, End: 900, Text: "乙。"}}),
		Words: []TimedWord{{Start: 50, End: 900, Text: "乙"}}}
	got, err := JoinParts(first, second, Samples(850), Samples(1150))
	if err != nil || !got.Edited || got.Text != "甲。乙。" || got.Start != 1000 || got.End != 3000 || got.Duration != 2000 {
		t.Fatalf("join = %+v, %v", got, err)
	}
	cues, err := ReadSRT(got.SRT)
	if err != nil || len(cues) != 2 || cues[1] != (Cue{Start: 900, End: 1750, Text: "乙。"}) {
		t.Fatalf("joined subtitles = %+v, %v", cues, err)
	}
	if len(got.Words) != 2 || got.Words[1] != (TimedWord{Start: 900, End: 1750, Text: "乙"}) {
		t.Fatalf("joined words = %+v", got.Words)
	}
	second.Words = nil
	got, err = JoinParts(first, second, Samples(850), Samples(1150))
	if err != nil || len(got.Words) != 0 {
		t.Fatalf("incomplete timing should use recognition on the next split: %+v, %v", got.Words, err)
	}
}

func TestHumanFissionMergeRetainsOnlyEquivalentRecipes(t *testing.T) {
	parts := []Part{{Duration: 500}, {Duration: 500}, {Duration: 500}, {Duration: 500}}
	state := State{Input: "source", Duration: 2000, Parts: parts, Render: &Render{Object: "old.wav"}}
	state.AddPlans([][]int{{1, 2, 3, 4}, {4, 1}, {1, 3, 4}, {3, 2, 4}, {2, 3, 2, 3, 4}})
	state.Recipes[0].Used = true
	state.Last = state.Recipes[0].ID
	state.MergeParts(2, Part{Duration: 1000, Edited: true})
	if len(state.Parts) != 3 || len(state.Recipes) != 2 || state.Render != nil {
		t.Fatalf("invalid merged state: %+v", state)
	}
	if first := state.Recipes[0]; !first.Used || first.ID != state.Last || len(first.Order) != 3 ||
		first.Order[0] != 1 || first.Order[1] != 2 || first.Order[2] != 3 || first.Duration != 2000 {
		t.Fatalf("forward pair changed the recipe: %+v", first)
	}
	if kept := state.Recipes[1].Order; len(kept) != 2 || kept[0] != 3 || kept[1] != 1 {
		t.Fatalf("unaffected order was not renumbered: %v", kept)
	}
}
