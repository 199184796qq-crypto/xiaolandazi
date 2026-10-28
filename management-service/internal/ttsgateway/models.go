package ttsgateway

import "strings"

const (
	ProviderCosyVoice = "cosyvoice"
	CosyVoiceFlash    = "cosyvoice-v3.5-flash"
	CosyVoicePlus     = "cosyvoice-v3.5-plus"
)

func IsCosyVoiceModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model == CosyVoiceFlash || model == CosyVoicePlus
}

func SupportsCloneModel(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case DefaultQwenTTSModel, "qwen-audio-3.0-tts-flash", "qwen-audio-3.1-tts-flash", CosyVoiceFlash, CosyVoicePlus:
		return true
	}
	return false
}

func ProviderForModel(model string) string {
	if strings.TrimSpace(model) == TencentPremiumModel {
		return ProviderTencent
	}
	if IsCosyVoiceModel(model) {
		return ProviderCosyVoice
	}
	return ProviderQwen
}

// Libraries share a tenant, but their provider-issued voice IDs never mix.
// CosyVoice 3.5 voices are bound to an exact model, including Flash/Plus.
func VoiceLibraryMatches(library, target string) bool {
	if library == "" {
		return true
	}
	if library == ProviderQwen {
		return !IsCosyVoiceModel(target) && target != TencentPremiumModel
	}
	return library == target
}

// SupportsSSML reports whether the TTS model accepts SSML <break> pauses.
func SupportsSSML(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	switch model {
	case "qwen-audio-3.0-tts-plus", "qwen-audio-3.0-tts-flash", "qwen-audio-3.1-tts-plus", "qwen-audio-3.1-tts-flash",
		CosyVoiceFlash, CosyVoicePlus, "cosyvoice-v2", "cosyvoice-v3-flash", "cosyvoice-v3-plus":
		return true
	}
	return false
}
