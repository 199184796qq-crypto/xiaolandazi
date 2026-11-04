package ttsgateway

// Hash the actual credential and endpoint after account-pool voice mapping.
// Only the digest enters the private cache; credentials never enter logs/files.
func interactionProviderIdentity(p Provider) string {
	switch p := p.(type) {
	case *qwenProvider:
		return digestSynthesisText(p.apiKey + "\x00" + p.ttsBaseURL)
	case *cosyVoiceProvider:
		return interactionProviderIdentity(p.transport)
	case *volcengineProvider:
		return digestSynthesisText(p.config.APIKey + "\x00" + p.config.Endpoint + "\x00" + VolcengineSynthesisProfile)
	case *tencentProvider:
		return digestSynthesisText(p.config.AppID + "\x00" + p.config.SecretID + "\x00" + p.config.SecretKey + "\x00" + p.config.Endpoint)
	default:
		// Unknown adapters cannot claim a stable account identity.
		return ""
	}
}
