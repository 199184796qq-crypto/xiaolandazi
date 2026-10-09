package decisionexecutor

import (
	"livecompanion/management/internal/ttsgateway"
	"testing"
)

func TestCosyVoiceDeliveryFitsProtocolAndResolvesProvider(t *testing.T) {
	w := &Worker{}
	for _, key := range []string{"read_comment_softly", "hard_cut", "ask_controller", "thinking_pause", "repeat_confirm", ""} {
		instruction, err := w.cosyVoiceInstructionForMission(&decisionItem{SelectedInterrupt: key})
		if err != nil || instruction == "" || ttsgateway.CharacterCount(instruction) > 100 {
			t.Fatal(key, instruction, err)
		}
	}
}
