package decisionexecutor

import (
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/voicecatalog"
	"testing"
)

func TestSelectedTencentOfficialVoiceRoutesInteractionAndBridge(t *testing.T) {
	voice, ok := voicecatalog.Find("101030")
	if !ok {
		t.Fatal("missing official voice")
	}
	profile := officialVoiceProfile(voice)
	if profile.VoiceID != "101030" || voiceModel(profile) != ttsgateway.TencentPremiumModel || voiceProvider(profile) != ttsgateway.ProviderTencent {
		t.Fatalf("Tencent selection lost: %+v", profile)
	}
	// Published identities may have a legacy provider label; the confirmed model
	// determines the actual service for both answers and optional bridges.
	profile.Provider = "aliyun_qwen"
	if voiceProvider(profile) != ttsgateway.ProviderTencent {
		t.Fatal("published voice fell back to Qwen")
	}
	legacy := officialVoiceProfile(voicecatalog.Default())
	if voiceProvider(legacy) != ttsgateway.ProviderQwen {
		t.Fatal("existing rooms changed")
	}
}
