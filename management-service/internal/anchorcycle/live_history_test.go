package anchorcycle

import "testing"

func TestFormalHistoryCompactionKeepsAbsoluteMemoryAndMacroIndices(t *testing.T) {
	state := State{Surface: "formal_live", Delivery: &MainlineDelivery{}, Config: Config{PointGap: 2, CombinationGap: 8}, Used: map[string]Use{"point×angle": {LastIndex: 295, Count: 3}}}
	for i := 0; i < 300; i++ {
		state.Accepted = append(state.Accepted, Segment{Selected: Candidate{ID: ID()}})
	}
	removed := state.CompactDeliveredHistory()
	if len(state.Accepted) != 128 || len(removed) != 172 || state.TotalAccepted() != 300 || state.Used["point×angle"].LastIndex != 295 {
		t.Fatal(state)
	}
	state.Surface = "generation_preview"
	if len(state.CompactDeliveredHistory()) != 0 {
		t.Fatal("preview history was pruned")
	}
}
