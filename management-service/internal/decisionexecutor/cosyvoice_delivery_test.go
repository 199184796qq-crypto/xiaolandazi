package decisionexecutor

import (
	"encoding/json"
	"livecompanion/management/internal/speechmission"
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

func TestLegacyHostStateCannotAffectDelivery(t *testing.T) {
	for _, state := range []string{"今天嗓子不舒服，咳嗽", "今天很累没精神", "今天状态好很兴奋", "今天很紧张焦虑"} {
		t.Run(state, func(t *testing.T) {
			var legacy speechmission.Mission
			raw, _ := json.Marshal(map[string]any{
				"id": "legacy", "human_style": map[string]any{
					"guidance": "自然表达",
					"state":    map[string]any{"host_state": state, "expires_at": "2099-01-01T00:00:00Z", "heat": "warm"},
					"reaction": map[string]any{"enabled": true, "channel": "TTS_STYLE", "instruction": "语气亲切自然"},
				},
			})
			if err := json.Unmarshal(raw, &legacy); err != nil {
				t.Fatal(err)
			}
			clean := legacy
			clean.ID = "clean"
			clean.HumanStyle.State = speechmission.HumanStatePlan{Heat: "warm"}
			missions := speechmission.New()
			missions.Ensure(legacy)
			missions.Ensure(clean)
			w := &Worker{missions: missions}
			oldItem, cleanItem := &decisionItem{MissionID: "legacy"}, &decisionItem{MissionID: "clean"}
			if got, want := w.ttsInstructionForMission(oldItem, "自然播报"), w.ttsInstructionForMission(cleanItem, "自然播报"); got != want {
				t.Fatalf("legacy state affected TTS: %q != %q", got, want)
			}
			got, err := w.cosyVoiceInstructionForMission(oldItem)
			if err != nil {
				t.Fatal(err)
			}
			want, err := w.cosyVoiceInstructionForMission(cleanItem)
			if err != nil || got != want {
				t.Fatalf("legacy state affected CosyVoice: %q != %q, %v", got, want, err)
			}
			encoded, err := json.Marshal(legacy.HumanStyle.State)
			if err != nil || string(encoded) != `{"heat":"warm"}` {
				t.Fatalf("legacy fields survived decoding: %s, %v", encoded, err)
			}
		})
	}
}
