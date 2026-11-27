package humanfission

import (
	"slices"
	"testing"
)

func TestHumanFissionPlansValidateOrderAndRotate(t *testing.T) {
	state := State{Input: "source", Duration: 1000, Parts: []Part{{Duration: 150}, {Duration: 250}, {Duration: 600}}}
	orders := [][]int{{1}, {3, 3}, {3, 2, 3}, {4, 3}, {1, 2}, {1, 2, 1, 3}, {3}, {3, 2, 3, 2}, {1, 2}}
	if got := state.AddPlans(orders); got != 3 {
		t.Fatalf("added %d recipes: %+v", got, state.Recipes)
	}
	if got := state.Recipes[1]; !slices.Equal(got.Order, []int{1, 2, 1, 3}) || got.Duration != 1150 {
		t.Fatal(got)
	}
	last := state.Recipes[0].ID
	state.Recipes[0].Used, state.Recipes[1].Used = true, true
	got := state.Choices(last)
	if got[0].ID != state.Recipes[2].ID || got[1].ID != state.Recipes[1].ID || got[2].ID != last {
		t.Fatal(got)
	}
	state.Recipes = state.Recipes[:1]
	if only := state.Choices(last); len(only) != 1 || only[0].ID != last {
		t.Fatal(only)
	}
	if !DurationOK(400, 1000) || !DurationOK(1300, 1000) || DurationOK(399, 1000) || DurationOK(1301, 1000) {
		t.Fatal("incorrect duration bounds")
	}
}

func TestHumanFissionCutsFollowGuidanceAndWAVFormat(t *testing.T) {
	pcm := make([]int16, Samples(5000))
	for i := range pcm {
		pcm[i] = 5000
	}
	clear(pcm[Samples(900):Samples(1100)])
	clear(pcm[Samples(1950):Samples(2140)]) // Too short to use.
	clear(pcm[Samples(2800):Samples(3100)])
	data, err := ReadWAV(WAV(pcm))
	if err != nil || !slices.Equal(data, pcm) {
		t.Fatal("WAV data changed", err)
	}
	edges, err := Partition(data, []int64{1050, 2880})
	expected := []int{0, Samples(1000), Samples(2950), len(pcm)}
	if err != nil || !slices.Equal(edges, expected) {
		t.Fatal(edges, err)
	}
	if _, err = Partition(data, []int64{1000, 2000, 3000}); err == nil {
		t.Fatal("accepted group count with too few safe cuts")
	}
	cues := []Cue{{Start: 15, End: 1350, Text: "买一份。"}, {Start: 1400, End: 1800, Text: "到店领取。"}}
	if parsed, err := ReadSRT(SRT(cues)); err != nil || !slices.Equal(parsed, cues) {
		t.Fatal(parsed, err)
	}
}
