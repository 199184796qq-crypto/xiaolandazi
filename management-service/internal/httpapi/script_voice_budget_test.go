package httpapi

import "testing"

func TestScriptVoiceHourlyBudgetIsDisabled(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("XIAOLAN_SCRIPT_VOICE_HOURLY_CHARS", "1")
	if !allowScriptVoiceSynthesis(1, 80_000) {
		t.Fatal("retired char setting blocked generation")
	}
	recordScriptVoiceSynthesis(1, 80_000)
	if !allowScriptVoiceSynthesis(1, 80_000) || scriptVoiceHourlyLimit() != 0 {
		t.Fatal("hourly char limit still active")
	}
}
