package httpapi

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestValidateLiveAgentPlanVersionAllowsTextOnlyRecordingClone(t *testing.T) {
	input := model.CreateLiveAgentPlanVersionInput{
		RoomID:          29,
		DurationMinutes: 30,
		RoundMinutes:    5,
		VoiceIdentity: model.LiveAgentVoiceIdentity{
			Name: "克隆声音", Source: "official", Provider: "test", Model: "test", VoiceID: "voice-1", Rate: 1,
		},
		GenerationContext: map[string]any{
			"content_mode":         model.LiveContentUserAudio,
			"recording_voice_mode": "stream_clone",
			"recording_script": map[string]any{
				"tabs": []any{
					map[string]any{"tab_id": "t1", "title": "开场", "text": "这是文本框里的话术"},
				},
			},
		},
	}
	if err := validateLiveAgentPlanVersionInput(&input); err != nil {
		t.Fatalf("text-only recording clone should be valid: %v", err)
	}

	input.GenerationContext["recording_voice_mode"] = "original"
	if err := validateLiveAgentPlanVersionInput(&input); err == nil {
		t.Fatal("empty variants in original voice mode must still be rejected")
	}
}
